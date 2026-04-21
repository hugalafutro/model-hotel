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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
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
}

func NewHandler(
	cfg *config.Config,
	providerRepo *provider.Repository,
	modelRepo *model.Repository,
	dbPool *pgxpool.Pool,
	virtualKeyRepo *virtualkey.Repository,
) *Handler {
	return &Handler{
		cfg:            cfg,
		providerRepo:   providerRepo,
		modelRepo:      modelRepo,
		dbPool:         dbPool,
		virtualKeyRepo: virtualKeyRepo,
	}
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

	openAIModels := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
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

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	models, err := h.modelRepo.ListEnabled(r.Context())
	if err != nil {
		http.Error(w, "failed to query models", http.StatusInternalServerError)
		return
	}

	var targetModel *model.Model
	for _, m := range models {
		if m.ModelID == req.Model {
			targetModel = m
			break
		}
	}

	if targetModel == nil {
		http.Error(w, "model not found or disabled", http.StatusNotFound)
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), targetModel.ProviderID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusInternalServerError)
		return
	}

	apiKey, err := auth.Decrypt(prov.EncryptedKey, prov.KeyNonce, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to decrypt API key", http.StatusInternalServerError)
		return
	}

	targetURL := util.SanitizeBaseURL(prov.BaseURL) + "/chat/completions"

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

	proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(proxyReqBody))
	if err != nil {
		http.Error(w, "failed to create proxy request", http.StatusInternalServerError)
		return
	}

	proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")

	proxyOverhead := time.Since(startTime).Milliseconds()

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "failed to call provider", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	vkName := ""
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName = v.(string)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		reqHash := generateRequestHash()
		totalDuration := time.Since(startTime).Milliseconds()
		errMsg := string(body)
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		h.dbPool.Exec(r.Context(), `
			INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, latency_ms, duration_ms, error_message, streaming, virtual_key_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			prov.ID, req.Model, reqHash, reqHash, resp.StatusCode, totalDuration, totalDuration, errMsg, req.Stream, vkName,
		)

		http.Error(w, fmt.Sprintf("provider error: %s", string(body)), resp.StatusCode)
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		var buf bytes.Buffer
		tee := io.TeeReader(resp.Body, &buf)
		io.Copy(w, tee)

		totalDuration := time.Since(startTime).Milliseconds()
		reqHash := generateRequestHash()

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
			INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, latency_ms, duration_ms, ttft_ms, proxy_overhead_ms, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			prov.ID, req.Model, reqHash, reqHash, resp.StatusCode, totalDuration, totalDuration, totalDuration, proxyOverhead, tps,
			promptTokens, completionTokens, true, vkName,
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
			reqHash := generateRequestHash()
			w.Header().Set("X-Request-ID", reqHash)

			totalDuration := time.Since(startTime).Milliseconds()
			var tps float64
			if chatResp.Usage.CompletionTokens > 0 && totalDuration > 0 {
				tps = float64(chatResp.Usage.CompletionTokens) / float64(totalDuration) * 1000
			}

			query := `
				INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, latency_ms, duration_ms, ttft_ms, proxy_overhead_ms, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			`
			_, logErr := h.dbPool.Exec(r.Context(), query,
				prov.ID, req.Model, reqHash, reqHash, resp.StatusCode, totalDuration, totalDuration, totalDuration, proxyOverhead, tps,
				chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, false, vkName,
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
