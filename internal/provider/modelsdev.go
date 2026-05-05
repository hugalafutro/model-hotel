package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

const modelsDevAPIURL = "https://models.dev/api.json"

// ModelsDevCache holds an in-memory index of the models.dev catalogue.
// It is safe for concurrent read access after initial Load().
type ModelsDevCache struct {
	mu       sync.RWMutex
	byID     map[string]*ModelsDevModelSpec // exact model ID → spec
	loaded   bool
	loadTime time.Time
}

// ModelsDevProviderSpec represents a provider entry in the models.dev API.
type ModelsDevProviderSpec struct {
	ID     string                         `json:"id"`
	Name   string                         `json:"name"`
	API    string                         `json:"api"`
	Doc    string                         `json:"doc"`
	Models map[string]*ModelsDevModelSpec `json:"models"`
}

// ModelsDevModelSpec represents a model entry in the models.dev API.
type ModelsDevModelSpec struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Family           string               `json:"family,omitempty"`
	Attachment       bool                 `json:"attachment"`
	Reasoning        bool                 `json:"reasoning"`
	ToolCall         bool                 `json:"tool_call"`
	Temperature      *bool                `json:"temperature,omitempty"`
	StructuredOutput *bool                `json:"structured_output,omitempty"`
	Knowledge        string               `json:"knowledge,omitempty"`
	ReleaseDate      string               `json:"release_date,omitempty"`
	LastUpdated      string               `json:"last_updated,omitempty"`
	Modalities       ModelsDevModalities  `json:"modalities"`
	OpenWeights      bool                 `json:"open_weights"`
	Cost             ModelsDevCost        `json:"cost"`
	Limit            ModelsDevLimit       `json:"limit"`
	Interleaved      ModelsDevInterleaved `json:"interleaved,omitempty"`
}

type ModelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type ModelsDevCost struct {
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	CacheRead   *float64 `json:"cache_read,omitempty"`
	CacheWrite  *float64 `json:"cache_write,omitempty"`
	InputAudio  *float64 `json:"input_audio,omitempty"`
	OutputAudio *float64 `json:"output_audio,omitempty"`
	Reasoning   *float64 `json:"reasoning,omitempty"`
}

type ModelsDevLimit struct {
	Context int  `json:"context"`
	Output  int  `json:"output"`
	Input   *int `json:"input,omitempty"`
}

// ModelsDevInterleaved handles the "interleaved" field which can be either
// a bool or an object {"field": "..."} in the models.dev API.
type ModelsDevInterleaved struct {
	Field string
	Bool  bool
}

func (i *ModelsDevInterleaved) UnmarshalJSON(data []byte) error {
	// Try bool first
	var b bool
	if json.Unmarshal(data, &b) == nil {
		i.Bool = b
		i.Field = ""
		return nil
	}
	// Try object
	var obj struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	i.Field = obj.Field
	i.Bool = true
	return nil
}

// Global models.dev cache instance.
var modelsDevCache = &ModelsDevCache{}

// LoadModelsDev fetches the models.dev API and builds the in-memory index.
// Each call fetches fresh data from the remote API and replaces the cache.
// It is safe to call concurrently — the write is protected by a mutex.
func LoadModelsDev(ctx context.Context) error {
	return modelsDevCache.load(ctx, http.DefaultClient)
}

// LoadModelsDevWithClient is the testable version of LoadModelsDev.
func LoadModelsDevWithClient(ctx context.Context, client *http.Client) error {
	return modelsDevCache.load(ctx, client)
}

// GetModelsDevCache returns the global cache. Returns nil if not loaded.
func GetModelsDevCache() *ModelsDevCache {
	modelsDevCache.mu.RLock()
	defer modelsDevCache.mu.RUnlock()
	if !modelsDevCache.loaded {
		return nil
	}
	return modelsDevCache
}

