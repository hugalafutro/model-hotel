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

// updateLogOption configures updateRequestLog behavior.
type updateLogOption struct {
	// skipWaitForInsert skips the WaitForInsert call before the UPDATE.
	// Use for interim log writes on the streaming hot path where blocking
	// on the async INSERT would delay the first streamed byte. The final
	// log update (completed/failed state) should always wait.
	skipWaitForInsert bool
}

// insertRequestLogAsync pre-generates the log ID and fires off the DB
// insert in a goroutine so the handler is not blocked by the write. The ID
// is assigned synchronously so updateRequestLog can reference it later. If
// the insert fails, the error is logged but does not fail the request —
// the update will simply be a no-op.
//
// Note: The SSE "request.started" event is NOT published here because
// modelID may be empty when this is called (before body parsing). Call
// publishRequestStartedEvent after modelID is resolved.
func (h *Handler) insertRequestLogAsync(logEntry *requestLogData) {
	logEntry.id = uuid.New().String()
	logEntry.requestHash = generateRequestHash()

	// Skip DB operations when no pool is available (unit tests without DB).
	if h.dbPool == nil {
		return
	}

	logEntry.insertWg.Add(1)

	// Capture values before spawning goroutine to avoid data races.
	// The handler modifies logEntry fields after this function returns,
	// so the goroutine must read from local copies, not the shared struct.
	id := logEntry.id
	requestHash := logEntry.requestHash
	modelID := logEntry.modelID
	streaming := logEntry.streaming
	virtualKeyName := logEntry.virtualKeyName
	virtualKeyID := logEntry.virtualKeyID
	failoverAttempt := logEntry.failoverAttempt
	state := logEntry.state
	wg := &logEntry.insertWg

	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				debuglog.Error("proxy: panic in insertRequestLog", "request_id", id, "error", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var vkID interface{}
		if virtualKeyID != "" {
			vkID = virtualKeyID
		}
		_, err := h.dbPool.Exec(ctx, `
			INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, virtual_key_id, failover_attempt, state)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, modelID, requestHash, streaming, virtualKeyName, vkID, failoverAttempt, state,
		)
		if err != nil {
			debuglog.Error("proxy: failed to insert initial request log", "request_id", id, "error", err)
		}
	}()
}

// publishRequestStartedEvent emits the SSE "request.started" event.
// Call this after modelID is resolved so the event always carries the
// correct model (previously this was embedded in insertRequestLogAsync,
// which could fire before body parsing had set modelID).
func publishRequestStartedEvent(logEntry *requestLogData) {
	events.Publish(events.Event{
		Type:     "request.started",
		Severity: "info",
		Source:   "proxy",
		Message:  fmt.Sprintf("Request started: %s", logEntry.modelID),
		Metadata: map[string]interface{}{
			"request_id": logEntry.id,
			"model_id":   logEntry.modelID,
			"streaming":  logEntry.streaming,
			"state":      logEntry.state,
		},
	})
}

// WaitForInsert blocks until the async INSERT goroutine has completed (or
// timed out). Callers should invoke this before
// updateRequestLog to guarantee the row exists in the database.
func (h *Handler) WaitForInsert(logEntry *requestLogData) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		logEntry.insertWg.Wait()
	}()
	timeout := h.waitInsertTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-done:
	case <-time.After(timeout):
		debuglog.Warn("proxy: timed out waiting for request log INSERT", "request_id", logEntry.id)
	}
}

func (h *Handler) updateRequestLog(logEntry *requestLogData, opts ...updateLogOption) {
	// Guard: if the log entry was never assigned an ID (insertRequestLogAsync
	// not called), there is no row to update. An empty string is not a valid
	// UUID and would cause "invalid input syntax for type uuid" errors.
	// Note: if insertRequestLogAsync was called but the async INSERT failed,
	// the ID will still be set (assigned synchronously), and the UPDATE will
	// simply affect 0 rows (logged as a warning below).
	if logEntry.id == "" {
		debuglog.Warn("proxy: skipping updateRequestLog — log entry has no ID")
		return
	}

	// Skip DB operations when no pool is available (unit tests without DB).
	if h.dbPool == nil {
		return
	}

	// Determine if we should skip WaitForInsert (fire-and-forget).
	// The interim "streaming" state update runs on the hot path before the
	// first streamed byte — blocking on the DB INSERT can delay the client
	// by up to waitInsertTimeout (5s). Terminal states (completed/failed)
	// always wait to guarantee the row exists for the final UPDATE.
	skipWait := false
	for _, o := range opts {
		if o.skipWaitForInsert {
			skipWait = true
		}
	}

	if !skipWait {
		// Ensure the async INSERT has completed before we try to UPDATE the row.
		h.WaitForInsert(logEntry)
	}

	var providerID interface{}
	if logEntry.providerID != uuid.Nil {
		providerID = logEntry.providerID
	}
	logEntry.latencyMs = logEntry.durationMs - logEntry.proxyOverheadMs

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tag, err := h.dbPool.Exec(ctx, `
		UPDATE request_logs SET
			model_id = $2,
			provider_id = $3,
			status_code = $4,
			duration_ms = $5,
			latency_ms = $21,
			proxy_overhead_ms = $6,
			parse_ms = $7,
			failover_lookup_ms = $8,
			model_lookup_ms = $9,
			provider_lookup_ms = $10,
			key_decrypt_ms = $11,
			dial_ms = $22,
			settings_read_ms = $23,
			response_header_ms = $12,
			ttft_ms = $25,
			tokens_per_second = $13,
			tokens_prompt = $14,
			tokens_completion = $15,
			tokens_completion_reasoning = $24,
			tokens_prompt_cache_hit = $16,
			tokens_prompt_cache_miss = $17,
			error_message = $18,
			failover_attempt = $19,
			state = $20,
			resolved_model_id = $26
		WHERE id = $1`,
		logEntry.id, logEntry.modelID, providerID, logEntry.statusCode, logEntry.durationMs,
		logEntry.proxyOverheadMs, logEntry.parseMs, logEntry.failoverLookupMs, logEntry.modelLookupMs, logEntry.providerLookupMs,
		logEntry.keyDecryptMs, logEntry.responseHeaderMs, logEntry.tokensPerSecond, logEntry.tokensPrompt,
		logEntry.tokensCompletion, logEntry.tokensPromptCacheHit, logEntry.tokensPromptCacheMiss,
		logEntry.errorMessage, logEntry.failoverAttempt, logEntry.state, logEntry.latencyMs,
		logEntry.dialMs, logEntry.settingsReadMs, logEntry.tokensCompletionReasoning, logEntry.ttftMs,
		logEntry.resolvedModelID,
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
			Source:   "proxy",
			Message:  msg,
			Metadata: map[string]interface{}{
				"request_id":    logEntry.id,
				"model_id":      logEntry.modelID,
				"provider_name": logEntry.providerName,
				"state":         logEntry.state,
				"status_code":   logEntry.statusCode,
			},
		})
	}
}
