package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The dashboard's Test button: how one probe request is parsed and how each of
// its three outcomes is written to request_logs. Split out of models.go, which
// had reached the size ceiling; the probe's own bookkeeping is a self-contained
// concern and the CRUD handlers do not read it.

// buildTestRerankRequest is the rerank probe: the Cohere-style body every
// rerank provider accepts, against the route the proxy's /v1/rerank forwards
// to (a Cohere base is redirected to its native /v2/rerank by
// BuildProviderTargetURL). One document and top_n 1 keep it one search unit.
func buildTestRerankRequest(modelID, baseURL, providerType string) (body []byte, targetURL string) {
	body, _ = json.Marshal(map[string]any{
		"model":     modelID,
		"query":     "ping",
		"documents": []string{"ping"},
		"top_n":     1,
	})
	return body, util.BuildProviderTargetURL(baseURL, providerType, "/rerank")
}

// doTestRerankRequest sends the rerank probe as a plain POST. The chat
// self-heal executor is not used: its 400 retry rewrites chat parameters the
// rerank body does not carry.
func (h *Handler) doTestRerankRequest(ctx context.Context, providerType, targetURL, apiKey string, body []byte) (*http.Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	if h.testModelTransport != nil {
		client.Transport = h.testModelTransport
	}
	if h.testModelCheckRedirect != nil {
		client.CheckRedirect = h.testModelCheckRedirect
	}
	// #nosec G704 -- provider URL is admin-configured, not arbitrary user input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.SetProviderAuthHeaders(req, providerType, apiKey)
	req.Header.Set("Content-Type", "application/json")
	// #nosec G704 -- provider URL is admin-configured, not arbitrary user input
	return client.Do(req)
}

// countRankedResults reads how many ranked documents a rerank probe answer
// carries, under whichever key the provider uses: `results` (Cohere, Jina,
// local rerankers), `data` (Voyage) or a bare top-level list
// (text-embeddings-inference). The documents themselves are never read.
func countRankedResults(respBody []byte) int {
	trimmed := bytes.TrimSpace(respBody)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var list []json.RawMessage
		if json.Unmarshal(trimmed, &list) == nil {
			return len(list)
		}
		return 0
	}
	var out struct {
		Results []json.RawMessage `json:"results"`
		Data    []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(respBody, &out) != nil {
		return 0
	}
	return max(len(out.Results), len(out.Data))
}

// billedSearchUnits reads how many search units a rerank probe answer was
// billed for (Cohere's meta.billed_units.search_units), the same member the
// proxy meters live traffic by; zero for providers that bill per token.
func billedSearchUnits(respBody []byte) int {
	var envelope struct {
		Meta struct {
			BilledUnits json.RawMessage `json:"billed_units"`
		} `json:"meta"`
	}
	if json.Unmarshal(respBody, &envelope) != nil || !util.JSONMemberSet(envelope.Meta.BilledUnits) {
		return 0
	}
	var billed struct {
		SearchUnits int `json:"search_units"`
	}
	if util.DecodeCountsTolerant(envelope.Meta.BilledUnits, &billed) != nil {
		return 0
	}
	return util.ClampTokenCount(billed.SearchUnits)
}

// probeEndpointType is the request_logs.endpoint_type a probe row carries:
// the family the probe was sent on.
func probeEndpointType(m *model.Model) string {
	if m.Modality == "rerank" {
		return "rerank"
	}
	return "chat"
}

// probeCost prices a probe the way the proxy prices live traffic: a rerank
// from the units it was billed, anything else from its tokens. nil when the
// model carries no usable price, so the row reads unknown rather than free.
func probeCost(m *model.Model, searchUnits, promptTokens, completionTokens int) any {
	var cost float64
	var ok bool
	if m.Modality == "rerank" && searchUnits > 0 {
		cost, ok = m.SearchCostUSD(searchUnits)
	} else {
		cost, ok = m.CostUSD(model.Usage{Prompt: promptTokens, Completion: completionTokens})
	}
	if !ok {
		return nil
	}
	return cost
}

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