func (c *ModelsDevCache) load(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, "GET", modelsDevAPIURL, nil)
	if err != nil {
		return fmt.Errorf("models.dev: failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("models.dev: fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models.dev: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50MB limit
	if err != nil {
		return fmt.Errorf("models.dev: failed to read response: %w", err)
	}

	var providers map[string]*ModelsDevProviderSpec
	if err := json.Unmarshal(body, &providers); err != nil {
		return fmt.Errorf("models.dev: failed to parse JSON: %w", err)
	}

	// Build flat index: model ID → spec (across all providers).
	index := make(map[string]*ModelsDevModelSpec)
	for _, p := range providers {
		if p == nil || p.Models == nil {
			continue
		}
		for modelID, spec := range p.Models {
			if spec == nil {
				continue
			}
			// Use the model ID from the map key (matches spec.ID).
			key := modelID
			if key == "" {
				key = spec.ID
			}
			if key != "" {
				// First provider wins — avoids overwriting with less detailed entries.
				if _, exists := index[key]; !exists {
					index[key] = spec
				}
			}
		}
	}

	c.mu.Lock()
	c.byID = index
	c.loaded = true
	c.loadTime = time.Now()
	c.mu.Unlock()

	debuglog.Info("modelsdev: loaded models", "models", len(index), "providers", len(providers))
	return nil
}

// Lookup finds a models.dev spec by exact model ID.
func (c *ModelsDevCache) Lookup(modelID string) *ModelsDevModelSpec {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.byID[modelID]
}

// LookupFuzzy tries exact match first, then prefix-based matching.
// This handles cases where the provider returns "gpt-4o-2024-08-06"
// but models.dev has "gpt-4o".
func (c *ModelsDevCache) LookupFuzzy(modelID string) *ModelsDevModelSpec {
	if c == nil {
		return nil
	}

	// 1. Exact match.
	if spec := c.Lookup(modelID); spec != nil {
		return spec
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// 2. Model ID with date suffix: try stripping the date portion.
	//    e.g. "gpt-4o-2024-08-06" → try "gpt-4o"
	if parts := strings.Split(modelID, "-"); len(parts) >= 2 {
		// Check if last part(s) look like a date (YYYY-MM-DD or YYYYMMDD).
		last := parts[len(parts)-1]
		if len(last) == 4 && isNumeric(last) {
			// Try without the date suffix "-YYYY"
			candidate := strings.Join(parts[:len(parts)-1], "-")
			if spec, ok := c.byID[candidate]; ok {
				return spec
			}
		}
		if len(parts) >= 3 {
			// Try without "-YYYY-MM-DD" or "-YYYYMMDD"
			last3 := strings.Join(parts[len(parts)-3:], "-")
			if looksLikeDate(last3) {
				candidate := strings.Join(parts[:len(parts)-3], "-")
				if spec, ok := c.byID[candidate]; ok {
					return spec
				}
			}
		}
	}

	// 3. Model ID with version suffix: try stripping last segment.
	//    e.g. "claude-sonnet-4-20250514" → try "claude-sonnet-4"
	if parts := strings.Split(modelID, "-"); len(parts) >= 2 {
		last := parts[len(parts)-1]
		if isNumeric(last) && len(last) >= 6 {
			// Strip the trailing numeric date/version segment.
			candidate := strings.Join(parts[:len(parts)-1], "-")
			if spec, ok := c.byID[candidate]; ok {
				return spec
			}
		}
	}

	// 4. Prefix match: find the longest key that is a prefix of modelID.
	//    This handles "claude-3-5-sonnet-20241022" matching "claude-3-5-sonnet".
	//    Note: O(n) over the full index — acceptable for discovery-time batch
	//    usage but not suitable for hot-path per-request lookups.
	var bestMatch *ModelsDevModelSpec
	bestLen := 0
	for key, spec := range c.byID {
		if strings.HasPrefix(modelID, key) && len(key) > bestLen {
			bestMatch = spec
			bestLen = len(key)
		}
	}

	return bestMatch
}

// EnrichModel fills gaps in a model.Model using models.dev data.
// It only overwrites fields that are empty/zero (never replaces existing data).
// Returns true if at least one field was enriched.
func (c *ModelsDevCache) EnrichModel(m *model.Model) bool {
	if c == nil {
		return false
	}

	spec := c.LookupFuzzy(m.ModelID)
	if spec == nil {
		return false
	}

	// Parse existing capabilities to merge.
	var caps model.Capability
	if m.Capabilities != "" && m.Capabilities != "{}" {
		if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
			debuglog.Debug("models.dev: failed to parse capabilities JSON", "model_id", m.ModelID, "error", err)
		}
	}

	enriched := false

	// Display name: only set if empty or same as model_id.
	if m.DisplayName == "" || m.DisplayName == m.ModelID {
		if spec.Name != "" {
			m.DisplayName = spec.Name
			enriched = true
		}
	}

	// Context length: only set if nil.
	if m.ContextLength == nil && spec.Limit.Context > 0 {
		ctxLen := spec.Limit.Context
		m.ContextLength = &ctxLen
		enriched = true
	}

	// Max output tokens: only set if nil.
	if m.MaxOutputTokens == nil && spec.Limit.Output > 0 {
		maxOut := spec.Limit.Output
		m.MaxOutputTokens = &maxOut
		enriched = true
	}

	// Input price: only set if nil.
	if m.InputPricePerMillion == nil && spec.Cost.Input > 0 {
		inPrice := spec.Cost.Input
		m.InputPricePerMillion = &inPrice
		enriched = true
	}

	// Output price: only set if nil.
	if m.OutputPricePerMillion == nil && spec.Cost.Output > 0 {
		outPrice := spec.Cost.Output
		m.OutputPricePerMillion = &outPrice
		enriched = true
	}

	// Cache hit price: only set if nil and models.dev has it.
	if m.InputPricePerMillionCacheHit == nil && spec.Cost.CacheRead != nil && *spec.Cost.CacheRead > 0 {
		cachePrice := *spec.Cost.CacheRead
		m.InputPricePerMillionCacheHit = &cachePrice
		enriched = true
	}

	// Capabilities: only set individual fields if they're currently false.
	if spec.Reasoning && !caps.Reasoning {
		caps.Reasoning = true
		enriched = true
	}
	if spec.ToolCall && !caps.ToolCalling {
		caps.ToolCalling = true
		enriched = true
	}
	if spec.StructuredOutput != nil && *spec.StructuredOutput && !caps.StructuredOutput {
		caps.StructuredOutput = true
		enriched = true
	}
	// Attachment → Vision mapping.
	if spec.Attachment && !caps.Vision {
		caps.Vision = true
		enriched = true
	}

	// Modalities: only set if currently empty/default.
	if (m.Modality == "" || m.Modality == "text") && len(spec.Modalities.Input) > 0 {
		mod := modalityFromModelsDev(spec.Modalities)
		if mod != "" {
			m.Modality = mod
			enriched = true
		}
	}
	if (m.InputModalities == "" || m.InputModalities == "[]") && len(spec.Modalities.Input) > 0 {
		inMods, _ := json.Marshal(spec.Modalities.Input)
		m.InputModalities = string(inMods)
		enriched = true
	}
	if (m.OutputModalities == "" || m.OutputModalities == "[]") && len(spec.Modalities.Output) > 0 {
		outMods, _ := json.Marshal(spec.Modalities.Output)
		m.OutputModalities = string(outMods)
		enriched = true
	}

	// Owned by / family: only set if empty.
	if m.OwnedBy == "" && spec.Family != "" {
		m.OwnedBy = spec.Family
		enriched = true
	}

	if enriched {
		capJSON, _ := json.Marshal(caps)
		m.Capabilities = string(capJSON)
	}
	return enriched
}

// EnrichModels enriches a batch of models using models.dev data.
// Returns the number of models that were enriched (had at least one field filled).
func (c *ModelsDevCache) EnrichModels(models []*model.Model) int {
	if c == nil {
		return 0
	}
	count := 0
	for _, m := range models {
		if c.EnrichModel(m) {
			count++
		}
	}
	return count
}

// modalityFromModelsDev derives a modality string from models.dev modalities.
func modalityFromModelsDev(mods ModelsDevModalities) string {
	hasImage := contains(mods.Input, "image")
	hasAudio := contains(mods.Input, "audio")
	hasVideo := contains(mods.Input, "video")

	switch {
	case hasVideo:
		return "video"
	case hasAudio && hasImage:
		return "multimodal"
	case hasImage:
		return "vision"
	case hasAudio:
		return "audio"
	default:
		return "text"
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func looksLikeDate(s string) bool {
	// Matches patterns like "2024-08-06" or "20240806"
	cleaned := strings.ReplaceAll(s, "-", "")
	return len(cleaned) == 8 && isNumeric(cleaned)
}
