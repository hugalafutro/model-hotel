package proxy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func (h *Handler) insertRequestLog(_ context.Context, log *requestLogData) error {
	log.id = uuid.New().String()
	log.requestHash = generateRequestHash()
	_, err := h.dbPool.Exec(context.Background(), `
		INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, virtual_key_id, failover_attempt, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		log.id, log.modelID, log.requestHash, log.streaming, log.virtualKeyName, log.virtualKeyID, log.failoverAttempt, log.state,
	)
	return err
}

func (h *Handler) updateRequestLog(_ context.Context, log *requestLogData) {
	tag, err := h.dbPool.Exec(context.Background(), `
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
			tokens_prompt_cache_hit = $14,
			tokens_prompt_cache_miss = $15,
			error_message = $16,
			failover_attempt = $17,
			state = $18
		WHERE id = $1`,
		log.id, log.providerID, log.statusCode, log.durationMs,
		log.proxyOverheadMs, log.parseMs, log.modelLookupMs, log.providerLookupMs,
		log.keyDecryptMs, log.ttftMs, log.tokensPerSecond, log.tokensPrompt,
		log.tokensCompletion, log.tokensPromptCacheHit, log.tokensPromptCacheMiss,
		log.errorMessage, log.failoverAttempt, log.state,
	)
	if err != nil {
		fmt.Printf("Failed to update request log %s: %v\n", log.id, err)
	} else if tag.RowsAffected() == 0 {
		fmt.Printf("updateRequestLog: no rows affected for log %s (may have been deleted)\n", log.id)
	}
}
