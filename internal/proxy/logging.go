package proxy

import (
	"context"
	"log"

	"github.com/google/uuid"
)

func (h *Handler) insertRequestLog(ctx context.Context, logEntry *requestLogData) error {
	logEntry.id = uuid.New().String()
	logEntry.requestHash = generateRequestHash()
	var vkID interface{}
	if logEntry.virtualKeyID != "" {
		vkID = logEntry.virtualKeyID
	}
	_, err := h.dbPool.Exec(ctx, `
		INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, virtual_key_id, failover_attempt, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		logEntry.id, logEntry.modelID, logEntry.requestHash, logEntry.streaming, logEntry.virtualKeyName, vkID, logEntry.failoverAttempt, logEntry.state,
	)
	return err
}

func (h *Handler) updateRequestLog(ctx context.Context, logEntry *requestLogData) {
	var providerID interface{}
	if logEntry.providerID != uuid.Nil {
		providerID = logEntry.providerID
	}
	logEntry.latencyMs = logEntry.durationMs - logEntry.proxyOverheadMs

	tag, err := h.dbPool.Exec(ctx, `
		UPDATE request_logs SET
			provider_id = $2,
			status_code = $3,
			duration_ms = $4,
			latency_ms = $19,
			proxy_overhead_ms = $5,
			parse_ms = $6,
			model_lookup_ms = $7,
			provider_lookup_ms = $8,
			key_decrypt_ms = $9,
			ttft_ms = $10,
			tokens_per_second = $11,
			tokens_prompt = $12,
			tokens_completion = $13,
			tokens_prompt_cache_hit = $14,
			tokens_prompt_cache_miss = $15,
			error_message = $16,
			failover_attempt = $17,
			state = $18
		WHERE id = $1`,
		logEntry.id, providerID, logEntry.statusCode, logEntry.durationMs,
		logEntry.proxyOverheadMs, logEntry.parseMs, logEntry.modelLookupMs, logEntry.providerLookupMs,
		logEntry.keyDecryptMs, logEntry.ttftMs, logEntry.tokensPerSecond, logEntry.tokensPrompt,
		logEntry.tokensCompletion, logEntry.tokensPromptCacheHit, logEntry.tokensPromptCacheMiss,
		logEntry.errorMessage, logEntry.failoverAttempt, logEntry.state, logEntry.latencyMs,
	)
	if err != nil {
		log.Printf("[proxy] error: failed to update request log %s: %v", logEntry.id, err)
	} else if tag.RowsAffected() == 0 {
		log.Printf("[proxy] warning: updateRequestLog no rows affected for log %s (may have been deleted)", logEntry.id)
	}
}
