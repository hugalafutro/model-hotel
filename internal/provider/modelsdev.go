package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
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
	mu         sync.RWMutex
	byID       map[string]*ModelsDevModelSpec            // exact model ID → spec (cross-provider, canonical-first)
	byProvider map[string]map[string]*ModelsDevModelSpec // models.dev provider ID → model ID → spec
	loaded     bool
	loadTime   time.Time
}

// modelsDevCanonical names the models.dev provider entry that carries a Model
// Hotel provider type's own official metadata and pricing, and whether that
// entry is the ONLY models.dev source the type may use.
type modelsDevCanonical struct {
	ID string
	// Exclusive stops the lookup from falling back to the cross-provider index
	// when the canonical entry misses. Set for single-vendor provider types:
	// their API serves only their own models, so another models.dev provider's
	// data for the same bare ID is by definition secondhand (OpenCode Go lists
	// "glm-5.3" with a guessed price before Z.ai publishes one, and that guess
	// must not become the metered price on a Z.ai provider). Aggregator and
	// catch-all types stay non-exclusive: their listings genuinely span many
	// vendors, so the cross-provider index is legitimate gap coverage.
	Exclusive bool
}

// modelsDevProviderForType maps Model Hotel provider types (as returned by
// provider_type) to their canonical models.dev entry. Enrichment consults
// that entry's models first, so a reseller's price for the same bare model ID
// (models.dev lists "glm-5.2" under 26 different providers) can never shadow
// the official one.
//
// Coding-plan provider types map to the pay-per-token provider (zai-coding →
// "zai", kimi-code → "moonshotai"), not to the "-coding-plan" models.dev
// entries: those price every model at $0 (subscription), while Model Hotel
// meters the shadow cost a request would have had at list price.
//
// "ollama-cloud" is deliberately absent: models.dev's ollama-cloud entry
// carries no cost data at all (subscription shape), so mapping it would return
// canonical specs whose empty prices block the cross-provider index that is
// Ollama Cloud's only pricing source.
var modelsDevProviderForType = map[string]modelsDevCanonical{
	// Single-vendor types: canonical entry or nothing.
	"anthropic":      {ID: "anthropic", Exclusive: true},
	"deepseek":       {ID: "deepseek", Exclusive: true},
	"xai":            {ID: "xai", Exclusive: true},
	"google":         {ID: "google", Exclusive: true},
	"vertex-express": {ID: "google-vertex", Exclusive: true},
	"cohere":         {ID: "cohere", Exclusive: true},
	"minimax":        {ID: "minimax", Exclusive: true},
	"kimi-code":      {ID: "moonshotai", Exclusive: true},
	"zai-coding":     {ID: "zai", Exclusive: true},
	// Aggregators and the unknown-host catch-all ("openai"): canonical first,
	// cross-provider index as gap coverage (Bedrock/Azure host many vendors'
	// models, custom OpenAI-compatible hosts serve arbitrary ones).
	"openai":       {ID: "openai"},
	"nanogpt":      {ID: "nano-gpt"},
	"openrouter":   {ID: "openrouter"},
	"opencode-go":  {ID: "opencode-go"},
	"opencode-zen": {ID: "opencode"},
	"bedrock":      {ID: "amazon-bedrock"},
	"azure":        {ID: "azure"},
	"neuralwatt":   {ID: "neuralwatt"},
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
	Interleaved      ModelsDevInterleaved `json:"interleaved"`
}

// ModelsDevModalities describes input and output modalities for a models.dev model.
type ModelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// ModelsDevCost contains pricing information for a models.dev model.
type ModelsDevCost struct {
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	CacheRead   *float64 `json:"cache_read,omitempty"`
	CacheWrite  *float64 `json:"cache_write,omitempty"`
	InputAudio  *float64 `json:"input_audio,omitempty"`
	OutputAudio *float64 `json:"output_audio,omitempty"`
	Reasoning   *float64 `json:"reasoning,omitempty"`
}

// ModelsDevLimit describes token limits for a models.dev model.
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

