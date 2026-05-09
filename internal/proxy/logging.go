package proxy

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// insertRequestLogAsync pre-generates the log ID and fires off the DB
// insert + SSE event in a goroutine so the handler is not blocked by the
// write. The ID is assigned synchronously so updateRequestLog can reference
// it later. If the insert fails, the error is logged but does not fail the
// request — the update will simply be a no-op.
func (h *Handler) insertRequestLogAsync(logEntry *requestLogData) {
	logEntry.id = uuid.New().String()
	logEntry.requestHash = generateRequestHash()
	logEntry.insertWg.Add(1)

	go func() {
		defer logEntry.insertWg.Done()
		defer func() {
			if r := recover(); r != nil {
				debuglog.Error("proxy: panic in insertRequestLog", "error", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

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
			debuglog.Error("proxy: failed to insert initial request log", "request_id", logEntry.id, "error", err)
			return
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
	}()
}

// WaitForInsert blocks until the async INSERT goroutine has completed (or
// timed out after 5 seconds). Callers should invoke this before
// updateRequestLog to guarantee the row exists in the database.
func (h *Handler) WaitForInsert(logEntry *requestLogData) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		logEntry.insertWg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		debuglog.Warn("proxy: timed out waiting for request log INSERT", "request_id", logEntry.id)
	}
}

func (h *Handler) updateRequestLog(ctx context.Context, logEntry *requestLogData) {
	// Ensure the async INSERT has completed before we try to UPDATE the row.
	h.WaitForInsert(logEntry)

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
			safe_dial_ms = $20,
			settings_read_ms = $21,
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
		logEntry.safeDialMs, logEntry.settingsReadMs,
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
				msg += "…"
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
