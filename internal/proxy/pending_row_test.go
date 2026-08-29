package proxy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedRequestLogRow writes the row insertRequestLogAsync would have written.
func seedRequestLogRow(t *testing.T, h *Handler, logData *requestLogData) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := h.dbPool.Exec(ctx, `
		INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, failover_attempt, state, endpoint_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		logData.id, logData.modelID, logData.requestHash, false, logData.virtualKeyName, 0, "pending", endpointTypeChat)
	if err != nil {
		t.Errorf("seed insert: %v", err)
	}
}

// A terminal update must not lose the race with the INSERT it is updating.
//
// updateRequestLog's own comment stated the invariant — "Terminal states
// (completed/failed) always wait to guarantee the row exists for the final
// UPDATE" — but six of the seven terminal call sites passed skipWaitForInsert
// (only the streaming finalize waited), so the UPDATE could run first, hit 0
// rows, and leave the request at 'pending' with no status, no duration and no
// error.
//
// The flag's reasoning was that the INSERT "has the entire provider round-trip
// to complete". That is true of a request that reached a provider and false of
// every request that failed before one: an unknown model, an invalid model
// format, a rejected key. Those update microseconds after the insert is queued,
// so for them it was not a low-probability race, it was the normal case.
func TestUpdateRequestLog_ATerminalUpdateWaitsForItsRow(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	for _, state := range []string{"failed", "completed"} {
		t.Run(state, func(t *testing.T) {
			logData := &requestLogData{
				modelID: "m", virtualKeyName: "k", state: "pending",
				endpointType: endpointTypeChat,
			}
			// The production shape: the id is assigned synchronously and the row
			// is written by a goroutine the wait group guards. Held open here so
			// the update genuinely has to wait rather than happening to win.
			logData.id = uuid.New().String()
			logData.requestHash = generateRequestHash()
			logData.insertWg.Add(1)
			go func() {
				defer logData.insertWg.Done()
				time.Sleep(150 * time.Millisecond)
				seedRequestLogRow(t, h, logData)
			}()

			logData.state = state
			logData.statusCode = 404
			logData.errorMessage = "model not found"
			// The flag six of the seven terminal callers pass, and which must not
			// cost a terminal update its row.
			h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

			// The row certainly exists by now, so what is read back says only
			// whether the UPDATE landed on it or ran before it and hit nothing.
			logData.insertWg.Wait()
			var got string
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.dbPool.QueryRow(ctx, `SELECT state FROM request_logs WHERE id = $1`, logData.id).Scan(&got); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got != state {
				t.Errorf("row state = %q, want %q: the update beat its own insert and stranded the request", got, state)
			}
		})
	}
}

// And it must not pay for that row when it already has one.
//
// The terminal update on the non-streaming path runs BEFORE the response body is
// encoded, so a wait there is pure added latency on every successful completion —
// up to waitInsertTimeout (5s) whenever the pool is contended. Waiting only after
// an UPDATE reports 0 rows keeps the fix and charges nothing for it: the row is
// normally long since inserted, and then the retry never fires.
func TestUpdateRequestLog_ATerminalUpdateWithARowDoesNotWait(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	logData := &requestLogData{
		modelID: "m", virtualKeyName: "k", state: "pending",
		endpointType: endpointTypeChat,
	}
	logData.id = uuid.New().String()
	logData.requestHash = generateRequestHash()
	seedRequestLogRow(t, h, logData)

	// Never released. Anything that waits on the insert blocks until
	// WaitForInsert's timeout, whether it waits before the UPDATE or after it.
	logData.insertWg.Add(1)
	defer logData.insertWg.Done()

	logData.state = "completed"
	logData.statusCode = 200
	start := time.Now()
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("a terminal update whose row exists blocked for %s before the response was written", waited)
	}

	var got string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.dbPool.QueryRow(ctx, `SELECT state FROM request_logs WHERE id = $1`, logData.id).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "completed" {
		t.Errorf("row state = %q, want completed", got)
	}
}

// The flag still means what it meant for an interim update: those run on the hot
// path before the client's first byte, and blocking them on the DB is the
// latency the flag exists to avoid. An interim update that finds no row does not
// retry — the terminal update that follows it writes the same row anyway.
func TestUpdateRequestLog_AnInterimUpdateStillDoesNotWait(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	logData := &requestLogData{modelID: "m", virtualKeyName: "k", endpointType: endpointTypeChat}
	logData.id = uuid.New().String()
	logData.requestHash = generateRequestHash()
	logData.insertWg.Add(1)
	defer logData.insertWg.Done()

	// The insert is never released and no row is seeded, so the UPDATE hits 0
	// rows — the one condition that makes a TERMINAL update wait. An interim one
	// must return long before WaitForInsert's timeout.
	logData.state = "streaming"
	start := time.Now()
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("an interim update blocked for %s on the hot path", waited)
	}
}

func TestIsTerminalLogState(t *testing.T) {
	t.Parallel()
	for state, want := range map[string]bool{
		"completed": true,
		"failed":    true,
		"pending":   false,
		"streaming": false,
		"":          false,
	} {
		if got := isTerminalLogState(state); got != want {
			t.Errorf("isTerminalLogState(%q) = %v, want %v", state, got, want)
		}
	}
}

// The production shape end to end: the real insertRequestLogAsync, and a
// terminal update fired the instant it returns — which is what a request that
// fails before reaching a provider does.
//
// The hand-rolled tests above reproduce the RACE; this one reproduces the
// INSERT as well, so a change to insertRequestLogAsync (moving the id
// assignment inside the goroutine, say) cannot leave them green while
// production re-breaks. Run against master it strands the majority of the
// batch; the fix must strand none of it.
func TestUpdateRequestLog_ConcurrentTerminalUpdatesNeverStrand(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	const requests = 60
	entries := make([]*requestLogData, requests)
	var wg sync.WaitGroup
	for i := range entries {
		logData := &requestLogData{
			modelID: "concurrent-model", virtualKeyName: "k", state: "pending",
			endpointType: endpointTypeChat,
		}
		entries[i] = logData
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.insertRequestLogAsync(logData)
			// No pause: an unknown model or an invalid model format fails here,
			// microseconds after the insert was queued.
			logData.state = "failed"
			logData.statusCode = 400
			logData.errorKind = KindValidation
			logData.errorMessage = "invalid model format"
			h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		}()
	}
	wg.Wait()

	ids := make([]string, 0, requests)
	for _, e := range entries {
		e.insertWg.Wait() // every row is certainly written by now
		ids = append(ids, e.id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := h.dbPool.Query(ctx, `SELECT state FROM request_logs WHERE id = ANY($1)`, ids)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer rows.Close()
	stranded, seen := 0, 0
	for rows.Next() {
		var state string
		if err := rows.Scan(&state); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if state != "failed" {
			stranded++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if seen != requests {
		t.Fatalf("read back %d rows, want %d", seen, requests)
	}
	if stranded != 0 {
		t.Errorf("%d of %d requests stranded before the terminal update landed", stranded, requests)
	}
}

// A terminal caller that did NOT skip the wait does not wait a second time.
//
// stream_finalize is the one such caller. Its up-front WaitForInsert can only
// return with the insert still outstanding by timing out, and the insert
// goroutine is bounded by a context of its own — so 0 rows there means the
// INSERT failed outright, which no further waiting repairs.
func TestUpdateRequestLog_ATerminalUpdateThatAlreadyWaitedDoesNotWaitAgain(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.waitInsertTimeout = 300 * time.Millisecond

	logData := &requestLogData{modelID: "m", virtualKeyName: "k", endpointType: endpointTypeChat}
	logData.id = uuid.New().String()
	logData.requestHash = generateRequestHash()
	logData.insertWg.Add(1)
	defer logData.insertWg.Done()

	// No row and an insert that never completes: the up-front wait times out,
	// the UPDATE finds nothing. One timeout is the whole budget.
	logData.state = "completed"
	start := time.Now()
	h.updateRequestLog(logData)
	if waited := time.Since(start); waited > 450*time.Millisecond {
		t.Errorf("waited %s, about twice the %s timeout: the update waited for the insert twice", waited, h.waitInsertTimeout)
	}
}

// An UPDATE that ERRORS is not the race and must not be treated as one. There
// is no row count to believe either way, and waiting for an insert cannot fix a
// statement the database refused.
func TestUpdateRequestLog_AFailedUpdateDoesNotWait(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	logData := &requestLogData{modelID: "m", virtualKeyName: "k", endpointType: endpointTypeChat}
	// Non-empty, so it clears the "no ID" guard, and not a UUID, so Postgres
	// rejects the statement rather than matching no rows.
	logData.id = "not-a-uuid"
	logData.insertWg.Add(1)
	defer logData.insertWg.Done()

	logData.state = "failed"
	start := time.Now()
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("a rejected UPDATE blocked for %s waiting for an insert that could not have helped", waited)
	}
}

// Twenty-eight positional parameters, and this PR is what moved them into a
// function of their own. A transposition between two columns of the same type
// is silent — every figure below is distinct, and exactly representable in
// float4, so it can only read back from the column it was written to.
func TestUpdateRequestLog_WritesEachFigureToItsOwnColumn(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	logData := &requestLogData{
		modelID: "column-model", virtualKeyName: "k", state: "pending",
		endpointType: endpointTypeChat,
	}
	logData.id = uuid.New().String()
	logData.requestHash = generateRequestHash()
	seedRequestLogRow(t, h, logData)

	logData.state = "completed"
	logData.statusCode = 201
	logData.durationMs = 111.5
	logData.proxyOverheadMs = 22.25 // latencyMs is derived: 111.5 - 22.25
	logData.parseMs = 1.5
	logData.failoverLookupMs = 2.5
	logData.modelLookupMs = 3.5
	logData.providerLookupMs = 4.5
	logData.keyDecryptMs = 5.5
	logData.dialMs = 6.5
	logData.settingsReadMs = 7.5
	logData.responseHeaderMs = 8.5
	logData.ttftMs = 9.5
	logData.tokensPerSecond = 12.5
	logData.tokensPrompt = 101
	logData.tokensCompletion = 102
	logData.tokensCompletionReasoning = 103
	logData.tokensPromptCacheHit = 104
	logData.tokensPromptCacheMiss = 105
	logData.failoverAttempt = 3
	logData.errorMessage = "column-error"
	logData.resolvedModelID = "column-resolved"
	logData.errorKind = KindValidation
	h.updateRequestLog(logData)

	var got struct {
		model                                     string
		status, attempt                           int
		prompt, completion, reasoning, hit, miss  int
		duration, latency, overhead, parse, fLook float64
		mLook, pLook, keyDec, dial, settings      float64
		header, ttft, tps                         float64
		errMsg, resolved, kind, state             string
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := h.dbPool.QueryRow(ctx, `
		SELECT model_id, status_code, failover_attempt,
		       tokens_prompt, tokens_completion, tokens_completion_reasoning,
		       tokens_prompt_cache_hit, tokens_prompt_cache_miss,
		       duration_ms, latency_ms, proxy_overhead_ms, parse_ms, failover_lookup_ms,
		       model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
		       response_header_ms, ttft_ms, tokens_per_second,
		       error_message, resolved_model_id, error_kind, state
		  FROM request_logs WHERE id = $1`, logData.id).Scan(
		&got.model, &got.status, &got.attempt,
		&got.prompt, &got.completion, &got.reasoning, &got.hit, &got.miss,
		&got.duration, &got.latency, &got.overhead, &got.parse, &got.fLook,
		&got.mLook, &got.pLook, &got.keyDec, &got.dial, &got.settings,
		&got.header, &got.ttft, &got.tps,
		&got.errMsg, &got.resolved, &got.kind, &got.state)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	for _, c := range []struct {
		column    string
		got, want float64
	}{
		{"duration_ms", got.duration, 111.5},
		{"latency_ms", got.latency, 89.25},
		{"proxy_overhead_ms", got.overhead, 22.25},
		{"parse_ms", got.parse, 1.5},
		{"failover_lookup_ms", got.fLook, 2.5},
		{"model_lookup_ms", got.mLook, 3.5},
		{"provider_lookup_ms", got.pLook, 4.5},
		{"key_decrypt_ms", got.keyDec, 5.5},
		{"dial_ms", got.dial, 6.5},
		{"settings_read_ms", got.settings, 7.5},
		{"response_header_ms", got.header, 8.5},
		{"ttft_ms", got.ttft, 9.5},
		{"tokens_per_second", got.tps, 12.5},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.column, c.got, c.want)
		}
	}
	for _, c := range []struct {
		column    string
		got, want int
	}{
		{"status_code", got.status, 201},
		{"failover_attempt", got.attempt, 3},
		{"tokens_prompt", got.prompt, 101},
		{"tokens_completion", got.completion, 102},
		{"tokens_completion_reasoning", got.reasoning, 103},
		{"tokens_prompt_cache_hit", got.hit, 104},
		{"tokens_prompt_cache_miss", got.miss, 105},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.column, c.got, c.want)
		}
	}
	for _, c := range []struct {
		column, got, want string
	}{
		{"model_id", got.model, "column-model"},
		{"error_message", got.errMsg, "column-error"},
		{"resolved_model_id", got.resolved, "column-resolved"},
		{"error_kind", got.kind, string(KindValidation)},
		{"state", got.state, "completed"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.column, c.got, c.want)
		}
	}
}