// UnmarshalJSON implements json.Unmarshaler for ModelsDevInterleaved (bool or object).
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

// LoadModelsDevWithClient fetches the models.dev API and builds the in-memory
// index. Each call fetches fresh data from the remote API and replaces the
// cache. It is safe to call concurrently: the write is protected by a mutex.
// Callers supply the client so production can use a SafeDialer-backed
// transport (models.dev redirects must not become an SSRF vector).
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

// ResetModelsDevCache clears the models.dev cache. For use in tests only.
func ResetModelsDevCache() {
	modelsDevCache.mu.Lock()
	modelsDevCache.loaded = false
	modelsDevCache.byID = nil
	modelsDevCache.byProvider = nil
	modelsDevCache.mu.Unlock()
}

func (c *ModelsDevCache) load(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, "GET", modelsDevAPIURL, http.NoBody)
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

	// Per-provider index: models.dev provider ID → model ID → spec. This is the
	// authoritative lookup path for providers listed in modelsDevProviderForType.
	perProvider := make(map[string]map[string]*ModelsDevModelSpec, len(providers))
	for pid, p := range providers {
		if p == nil || len(p.Models) == 0 {
			continue
		}
		pm := make(map[string]*ModelsDevModelSpec, len(p.Models))
		for modelID, spec := range p.Models {
			if spec == nil {
				continue
			}
			key := modelID
			if key == "" {
				key = spec.ID
			}
			if key != "" {
				pm[key] = spec
			}
		}
		perProvider[pid] = pm
	}

	// Cross-provider index: model ID → spec, first provider wins. The same bare
	// model ID appears under dozens of models.dev providers (official vendor
	// plus resellers, each with its own prices), so the iteration order decides
	// which spec a fallback lookup sees. Rank canonical providers (the values of
	// modelsDevProviderForType, the official vendor entries) ahead of everything
	// else, sorted within each group so the winner is deterministic across
	// loads. Ranging over the providers map directly would pick a random winner
	// per process start.
	canonical := make(map[string]bool, len(modelsDevProviderForType))
	for _, c := range modelsDevProviderForType {
		canonical[c.ID] = true
	}
	providerIDs := make([]string, 0, len(perProvider))
	for pid := range perProvider {
		providerIDs = append(providerIDs, pid)
	}
	sort.Slice(providerIDs, func(i, j int) bool {
		ci, cj := canonical[providerIDs[i]], canonical[providerIDs[j]]
		if ci != cj {
			return ci
		}
		return providerIDs[i] < providerIDs[j]
	})

	index := make(map[string]*ModelsDevModelSpec)
	for _, pid := range providerIDs {
		for key, spec := range perProvider[pid] {
			if _, exists := index[key]; !exists {
				index[key] = spec
			}
		}
	}

	c.mu.Lock()
	c.byID = index
	c.byProvider = perProvider
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return lookupFuzzyIn(c.byID, modelID)
}

// lookupForProvider resolves a spec for a model discovered on a Model Hotel
// provider of the given type. The canonical models.dev provider entry for that
// type (see modelsDevProviderForType) is consulted first, since it carries the
// vendor's own official metadata and pricing; only on a miss does the lookup
// fall back to the cross-provider index. An empty or unmapped providerType
// goes straight to the cross-provider index.
func (c *ModelsDevCache) lookupForProvider(providerType, modelID string) *ModelsDevModelSpec {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if canonical, ok := modelsDevProviderForType[providerType]; ok {
		if spec := lookupFuzzyIn(c.byProvider[canonical.ID], modelID); spec != nil {
			return spec
		}
		if canonical.Exclusive {
			return nil
		}
	}
	return lookupFuzzyIn(c.byID, modelID)
}

