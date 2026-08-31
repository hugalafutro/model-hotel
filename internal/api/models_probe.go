package api

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The dashboard's Test button: how one probe request is parsed and how each of
// its three outcomes is written to request_logs. Split out of models.go, which
// had reached the size ceiling; the probe's own bookkeeping is a self-contained
// concern and the CRUD handlers do not read it.

// parseTestModelResponse extracts the assistant content and computes
// tokens-per-second from a successful test response body. A parse failure is
// logged and yields empty content / zero usage.
func parseTestModelResponse(respBody []byte, duration int64) (content string, tps float64, promptTokens, completionTokens int) {
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	// util.DecodeCounts: a count the provider quoted or wrote with a fraction on
	// it is still a count, and a plain int field met neither — so the dashboard's
	// probe reported 0/0 tokens and a tps of zero for a model that answered.
	// This decode already logged and carried on, so the ANSWER was never at risk
	// here; the counts were.
	if err := util.DecodeCounts(respBody, &chatResp); err != nil {
		debuglog.Debug("admin: failed to parse test model chat response", "error", err)
	}

	if len(chatResp.Choices) > 0 {
		content = chatResp.Choices[0].Message.Content
	}

	// The same bound the proxy holds every provider figure to: these two land
	// in the request log's int4 token columns, where a negative skews the
	// stats and an overflow fails the INSERT and loses the row.
	promptTokens = util.ClampTokenCount(chatResp.Usage.PromptTokens)
	completionTokens = util.ClampTokenCount(chatResp.Usage.CompletionTokens)

	if completionTokens > 0 && duration > 0 {
		tps = float64(completionTokens) / float64(duration) * 1000
	}

	return content, tps, promptTokens, completionTokens
}

// logTestModelRequestError records a failed test request (the upstream call
// never completed) as a 502 "failed" request_logs row.
func (h *Handler) logTestModelRequestError(ctx context.Context, m *model.Model, reqHash string, durationMs, proxyOverheadMs, keyDecryptMs float64, errMsg, clientIP string) {
	logQuery := `
		INSERT INTO request_logs (
			provider_id, model_id, request_hash, status_code,
			latency_ms, duration_ms, response_header_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
			error_message, streaming, virtual_key_name, virtual_key_id, failover_attempt, state, client_ip
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
	`
	_, logErr := h.dbPool.Pool().Exec(ctx, logQuery,
		m.ProviderID, m.ModelID, reqHash, 502,
		durationMs, durationMs, 0,
		proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
		errMsg, false, "internal", nil, 0, "failed", textOrNull(clientIP),
	)
	if logErr != nil {
		debuglog.Error("admin: TestModel log insert failed", "error", logErr)
	}
}

// logTestModelHTTPError records a test request that reached the upstream but
// returned a non-200 status as a "failed" request_logs row.
func (h *Handler) logTestModelHTTPError(ctx context.Context, m *model.Model, reqHash string, statusCode int, durationMs, proxyOverheadMs, keyDecryptMs float64, errMsg, clientIP string) {
	logQuery := `
		INSERT INTO request_logs (
			provider_id, model_id, request_hash, status_code,
			latency_ms, duration_ms, response_header_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
			error_message, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state, client_ip
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
	`
	_, logErr := h.dbPool.Pool().Exec(ctx, logQuery,
		m.ProviderID, m.ModelID, reqHash, statusCode,
		durationMs, durationMs, 0,
		proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
		errMsg, 0, 0, 0, false, "internal", nil, 0, "failed", textOrNull(clientIP),
	)
	if logErr != nil {
		debuglog.Error("admin: TestModel log insert failed", "error", logErr)
	}
}

// logTestModelCompleted records a successful (HTTP 200) test request as a
// "completed" request_logs row. For a non-streaming test, response_header_ms
// equals total duration (no separate streaming phase) and ttft_ms is stored as
// 0 to indicate non-streaming.
func (h *Handler) logTestModelCompleted(ctx context.Context, m *model.Model, reqHash string, statusCode int, durationMs, proxyOverheadMs, keyDecryptMs, tps float64, promptTokens, completionTokens int, clientIP string) {
	logQuery := `
		INSERT INTO request_logs (
			provider_id, model_id, request_hash, status_code,
			latency_ms, duration_ms, response_header_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
			tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state, client_ip
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
	`
	_, logErr := h.dbPool.Pool().Exec(ctx, logQuery,
		m.ProviderID, m.ModelID, reqHash, statusCode,
		durationMs, durationMs, durationMs,
		proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
		tps, promptTokens, completionTokens, false, "internal", nil, 0, "completed", textOrNull(clientIP),
	)
	if logErr != nil {
		debuglog.Error("admin: TestModel log insert failed", "error", logErr)
	}
}

// textOrNull maps "" to NULL so address-less rows look the same as rows
// predating the client_ip column.
func textOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// See util.BuildProviderTargetURL for URL construction and util.SetProviderAuthHeaders for auth.
