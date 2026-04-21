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
const virtualKeyIDKey contextKey = "virtual_key_id"
const virtualKeyHashKey contextKey = "virtual_key_hash"

type requestLogData struct {
	id                 string
	providerID         uuid.UUID
	modelID            string
	requestHash        string
	providerName       string
	statusCode         int
	durationMs         float64
	proxyOverheadMs    float64
	parseMs            float64
	modelLookupMs      float64
	providerLookupMs   float64
	keyDecryptMs       float64
	ttftMs             float64
	tokensPerSecond    float64
	tokensPrompt       int
	tokensCompletion   int
	streaming          bool
	virtualKeyName     string
	virtualKeyID       string
	errorMessage      string
	failoverAttempt    int
}

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

		if token == "" {
			http.Error(w, "Invalid virtual key", http.StatusUnauthorized)
			return
		}

		keyHash := virtualkey.Hash(token)
		vk, err := h.virtualKeyRepo.FindByKeyHash(r.Context(), keyHash)
		if err != nil {
			http.Error(w, "Invalid virtual key", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), virtualKeyNameKey, vk.Name)
		ctx = context.WithValue(ctx, virtualKeyIDKey, vk.ID.String())
		ctx = context.WithValue(ctx, virtualKeyHashKey, keyHash)
		h.virtualKeyRepo.TouchLastUsed(context.Background(), keyHash)
		next.ServeHTTP(w, r.WithContext(ctx))
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