// lookupFuzzyIn runs the exact-then-fuzzy match against one index map. The
// caller holds whatever lock protects the map.
func lookupFuzzyIn(index map[string]*ModelsDevModelSpec, modelID string) *ModelsDevModelSpec {
	if len(index) == 0 {
		return nil
	}

	// 1. Exact match.
	if spec, ok := index[modelID]; ok {
		return spec
	}

	// 2. Vendor/dot-prefixed IDs (Bedrock: "openai.gpt-oss-120b", inference
	//    profiles: "us.anthropic.claude-x"): strip dot segments left-to-right,
	//    accepting only an exact key hit. models.dev keys are full model IDs,
	//    so a stripped tail ("zai.glm-4.7" → "glm-4.7" → "7") only matches
	//    when the cache genuinely catalogs that ID under the tail name.
	rest := modelID
	for {
		i := strings.IndexByte(rest, '.')
		if i < 0 || i == len(rest)-1 {
			break
		}
		rest = rest[i+1:]
		if spec, ok := index[rest]; ok {
			return spec
		}
	}

	// 3. Model ID with date suffix: try stripping the date portion.
	//    e.g. "gpt-4o-2024-08-06" → try "gpt-4o"
	if parts := strings.Split(modelID, "-"); len(parts) >= 2 {
		// Check if last part(s) look like a date (YYYY-MM-DD or YYYYMMDD).
		last := parts[len(parts)-1]
		if len(last) == 4 && isNumeric(last) {
			// Try without the date suffix "-YYYY"
			candidate := strings.Join(parts[:len(parts)-1], "-")
			if spec, ok := index[candidate]; ok {
				return spec
			}
		}
		if len(parts) >= 3 {
			// Try without "-YYYY-MM-DD" or "-YYYYMMDD"
			last3 := strings.Join(parts[len(parts)-3:], "-")
			if looksLikeDate(last3) {
				candidate := strings.Join(parts[:len(parts)-3], "-")
				if spec, ok := index[candidate]; ok {
					return spec
				}
			}
		}
	}

	// 4. Model ID with version suffix: try stripping last segment.
	//    e.g. "claude-sonnet-4-20250514" → try "claude-sonnet-4"
	if parts := strings.Split(modelID, "-"); len(parts) >= 2 {
		last := parts[len(parts)-1]
		if isNumeric(last) && len(last) >= 6 {
			// Strip the trailing numeric date/version segment.
			candidate := strings.Join(parts[:len(parts)-1], "-")
			if spec, ok := index[candidate]; ok {
				return spec
			}
		}
	}

	// 4. Prefix match with date/version suffix: find the longest catalog key
	//    that is a prefix of modelID, AND the remainder looks like a date or
	//    version suffix. This prevents "gpt-5-search-api" from matching "gpt-5".
	//    e.g. "claude-3-5-sonnet-20241022" → "claude-3-5-sonnet" ✓
	//    e.g. "gpt-5-search-api" → "gpt-5" ✗ (remainder is not a date/version)
	var bestMatch *ModelsDevModelSpec
	bestLen := 0
	for key, spec := range index {
		if strings.HasPrefix(modelID, key) && len(key) > bestLen {
			// Check that the remainder after the prefix is just a date/version suffix.
			// The key must either match exactly or be followed by a "-" then a
			// date/version string (not a model variant like "-search-api").
			remainder := modelID[len(key):]
			if remainder == "" {
				// Exact match (shouldn't reach here since step 1 catches these,
				// but safe to include).
				bestMatch = spec
				bestLen = len(key)
			} else if strings.HasPrefix(remainder, "-") {
				suffix := remainder[1:] // strip leading "-"
				if looksLikeDateOrVersion(suffix) {
					bestMatch = spec
					bestLen = len(key)
				}
			}
		}
	}

	return bestMatch
}

// fillIfEmpty copies v into *dst when *dst is nil and v is positive,
// reporting whether it did.
func fillIfEmpty[T int | float64](dst **T, v T) bool {
	if *dst != nil || v <= 0 {
		return false
	}
	*dst = &v
	return true
}

