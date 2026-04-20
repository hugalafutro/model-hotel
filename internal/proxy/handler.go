package proxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"context"
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
	discovery      *provider.DiscoveryService
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
		discovery:      provider.NewDiscoveryService(),
		virtualKeyRepo: virtualKeyRepo,
	}
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

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Use(h.ProxyKeyMiddleware)

		r.Get("/models", h.ListModels)
		r.Post("/chat/completions", h.ChatCompletions)
	})
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
	models, err := h.modelRepo.List(r.Context(), nil)
	if err != nil {
		http.Error(w, "failed to list models", http.StatusInternalServerError)
		return
	}

	openAIModels := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		openAIModels = append(openAIModels, map[string]interface{}{
			"id":      m.ModelID,
			"object":  "model",
			"created": m.CreatedAt.Unix(),
			"owned_by": m.ProviderID.String(),
		})
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   openAIModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}

	models, err := h.modelRepo.List(r.Context(), nil)
	if err != nil {
		http.Error(w, "failed to query models", http.StatusInternalServerError)
		return
	}

	var targetModel *model.Model
	for _, m := range models {
		if m.ModelID == req.Model && m.Enabled {
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

	proxyReqBody, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "failed to marshal request", http.StatusInternalServerError)
		return
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(proxyReqBody))
	if err != nil {
		http.Error(w, "failed to create proxy request", http.StatusInternalServerError)
		return
	}

	proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "failed to call provider", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	latency := time.Since(startTime).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("provider error: %s", string(body)), resp.StatusCode)
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		io.Copy(w, resp.Body)
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
			overhead := totalDuration - latency
			if overhead < 0 {
				overhead = 0
			}

			vkName := ""
			if v := r.Context().Value(virtualKeyNameKey); v != nil {
				vkName = v.(string)
			}

			prompt := extractPrompt(req.Messages)

			query := `
				INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, latency_ms, duration_ms, ttft_ms, proxy_overhead_ms, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, prompt)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			`
			_, logErr := h.dbPool.Exec(r.Context(), query,
				prov.ID, req.Model, reqHash, reqHash, resp.StatusCode, totalDuration, totalDuration, totalDuration, overhead, tps,
				chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, req.Stream, vkName, prompt,
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

func generateRequestHash() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func extractPrompt(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	var content string
	switch v := last.Content.(type) {
	case string:
		content = v
	default:
		b, _ := json.Marshal(v)
		content = string(b)
	}
	if len(content) > 500 {
		content = content[:497] + "..."
	}
	return content
}
