package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/settings"
	"github.com/user/llm-proxy/internal/util"
	"github.com/user/llm-proxy/internal/virtualkey"
)

type contextKey string

const virtualKeyNameKey contextKey = "virtual_key_name"

type Handler struct {
	cfg            *config.Config
	providerRepo   *provider.Repository
	modelRepo      *model.Repository
	dbPool         *pgxpool.Pool
	virtualKeyRepo *virtualkey.Repository
	failoverRepo   *failover.Repository
	settingsRepo   *settings.Repository
}

func NewHandler(
	cfg *config.Config,
	providerRepo *provider.Repository,
	modelRepo *model.Repository,
	dbPool *pgxpool.Pool,
	virtualKeyRepo *virtualkey.Repository,
	failoverRepo *failover.Repository,
	settingsRepo *settings.Repository,
) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: virtualKeyRepo,
		failoverRepo:   failoverRepo,
		settingsRepo:   settingsRepo,
	}
}

type modelCandidate struct {
	model    *model.Model
	provider *provider.Provider
	apiKey   string
}

func (h *Handler) resolveCandidates(ctx context.Context, modelID string) ([]modelCandidate, float64, error) {
	modelLookupStart := time.Now()

	allModels, err := h.modelRepo.GetByModelID(ctx, modelID)
	if err != nil {
		return nil, 0, err
	}
	if len(allModels) == 0 {
		return nil, 0, nil
	}

	modelLookupMs := float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	fg, fgErr := h.failoverRepo.GetByModel(ctx, modelID)

	if fgErr == nil && len(fg.PriorityOrder) > 0 {
		candidates := make([]modelCandidate, 0, len(fg.PriorityOrder))
		for _, modelUUID := range fg.PriorityOrder {
			m, err := h.modelRepo.Get(ctx, modelUUID)
			if err != nil || !m.Enabled || !m.ProviderEnabled {
				continue
			}
			prov, err := h.providerRepo.Get(ctx, m.ProviderID)
			if err != nil || !prov.Enabled {
				continue
			}
			apiKey, err := auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
			if err != nil {
				continue
			}
			candidates = append(candidates, modelCandidate{model: m, provider: prov, apiKey: apiKey})
		}
		return candidates, modelLookupMs, nil
	}

	candidates := make([]modelCandidate, 0, len(allModels))
	for _, m := range allModels {
		prov, err := h.providerRepo.Get(ctx, m.ProviderID)
		if err != nil || !prov.Enabled {
			continue
		}
		apiKey, err := auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
		if err != nil {
			continue
		}
		candidates = append(candidates, modelCandidate{model: m, provider: prov, apiKey: apiKey})
	}
	return candidates, modelLookupMs, nil
}

func (h *Handler) shouldFailover(statusCode int) bool {
	if statusCode >= 500 {
		return true
	}
	if statusCode == 429 {
		return h.settingsRepo.GetBool(context.Background(), "failover_on_rate_limit", true)
	}
	return false
}

type failoverAttemptResult struct {
	provider      *provider.Provider
	statusCode    int
	errorMessage  string
	respBody      []byte
	streamBuf     *bytes.Buffer
	usage         *Usage
	promptTokens  int
	compTokens    int
	isStream      bool
}

type ChatCompletionRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream,omitempty"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        Message `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (h *Handler) Register(r chi.Router) {
	r.Use(h.ProxyKeyMiddleware)

	r.Get("/models", h.ListModels)
	r.Post("/chat/completions", h.ChatCompletions)
}