// insertTestModelLog writes the one request_logs row a model test produces.
// Every probe outcome shares the same columns; the outcome only picks the
// values. A nil errMsg / tps / token count writes SQL NULL, which is what an
// outcome that never produced the figure means.
func (h *Handler) insertTestModelLog(ctx context.Context, m *model.Model, reqHash string, statusCode int,
	durationMs, responseHeaderMs, proxyOverheadMs, keyDecryptMs float64,
	errMsg, tps, promptTokens, completionTokens any, state, clientIP string,
	searchUnits int, cost any,
) {
	const logQuery = `
		INSERT INTO request_logs (
			provider_id, model_id, request_hash, status_code,
			latency_ms, duration_ms, response_header_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
			error_message, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state, client_ip,
			endpoint_type, search_units, cost_usd
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, 0, 0, 0, 0, $9, 0, 0, $10, $11, $12, $13, false, 'internal', NULL, 0, $14, $15, $16, $17, $18)
	`
	if _, err := h.dbPool.Pool().Exec(ctx, logQuery,
		m.ProviderID, m.ModelID, reqHash, statusCode,
		durationMs, durationMs, responseHeaderMs,
		proxyOverheadMs, keyDecryptMs,
		errMsg, tps, promptTokens, completionTokens, state, textOrNull(clientIP),
		probeEndpointType(m), searchUnits, cost,
	); err != nil {
		debuglog.Error("admin: TestModel log insert failed", "error", err)
	}
}

// logTestModelRequestError records a failed test request (the upstream call
// never completed) as a 502 "failed" request_logs row. No response arrived, so
// the token figures stay NULL rather than reading as a measured zero.
func (h *Handler) logTestModelRequestError(ctx context.Context, m *model.Model, reqHash string, durationMs, proxyOverheadMs, keyDecryptMs float64, errMsg, clientIP string) {
	h.insertTestModelLog(ctx, m, reqHash, 502, durationMs, 0, proxyOverheadMs, keyDecryptMs,
		errMsg, nil, nil, nil, "failed", clientIP, 0, nil)
}

// logTestModelHTTPError records a test request that reached the upstream but
// returned a non-200 status as a "failed" request_logs row.
func (h *Handler) logTestModelHTTPError(ctx context.Context, m *model.Model, reqHash string, statusCode int, durationMs, proxyOverheadMs, keyDecryptMs float64, errMsg, clientIP string) {
	h.insertTestModelLog(ctx, m, reqHash, statusCode, durationMs, 0, proxyOverheadMs, keyDecryptMs,
		errMsg, 0, 0, 0, "failed", clientIP, 0, nil)
}

// logTestModelEmptyAnswer records a 200 that carried no answer (a rerank
// probe with no ranked results) as a "failed" row, the verdict the live path
// reaches for the same body. The provider billed it all the same, so the
// units and their cost are kept.
func (h *Handler) logTestModelEmptyAnswer(ctx context.Context, m *model.Model, reqHash string, durationMs, proxyOverheadMs, keyDecryptMs float64, errMsg, clientIP string, searchUnits int, cost any) {
	h.insertTestModelLog(ctx, m, reqHash, http.StatusOK, durationMs, durationMs, proxyOverheadMs, keyDecryptMs,
		errMsg, 0, 0, 0, "failed", clientIP, searchUnits, cost)
}

// logTestModelCompleted records a successful (HTTP 200) test request as a
// "completed" request_logs row. For a non-streaming test, response_header_ms
// equals total duration (no separate streaming phase) and ttft_ms is stored as
// 0 to indicate non-streaming.
func (h *Handler) logTestModelCompleted(ctx context.Context, m *model.Model, reqHash string, statusCode int, durationMs, proxyOverheadMs, keyDecryptMs, tps float64, promptTokens, completionTokens int, clientIP string, searchUnits int, cost any) {
	h.insertTestModelLog(ctx, m, reqHash, statusCode, durationMs, durationMs, proxyOverheadMs, keyDecryptMs,
		nil, tps, promptTokens, completionTokens, "completed", clientIP, searchUnits, cost)
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