// mergeSpecCapabilities ORs models.dev capability flags into caps (never
// clears an already-set flag), reporting whether anything changed.
func mergeSpecCapabilities(spec *ModelsDevModelSpec, caps *model.Capability) bool {
	merged := false
	if spec.Reasoning && !caps.Reasoning {
		caps.Reasoning = true
		merged = true
	}
	if spec.ToolCall && !caps.ToolCalling {
		caps.ToolCalling = true
		merged = true
	}
	if spec.StructuredOutput != nil && *spec.StructuredOutput && !caps.StructuredOutput {
		caps.StructuredOutput = true
		merged = true
	}
	// Attachment → Vision mapping, corroborated by the declared input
	// modalities. models.dev sets attachment on a number of models whose only
	// input modality is text, among them deepseek-chat and deepseek-reasoner,
	// which answer an image with HTTP 400 "This model does not support image".
	// Attachment alone therefore advertises vision the provider will refuse.
	if spec.Attachment && attachmentImpliesVision(spec.Modalities.Input) && !caps.Vision {
		caps.Vision = true
		merged = true
	}
	return merged
}

// clearRefusedCapabilities clears a flag the catalog claims and the
// provider's API refuses, after the OR-merge above has put it in. models.dev
// lists structured output for Google's image-output models, following
// Google's own docs, and the API answers JSON mode on every one of them with
// a 400 (google-gemini/cookbook#1028); discovery leaves the flag off for
// them, so the merge must not switch it back on. Reports whether it cleared
// anything.
func clearRefusedCapabilities(providerType, modelID string, caps *model.Capability) bool {
	if !caps.StructuredOutput || !googleServedImageModel(providerType, modelID) {
		return false
	}
	caps.StructuredOutput = false
	return true
}

// googleServedImageModel reports an image-output model that Google's own
// generateContent route serves, where Google's refusals apply: on Google AI
// Studio and Vertex AI express by name, and on OpenCode Zen for the Gemini
// family it passes through to Google (Zen's own codenames share the naming
// space, so the image-name match alone would claim a model Google never
// served). An aggregator serving the same model id over its own dialect
// answers for itself, and the flag it advertises is its own claim.
func googleServedImageModel(providerType, modelID string) bool {
	if !isGoogleImageGenModel(modelID) {
		return false
	}
	switch providerType {
	case "google", "vertex-express":
		return true
	case "opencode-zen":
		return strings.HasPrefix(strings.ToLower(modelID), "gemini-")
	}
	return false
}

// attachmentImpliesVision reports whether an attachment claim is corroborated
// by the declared input modalities.
//
// Vision means image specifically (capsInputFlags maps it that way), so audio,
// video and pdf inputs are not evidence for it. Accepting them would grant
// vision to models that take a document but refuse a picture, and because
// unionCapsIntoInput derives modalities back out of the flags, it would also
// append "image" to their stored input array.
//
// An absent input list is no evidence either way, so the attachment flag stands
// alone there rather than silently losing vision on a models.dev entry that
// simply carries no modality data.
func attachmentImpliesVision(input []string) bool {
	if len(input) == 0 {
		return true
	}
	return slices.Contains(input, "image")
}

// fillModalities marshals mods into *dst when *dst is currently empty,
// reporting whether it did.
func fillModalities(dst *string, mods []string) bool {
	if (*dst != "" && *dst != "[]") || len(mods) == 0 {
		return false
	}
	b, _ := json.Marshal(mods)
	*dst = string(b)
	return true
}