func (h *Handler) ProxyKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		token := ""
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		} else {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		if len(token) >= 3 && token[:3] == "sk-" {
			keyHash := virtualkey.Hash(token)
			vk, err := h.virtualKeyRepo.FindByKeyHash(r.Context(), keyHash)
			if err != nil {
				http.Error(w, "Invalid virtual key", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if len(token) < 5 || token[:5] != "llmp_" {
			http.Error(w, "Invalid proxy key format", http.StatusUnauthorized)
			return
		}

		keyHash := auth.HashProxyKey(token)

		query := `SELECT id FROM proxy_keys WHERE key_hash = $1`
		var keyID string
		err := h.dbPool.QueryRow(r.Context(), query, keyHash).Scan(&keyID)
		if err != nil {
			http.Error(w, "Invalid proxy key", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.ListEnabled(r.Context())
	if err != nil {
		http.Error(w, "failed to list models", http.StatusInternalServerError)
		return
	}

	seen := make(map[string]bool)
	openAIModels := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		if seen[m.ModelID] {
			continue
		}
		seen[m.ModelID] = true

		ownedBy := m.OwnedBy
		if ownedBy == "" {
			ownedBy = m.ProviderName
		}

		item := map[string]interface{}{
			"id":      m.ModelID,
			"object":  "model",
			"created": m.CreatedAt.Unix(),
			"owned_by": ownedBy,
		}

		if m.ContextLength != nil {
			item["context_length"] = *m.ContextLength
		}
		if m.MaxOutputTokens != nil {
			item["max_output_tokens"] = *m.MaxOutputTokens
		}
		if m.DisplayName != "" {
			item["name"] = m.DisplayName
		} else if m.Name != "" {
			item["name"] = m.Name
		}
		if m.Description != "" {
			item["description"] = m.Description
		}
		if m.Modality != "" {
			item["modality"] = m.Modality
		}
		if m.InputPricePerMillion != nil {
			item["input_price_per_million"] = *m.InputPricePerMillion
		}
		if m.OutputPricePerMillion != nil {
			item["output_price_per_million"] = *m.OutputPricePerMillion
		}

		openAIModels = append(openAIModels, item)
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   openAIModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()
	parseMs := float64(time.Since(startTime).Microseconds()) / 1000.0

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	candidates, modelLookupMs, err := h.resolveCandidates(r.Context(), req.Model)
	if err != nil {
		http.Error(w, "failed to resolve model", http.StatusInternalServerError)
		return
	}
	if len(candidates) == 0 {
		http.Error(w, "model not found or disabled", http.StatusNotFound)
		return
	}

	var proxyReqBody []byte
	if req.Stream {
		var raw map[string]interface{}
		if json.Unmarshal(bodyBytes, &raw) == nil {
			raw["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
			if b, err := json.Marshal(raw); err == nil {
				proxyReqBody = b
			}
		}
	}
	if proxyReqBody == nil {
		proxyReqBody = bodyBytes
	}

	vkName := ""
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName = v.(string)
	}

	failoverTimeout := h.settingsRepo.GetDuration(context.Background(), "failover_timeout", 10*time.Second)
	maxRateLimitRetries := 0
	if v, err := strconv.Atoi(h.settingsRepo.GetWithDefault(context.Background(), "failover_retries_on_rate_limit", "0")); err == nil {
		maxRateLimitRetries = v
	}

	var lastErr string
	for attempt, candidate := range candidates {
		providerLookupMs := float64(time.Since(startTime).Microseconds()) / 1000.0

		keyDecryptStart := time.Now()
		apiKey := candidate.apiKey
		keyDecryptMs := float64(time.Since(keyDecryptStart).Microseconds()) / 1000.0

		proxyOverhead := float64(time.Since(startTime).Microseconds()) / 1000.0
		targetURL := util.SanitizeBaseURL(candidate.provider.BaseURL) + "/chat/completions"

		proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(proxyReqBody))
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
			continue
		}
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
		proxyReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: failoverTimeout}
		resp, err := client.Do(proxyReq)
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
			continue
		}

		if h.shouldFailover(resp.StatusCode) && attempt < len(candidates)-1 {
			io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)

			if resp.StatusCode == 429 && maxRateLimitRetries > 0 {
				retried := false
				for retry := 0; retry < maxRateLimitRetries; retry++ {
					retryReq, retryErr := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(proxyReqBody))
					if retryErr != nil {
						break
					}
					retryReq.Header.Set("Authorization", "Bearer "+apiKey)
					retryReq.Header.Set("Content-Type", "application/json")
					retryResp, retryErr := client.Do(retryReq)
					if retryErr != nil {
						continue
					}
					if retryResp.StatusCode == 429 {
						retryResp.Body.Close()
						continue
					}
					if retryResp.StatusCode >= 500 {
						retryResp.Body.Close()
						break
					}
					resp = retryResp
					retried = true
					break
				}
				if !retried {
					continue
				}
			} else {
				continue
			}
		}

        // Compute VK id from VK name if available
        vkID := ""
        if vkName != "" {
            // best-effort lookup of VK id by name
            var id string
            errVK := h.dbPool.Pool().QueryRow(r.Context(), `SELECT id FROM virtual_keys WHERE name = $1 ORDER BY created_at DESC LIMIT 1`, vkName).Scan(&id)
            if errVK == nil {
                vkID = id
            }
        }
        if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			errMsg := string(body)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
            h.dbPool.Exec(r.Context(), `
                INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, duration_ms, proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, error_message, streaming, virtual_key_name, virtual_key_id, failover_attempt)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
                candidate.provider.ID, req.Model, generateRequestHash(), generateRequestHash(), resp.StatusCode,
                float64(time.Since(startTime).Microseconds())/1000.0,
                proxyOverhead,
                parseMs, modelLookupMs, providerLookupMs, keyDecryptMs,
                errMsg, req.Stream, vkName, vkID, attempt,
            )
			http.Error(w, fmt.Sprintf("provider error: %s", string(body)), resp.StatusCode)
			return
		}

		defer resp.Body.Close()

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")

			var buf bytes.Buffer
			tee := io.TeeReader(resp.Body, &buf)
			io.Copy(w, tee)

			totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
			usage := extractStreamingUsage(buf.String())
			var promptTokens, completionTokens int
			if usage != nil {
				promptTokens = usage.PromptTokens
				completionTokens = usage.CompletionTokens
			}
			var tps float64
			if completionTokens > 0 && totalDuration > 0 {
				tps = float64(completionTokens) / float64(totalDuration) * 1000
			}

			h.dbPool.Exec(r.Context(), `
				INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, duration_ms, proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, ttft_ms, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, failover_attempt)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
				candidate.provider.ID, req.Model, generateRequestHash(), generateRequestHash(), resp.StatusCode,
				totalDuration,
				proxyOverhead,
				parseMs, modelLookupMs, providerLookupMs, keyDecryptMs,
				totalDuration, tps, promptTokens, completionTokens, true, vkName, attempt,
			)

			if usage != nil && usage.TotalTokens > 0 {
				authToken := r.Header.Get("Authorization")
				if len(authToken) > 7 && authToken[:7] == "Bearer " {
					bearer := authToken[7:]
					if len(bearer) >= 3 && bearer[:3] == "sk-" {
						vkHash := virtualkey.Hash(bearer)
						h.virtualKeyRepo.AddTokens(r.Context(), vkHash, usage.TotalTokens)
					}
				}
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			var chatResp ChatCompletionResponse
			if err := json.NewDecoder(resp.Body).Decode(&chatResp); err == nil {
				totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
				var tps float64
				if chatResp.Usage.CompletionTokens > 0 && totalDuration > 0 {
					tps = float64(chatResp.Usage.CompletionTokens) / float64(totalDuration) * 1000
				}

				_, logErr := h.dbPool.Exec(r.Context(), `
					INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, duration_ms, proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, ttft_ms, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, failover_attempt)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
					candidate.provider.ID, req.Model, generateRequestHash(), generateRequestHash(), resp.StatusCode,
					totalDuration,
					proxyOverhead,
					parseMs, modelLookupMs, providerLookupMs, keyDecryptMs,
					totalDuration, tps, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, false, vkName, attempt,
				)
				if logErr != nil {
					fmt.Printf("Proxy log insert failed: %v\n", logErr)
				}

				authToken := r.Header.Get("Authorization")
				if len(authToken) > 7 && authToken[:7] == "Bearer " {
					bearer := authToken[7:]
					if len(bearer) >= 3 && bearer[:3] == "sk-" {
						vkHash := virtualkey.Hash(bearer)
						totalTokens := chatResp.Usage.PromptTokens + chatResp.Usage.CompletionTokens
						h.virtualKeyRepo.AddTokens(r.Context(), vkHash, totalTokens)
					}
				}
			}

			json.NewEncoder(w).Encode(chatResp)
		}

		return
	}

	h.dbPool.Exec(r.Context(), `
		INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, duration_ms, proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, error_message, streaming, virtual_key_name, failover_attempt)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		uuid.Nil, req.Model, generateRequestHash(), generateRequestHash(), 502,
		float64(time.Since(startTime).Microseconds())/1000.0,
		float64(time.Since(startTime).Microseconds())/1000.0,
		parseMs, modelLookupMs, 0, 0,
		fmt.Sprintf("all providers failed: %s", lastErr), req.Stream, vkName, len(candidates)-1,
	)
	http.Error(w, fmt.Sprintf("all providers failed for model %s", req.Model), http.StatusBadGateway)
}

func extractStreamingUsage(data string) *Usage {
	scanner := bufio.NewScanner(strings.NewReader(data))
	var lastUsage *Usage
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Usage *Usage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) == nil && chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}
	return lastUsage
}

func generateRequestHash() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