func (h *Handler) insertRequestLog(_ context.Context, log *requestLogData) error {
	log.id = uuid.New().String()
	log.requestHash = generateRequestHash()
	_, err := h.dbPool.Exec(context.Background(), `
		INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, virtual_key_id, failover_attempt)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		log.id, log.modelID, log.requestHash, log.streaming, log.virtualKeyName, log.virtualKeyID, log.failoverAttempt,
	)
	return err
}

func (h *Handler) updateRequestLog(_ context.Context, log *requestLogData) {
	h.dbPool.Exec(context.Background(), `
		UPDATE request_logs SET
			provider_id = $2,
			status_code = $3,
			duration_ms = $4,
			proxy_overhead_ms = $5,
			parse_ms = $6,
			model_lookup_ms = $7,
			provider_lookup_ms = $8,
			key_decrypt_ms = $9,
			ttft_ms = $10,
			tokens_per_second = $11,
			tokens_prompt = $12,
			tokens_completion = $13,
			error_message = $14,
			failover_attempt = $15
		WHERE id = $1`,
		log.id, log.providerID, log.statusCode, log.durationMs,
		log.proxyOverheadMs, log.parseMs, log.modelLookupMs, log.providerLookupMs,
		log.keyDecryptMs, log.ttftMs, log.tokensPerSecond, log.tokensPrompt,
		log.tokensCompletion, log.errorMessage, log.failoverAttempt,
	)
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft float64, vkHash string, attempt int) {
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	var promptTokens, completionTokens int
	var lastErrMsg string
	clientDisconnected := false

	for scanner.Scan() {
		line := scanner.Bytes()

		select {
		case <-r.Context().Done():
			clientDisconnected = true
			goto logUpdate
		default:
		}

		if canFlush {
			flusher.Flush()
		}
		w.Write(line)
		w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}

		if strings.HasPrefix(string(line), "data: ") {
			payload := strings.TrimPrefix(string(line), "data: ")
			if payload == "[DONE]" {
				break
			}
			var chunk struct {
				Usage *Usage   `json:"usage"`
				Error *struct{ Message string } `json:"error"`
			}
			if json.Unmarshal([]byte(payload), &chunk) == nil {
				if chunk.Usage != nil {
					promptTokens = chunk.Usage.PromptTokens
					completionTokens = chunk.Usage.CompletionTokens
				}
				if chunk.Error != nil {
					lastErrMsg = chunk.Error.Message
				}
			}
		}
	}

logUpdate:
	totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
	var tps float64
	if completionTokens > 0 && totalDuration > 0 {
		tps = float64(completionTokens) / float64(totalDuration) * 1000
	}

	errMsg := lastErrMsg
	if errMsg == "" && scanner.Err() != nil {
		errMsg = scanner.Err().Error()
	}
	if clientDisconnected {
		errMsg = "client disconnected"
	}

	logData.statusCode = resp.StatusCode
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = modelLookupMs
	logData.providerLookupMs = providerLookupMs
	logData.keyDecryptMs = keyDecryptMs
	logData.ttftMs = ttft
	logData.tokensPerSecond = tps
	logData.tokensPrompt = promptTokens
	logData.tokensCompletion = completionTokens
	logData.errorMessage = errMsg
	logData.failoverAttempt = attempt
	h.updateRequestLog(r.Context(), logData)

	if vkHash != "" && !clientDisconnected {
		totalTokens := promptTokens + completionTokens
		if err := h.virtualKeyRepo.AddTokens(r.Context(), vkHash, totalTokens); err != nil {
			fmt.Printf("AddTokens (stream) failed: %v\n", err)
		}
	}
}

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft float64, vkHash string, attempt int) {
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err == nil {
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		var tps float64
		if chatResp.Usage.CompletionTokens > 0 && totalDuration > 0 {
			tps = float64(chatResp.Usage.CompletionTokens) / float64(totalDuration) * 1000
		}

		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.ttftMs = ttft
		logData.tokensPerSecond = tps
		logData.tokensPrompt = chatResp.Usage.PromptTokens
		logData.tokensCompletion = chatResp.Usage.CompletionTokens
		logData.failoverAttempt = attempt
		h.updateRequestLog(r.Context(), logData)

		if vkHash != "" {
			totalTokens := chatResp.Usage.PromptTokens + chatResp.Usage.CompletionTokens
			if err := h.virtualKeyRepo.AddTokens(r.Context(), vkHash, totalTokens); err != nil {
				fmt.Printf("AddTokens (non-stream) failed: %v\n", err)
			}
		}

		json.NewEncoder(w).Encode(chatResp)
	} else {
		body, _ := io.ReadAll(resp.Body)
		errMsg := string(body)
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.ttftMs = ttft
		logData.errorMessage = fmt.Sprintf("response decode error: %s", errMsg)
		logData.failoverAttempt = attempt
		h.updateRequestLog(r.Context(), logData)
		http.Error(w, errMsg, resp.StatusCode)
	}
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

	vkName := ""
	var vkID string
	var vkHash string
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName = v.(string)
	}
	if v := r.Context().Value(virtualKeyIDKey); v != nil {
		vkID = v.(string)
	}
	if v := r.Context().Value(virtualKeyHashKey); v != nil {
		vkHash = v.(string)
	}

	logData := &requestLogData{
		modelID:        req.Model,
		streaming:      req.Stream,
		virtualKeyName: vkName,
		virtualKeyID:   vkID,
		failoverAttempt: 0,
	}
	if err := h.insertRequestLog(r.Context(), logData); err != nil {
		fmt.Printf("Failed to insert initial request log: %v\n", err)
	}

	candidates, modelLookupMs, err := h.resolveCandidates(r.Context(), req.Model)
	if err != nil {
		logData.statusCode = 500
		logData.errorMessage = "failed to resolve model"
		logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.parseMs = parseMs
		h.updateRequestLog(r.Context(), logData)
		http.Error(w, "failed to resolve model", http.StatusInternalServerError)
		return
	}
	if len(candidates) == 0 {
		logData.statusCode = 404
		logData.errorMessage = "model not found or disabled"
		logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		h.updateRequestLog(r.Context(), logData)
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

	var lastErr string
	for attempt, candidate := range candidates {
		logData.providerID = candidate.provider.ID
		providerLookupMs := float64(time.Since(startTime).Microseconds()) / 1000.0

		keyDecryptStart := time.Now()
		apiKey := candidate.apiKey
		keyDecryptMs := float64(time.Since(keyDecryptStart).Microseconds()) / 1000.0

		proxyOverhead := float64(time.Since(startTime).Microseconds()) / 1000.0
		targetURL := util.SanitizeBaseURL(candidate.provider.BaseURL) + "/chat/completions"

		failoverCtx := r.Context()
		proxyReq, err := http.NewRequestWithContext(failoverCtx, "POST", targetURL, bytes.NewReader(proxyReqBody))
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
			continue
		}
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
		proxyReq.Header.Set("Content-Type", "application/json")

		streamingClient := &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 0,
			},
		}
		resp, err := streamingClient.Do(proxyReq)
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
			continue
		}
		ttft := float64(time.Since(startTime).Microseconds()) / 1000.0

		hasMoreCandidates := attempt < len(candidates)-1
		shouldFailoverNow := h.shouldFailover(resp.StatusCode) && hasMoreCandidates

		if shouldFailoverNow {
			io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
			logData.failoverAttempt = attempt
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			errMsg := string(body)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
			logData.statusCode = resp.StatusCode
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.proxyOverheadMs = proxyOverhead
			logData.parseMs = parseMs
			logData.modelLookupMs = modelLookupMs
			logData.providerLookupMs = providerLookupMs
			logData.keyDecryptMs = keyDecryptMs
			logData.ttftMs = ttft
			logData.errorMessage = errMsg
			logData.failoverAttempt = attempt
			h.updateRequestLog(r.Context(), logData)
			http.Error(w, fmt.Sprintf("provider error: %s", string(body)), resp.StatusCode)
			return
		}

		if req.Stream {
			h.handleStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft, vkHash, attempt)
			return
		}

		h.handleNonStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft, vkHash, attempt)
		return
	}

	logData.providerID = uuid.Nil
	logData.statusCode = 502
	logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = logData.durationMs
	logData.parseMs = parseMs
	logData.modelLookupMs = modelLookupMs
	logData.errorMessage = fmt.Sprintf("all providers failed: %s", lastErr)
	logData.failoverAttempt = len(candidates) - 1
	h.updateRequestLog(r.Context(), logData)
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
