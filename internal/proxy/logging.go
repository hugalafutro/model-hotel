package proxy

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
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
	if err != nil {
		return err
	}
	events.Publish(events.Event{
		Type:     "request.started",
		Severity: "info",
		Message:  fmt.Sprintf("Request started: %s", logEntry.modelID),
		Metadata: map[string]interface{}{
			"request_id": logEntry.id,
			"model_id":   logEntry.modelID,
			"streaming":  logEntry.streaming,
			"state":      logEntry.state,
		},
	})
	return nil
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
		debuglog.Error("proxy: failed to update request log", "request_id", logEntry.id, "error", err)
	} else if tag.RowsAffected() == 0 {
		debuglog.Warn("proxy: updateRequestLog no rows affected", "request_id", logEntry.id)
	}

	// Publish request lifecycle event for terminal states
	if logEntry.state == "completed" || logEntry.state == "failed" {
		severity := "success"
		if logEntry.state == "failed" {
			severity = "warning"
		}
		msg := fmt.Sprintf("Request completed: %s", logEntry.modelID)
		if logEntry.state == "failed" && logEntry.errorMessage != "" {
			msg = fmt.Sprintf("Request failed: %s — %s", logEntry.modelID, logEntry.errorMessage)
			if len(msg) > 200 {
				for len(msg) > 200 {
					_, size := utf8.DecodeLastRuneInString(msg)
					msg = msg[:len(msg)-size]
				}
				msg = msg + "…"
			}
		}
		events.Publish(events.Event{
			Type:     "request.completed",
			Severity: severity,
			Message:  msg,
			Metadata: map[string]interface{}{
				"request_id":  logEntry.id,
				"model_id":    logEntry.modelID,
				"state":       logEntry.state,
				"status_code": logEntry.statusCode,
			},
		})
	}
}