// EnrichModel fills gaps in a model.Model using models.dev data.
// It only overwrites fields that are empty/zero (never replaces existing data).
// providerType (a stored provider_type string) selects the canonical models.dev
// provider entry to consult first; pass "" to use only the cross-provider
// index. Returns true if at least one field was enriched.
func (c *ModelsDevCache) EnrichModel(m *model.Model, providerType string) bool {
	if c == nil {
		return false
	}

	spec := c.lookupForProvider(providerType, m.ModelID)
	if spec == nil && m.Name != "" && m.Name != m.ModelID {
		// Deployment-based providers (Azure) invoke by user-chosen alias but
		// record the underlying base-model name in Name, which is matched on
		// when the alias misses the catalog.
		spec = c.lookupForProvider(providerType, m.Name)
	}
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

	// Numeric fields: only set if nil.
	enriched = fillIfEmpty(&m.ContextLength, spec.Limit.Context) || enriched
	enriched = fillIfEmpty(&m.MaxOutputTokens, spec.Limit.Output) || enriched
	enriched = fillIfEmpty(&m.InputPricePerMillion, spec.Cost.Input) || enriched
	enriched = fillIfEmpty(&m.OutputPricePerMillion, spec.Cost.Output) || enriched
	if spec.Cost.CacheRead != nil {
		enriched = fillIfEmpty(&m.InputPricePerMillionCacheHit, *spec.Cost.CacheRead) || enriched
	}

	// Capabilities: only set individual fields if they're currently false.
	enriched = mergeSpecCapabilities(spec, &caps) || enriched
	enriched = clearRefusedCapabilities(providerType, m.ModelID, &caps) || enriched

	// Modality arrays: only set if currently empty. The modality *class* is
	// not set here; NormalizeModelClassification derives it from the arrays
	// after enrichment.
	enriched = fillModalities(&m.InputModalities, spec.Modalities.Input) || enriched
	enriched = fillModalities(&m.OutputModalities, spec.Modalities.Output) || enriched

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

// EnrichModels enriches a batch of models using models.dev data. providerType
// is the provider_type string of the provider the models were discovered
// on (see EnrichModel). Returns the number of models that were enriched (had
// at least one field filled).
func (c *ModelsDevCache) EnrichModels(models []*model.Model, providerType string) int {
	if c == nil {
		reportUnpricedModels(models)
		return 0
	}
	count := 0
	for _, m := range models {
		if c.EnrichModel(m, providerType) {
			count++
		}
	}
	reportUnpricedModels(models)
	return count
}

// reportUnpricedModels logs any model that finished discovery with no per-token
// price on either side.
//
// The embedded catalogs hold overrides only, so a model models.dev does not
// know yields no price at all. Such a model still works; it just meters at
// zero, which is invisible until someone reconciles a bill. Naming it here
// turns that into something an operator can see and fix by adding a catalog
// override.
func reportUnpricedModels(models []*model.Model) {
	var unpriced []string
	for _, m := range models {
		if m == nil || !m.Enabled {
			continue
		}
		// Free tiers are legitimately zero, so only a wholly absent price counts.
		if m.InputPricePerMillion == nil && m.OutputPricePerMillion == nil {
			unpriced = append(unpriced, m.ModelID)
		}
	}
	if len(unpriced) == 0 {
		return
	}
	debuglog.Warn("discovery: models have no pricing from catalog or models.dev; they will meter at zero",
		"count", len(unpriced), "models", strings.Join(unpriced, ","))
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

// looksLikeDateOrVersion checks whether a suffix looks like a date or version stamp.
// It accepts patterns like "2024-08-06", "20240806", "2024", or long numeric strings
// (4+ digits). It rejects strings that contain non-date-like segments such as
// "search-api", "mini", or other model variant identifiers.
func looksLikeDateOrVersion(suffix string) bool {
	// Full date: "2024-08-06" or "20240806"
	if looksLikeDate(suffix) {
		return true
	}

	parts := strings.Split(suffix, "-")

	// Single numeric segment: "2024" or "20240806" or "20250514"
	if len(parts) == 1 {
		return isNumeric(parts[0]) && len(parts[0]) >= 4
	}

	// Two segments: "2024-08" or similar year-month patterns
	if len(parts) == 2 {
		return isNumeric(parts[0]) && len(parts[0]) == 4 && isNumeric(parts[1]) && len(parts[1]) <= 2
	}

	// Three segments: "2024-08-06" already caught by looksLikeDate above.
	// Also accepts compact numeric segments like "2024-8-6" (all numeric,
	// first is 4-digit year). Non-numeric first segments (e.g. "v2024")
	// are rejected by isNumeric.
	if len(parts) == 3 && isNumeric(parts[0]) && len(parts[0]) == 4 {
		return true
	}

	return false
}
