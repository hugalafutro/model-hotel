package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestPublishRequestStreamingEvent verifies the mid-stream "request.streaming"
// event carries the request id plus the now-committed provider/model so a live
// dashboard row can replace its "Resolving" placeholder before completion.
func TestPublishRequestStreamingEvent(t *testing.T) {
	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	logData := &requestLogData{
		id:           "req-stream-123",
		modelID:      "hotel/gpt-4",
		providerName: "OpenAI",
		state:        "streaming",
	}
	publishRequestStreamingEvent(logData)

	// The default bus fans every event out to all subscribers, so other tests
	// publishing concurrently may interleave; filter to our request id.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "request.streaming" ||
				ev.Metadata["request_id"] != "req-stream-123" {
				continue
			}
			if ev.Metadata["provider_name"] != "OpenAI" {
				t.Errorf("provider_name = %v, want OpenAI", ev.Metadata["provider_name"])
			}
			if _, ok := ev.Metadata["resolved_model_id"]; ok {
				t.Errorf("resolved_model_id should be omitted (failover-only); got %v", ev.Metadata["resolved_model_id"])
			}
			if ev.Metadata["model_id"] != "hotel/gpt-4" {
				t.Errorf("model_id = %v, want hotel/gpt-4", ev.Metadata["model_id"])
			}
			if ev.Metadata["state"] != "streaming" {
				t.Errorf("state = %v, want streaming", ev.Metadata["state"])
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting for request.streaming event")
		}
	}
}

// ---------------------------------------------------------------------------
// insertRequestLogAsync integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestInsertRequestLogAsync_Success(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	h.insertRequestLogAsync(logEntry)
	// Wait briefly for the async goroutine to complete
	time.Sleep(100 * time.Millisecond)

	// ID should have been set synchronously before the goroutine
	if logEntry.id == "" {
		t.Error("id should be populated synchronously by insertRequestLogAsync")
	}
}

func TestInsertRequestLogAsync_SetsIDImmediately(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	if logEntry.id != "" {
		t.Error("id should be empty before async insert")
	}

	h.insertRequestLogAsync(logEntry)

	// ID must be set synchronously, before goroutine runs
	if logEntry.id == "" {
		t.Error("id should be populated synchronously by insertRequestLogAsync")
	}
	// Verify it is a valid UUID
	_, err := uuid.Parse(logEntry.id)
	if err != nil {
		t.Errorf("id should be a valid UUID, got %q: %v", logEntry.id, err)
	}
}

func TestInsertRequestLogAsync_SetsRequestHashImmediately(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	h.insertRequestLogAsync(logEntry)

	if logEntry.requestHash == "" {
		t.Error("requestHash should be populated synchronously by insertRequestLogAsync")
	}
	// generateRequestHash returns 16 hex chars (8 bytes)
	if len(logEntry.requestHash) != 16 {
		t.Errorf("requestHash should be 16 hex chars, got %d chars: %q", len(logEntry.requestHash), logEntry.requestHash)
	}
}

func TestInsertRequestLogAsync_EmptyVirtualKeyID(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       true,
		virtualKeyName:  "anonymous-key",
		virtualKeyID:    "", // empty — should be stored as NULL
		failoverAttempt: 1,
		state:           "pending",
	}

	h.insertRequestLogAsync(logEntry)
	time.Sleep(100 * time.Millisecond)
	// No panic = pass
}

// TestInsertRequestLogAsync_OwnerStoredOnEveryOwnedRow pins the request-time
// owner stamp: a keyless row (dashboard chat/arena) carries it because it has
// no virtual key to resolve one through, and a keyed row carries it because a
// user's dollar budget sums the column (the log views still read a keyed row
// through the key's CURRENT owner, so reassigning a key moves its history). A
// row with no owner in context stays NULL.
func TestInsertRequestLogAsync_OwnerStoredOnEveryOwnedRow(t *testing.T) {
	h := newIntegrationHandler()
	pool := testDB.Pool()
	ctx := context.Background()

	var ownerID string
	err := pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id::text`,
		"log-owner-"+uuid.NewString(),
	).Scan(&ownerID)
	if err != nil {
		t.Fatalf("create owner user: %v", err)
	}

	storedOwner := func(logEntry *requestLogData) *string {
		h.insertRequestLogAsync(logEntry)
		h.WaitForInsert(logEntry)
		var got *string
		if err := pool.QueryRow(ctx,
			`SELECT owner_user_id::text FROM request_logs WHERE id = $1`, logEntry.id,
		).Scan(&got); err != nil {
			t.Fatalf("read back row %s: %v", logEntry.id, err)
		}
		return got
	}

	keyless := storedOwner(&requestLogData{
		modelID:     uuid.NewString(),
		ownerUserID: ownerID,
		state:       "pending",
	})
	if keyless == nil || *keyless != ownerID {
		t.Errorf("keyless row owner_user_id = %v, want %q", keyless, ownerID)
	}

	keyed := storedOwner(&requestLogData{
		modelID:        uuid.NewString(),
		virtualKeyName: "some-key",
		virtualKeyID:   uuid.NewString(),
		ownerUserID:    ownerID,
		state:          "pending",
	})
	// A keyed row carries the owner too: the log views still resolve it
	// through the key, but a user's dollar budget sums the stamp.
	if keyed == nil || *keyed != ownerID {
		t.Errorf("keyed row owner_user_id = %v, want %q", keyed, ownerID)
	}

	// No owner in context at all (env admin token, unowned key): still NULL.
	anonymous := storedOwner(&requestLogData{
		modelID: uuid.NewString(),
		state:   "pending",
	})
	if anonymous != nil {
		t.Errorf("unattributed row owner_user_id = %q, want NULL", *anonymous)
	}
}

func TestInsertRequestLogAsync_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	// async version uses its own context internally, so canceled context
	// from the caller doesn't affect it — the ID should still be set.
	h.insertRequestLogAsync(logEntry)
	if logEntry.id == "" {
		t.Error("id should be populated even with async insert")
	}
}

// ---------------------------------------------------------------------------
// updateRequestLog integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestUpdateRequestLog_Success(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider so we can reference a valid providerID
	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-api-key-update-log", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-update-log-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-api-key-update-log",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() {
		_ = h.providerRepo.Delete(context.Background(), prov.ID)
	}()

	logEntry := &requestLogData{
		modelID:                   uuid.NewString(),
		streaming:                 false,
		virtualKeyName:            "test-key",
		virtualKeyID:              uuid.NewString(),
		failoverAttempt:           0,
		state:                     "pending",
		statusCode:                200,
		durationMs:                150.0,
		proxyOverheadMs:           10.0,
		parseMs:                   5.0,
		modelLookupMs:             1.0,
		providerLookupMs:          2.0,
		keyDecryptMs:              0.5,
		ttftMs:                    100.0,
		tokensPerSecond:           50.0,
		tokensPrompt:              100,
		tokensCompletion:          200,
		tokensPromptCacheHit:      50,
		tokensPromptCacheMiss:     50,
		tokensCompletionReasoning: 0,
		errorMessage:              "",
	}

	h.insertRequestLogAsync(logEntry)
	time.Sleep(100 * time.Millisecond) // wait for async DB insert
	if logEntry.id == "" {
		t.Fatalf("insertRequestLogAsync did not set id")
	}

	// Now update the log with a valid providerID
	logEntry.providerID = prov.ID
	logEntry.state = "completed"

	h.updateRequestLog(logEntry)
	// updateRequestLog does not return an error, just logs it.
	// If no panic occurred, the test passes.
}

func TestUpdateRequestLog_CalculatesLatency(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider so we can reference a valid providerID
	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-api-key-latency-log", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-latency-log-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-api-key-latency-log",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() {
		_ = h.providerRepo.Delete(context.Background(), prov.ID)
	}()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
		durationMs:      200.0,
		proxyOverheadMs: 30.0,
	}

	h.insertRequestLogAsync(logEntry)
	time.Sleep(100 * time.Millisecond) // wait for async DB insert

	logEntry.providerID = prov.ID
	logEntry.state = "completed"

	h.updateRequestLog(logEntry)

	// Verify latencyMs was calculated: latencyMs = durationMs - proxyOverheadMs
	expectedLatency := 200.0 - 30.0
	if logEntry.latencyMs != expectedLatency {
		t.Errorf("latencyMs = %f, want %f", logEntry.latencyMs, expectedLatency)
	}
}

func TestUpdateRequestLog_NilProviderID(t *testing.T) {
	h := newIntegrationHandler()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
		durationMs:      100.0,
		proxyOverheadMs: 10.0,
	}

	h.insertRequestLogAsync(logEntry)
	time.Sleep(100 * time.Millisecond) // wait for async DB insert

	// Update with nil providerID (uuid.Nil)
	logEntry.state = "failed"
	logEntry.errorMessage = "connection refused"

	h.updateRequestLog(logEntry)
	// Should not panic with nil providerID
}

func TestUpdateRequestLog_NonexistentID(t *testing.T) {
	h := newIntegrationHandler()

	// Create a log entry with a non-existent ID — update should log a
	// warning about 0 rows affected but not panic.
	logEntry := &requestLogData{
		id:              uuid.NewString(),
		providerID:      uuid.New(),
		statusCode:      500,
		durationMs:      100.0,
		proxyOverheadMs: 10.0,
		state:           "failed",
		errorMessage:    "test error",
	}

	h.updateRequestLog(logEntry)
	// No panic = pass
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_test.go
// ---------------------------------------------------------------------------

// TestWaitForInsert_Timeout tests that WaitForInsert returns after timeout
// when the insert goroutine never completes.
func TestWaitForInsert_Timeout(t *testing.T) {
	t.Helper()
	h := &Handler{waitInsertTimeout: 50 * time.Millisecond}

	// Create a requestLogData with a WaitGroup that never gets Done()
	logData := &requestLogData{
		id:             "test-timeout-id",
		modelID:        "test-model",
		streaming:      false,
		virtualKeyName: "test-key",
		state:          "pending",
	}
	// Add 1 to the WaitGroup but never call Done()
	logData.insertWg.Add(1)

	start := time.Now()
	h.WaitForInsert(logData)
	elapsed := time.Since(start)

	// Should return within ~100ms (50ms timeout + small margin)
	if elapsed < 40*time.Millisecond {
		t.Errorf("WaitForInsert returned too early: %v (expected ~50ms timeout)", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("WaitForInsert took too long: %v (expected ~50ms timeout)", elapsed)
	}
}

// TestWaitForInsert_Completes tests that WaitForInsert returns immediately
// when the insert completes.
func TestWaitForInsert_Completes(t *testing.T) {
	t.Helper()
	h := &Handler{}

	logData := &requestLogData{
		id:             "test-complete-id",
		modelID:        "test-model",
		streaming:      false,
		virtualKeyName: "test-key",
		state:          "pending",
	}

	// Call Done() in a goroutine after a brief delay
	logData.insertWg.Go(func() {
		time.Sleep(10 * time.Millisecond)
	})

	start := time.Now()
	h.WaitForInsert(logData)
	elapsed := time.Since(start)

	// Should return quickly (within ~100ms, not the 5s timeout)
	if elapsed > 100*time.Millisecond {
		t.Errorf("WaitForInsert took too long: %v (expected ~10ms)", elapsed)
	}
}

// ---------------------------------------------------------------------------
// updateRequestLog edge case tests
// ---------------------------------------------------------------------------

// TestUpdateRequestLog_EmptyIDSkips tests that updateRequestLog does nothing
// when the log entry has no ID (never inserted).
func TestUpdateRequestLog_EmptyIDSkips(t *testing.T) {
	h := &Handler{}

	logEntry := &requestLogData{
		id:         "", // empty ID — should be skipped
		modelID:    "test-model",
		state:      "completed",
		statusCode: 200,
	}

	// Should not panic with empty ID
	h.updateRequestLog(logEntry)
}

// TestUpdateRequestLog_NilDBPoolSkips tests that updateRequestLog does nothing
// when dbPool is nil (unit tests without DB).
func TestUpdateRequestLog_NilDBPoolSkips(t *testing.T) {
	h := &Handler{dbPool: nil}

	logEntry := &requestLogData{
		id:         uuid.NewString(),
		modelID:    "test-model",
		state:      "completed",
		statusCode: 200,
	}

	// Should not panic with nil dbPool
	h.updateRequestLog(logEntry)
}

// TestUpdateRequestLog_SkipWaitForInsert tests the skipWaitForInsert option
// path. When requested, the update should not call WaitForInsert.
func TestUpdateRequestLog_SkipWaitForInsert(t *testing.T) {
	h := &Handler{}

	logEntry := &requestLogData{
		id:         uuid.NewString(),
		modelID:    "test-model",
		state:      "streaming",
		statusCode: 200,
	}

	// With skipWaitForInsert, the function should not attempt to wait for
	// the async insert (which would hang since insertWg is never Done).
	// The nil dbPool also prevents any DB operations.
	h.updateRequestLog(logEntry, updateLogOption{skipWaitForInsert: true})
}

// TestInsertRequestLogAsync_PersistsClientIP verifies the resolved client IP
// stamped on the log entry lands in request_logs.client_ip (NULL when empty).
func TestInsertRequestLogAsync_PersistsClientIP(t *testing.T) {
	h := newIntegrationHandler()
	pool := testDB.Pool()
	ctx := context.Background()

	storedIP := func(logEntry *requestLogData) *string {
		h.insertRequestLogAsync(logEntry)
		h.WaitForInsert(logEntry)
		var ip *string
		if err := pool.QueryRow(ctx,
			`SELECT client_ip FROM request_logs WHERE id = $1`, logEntry.id,
		).Scan(&ip); err != nil {
			t.Fatalf("read inserted row: %v", err)
		}
		return ip
	}

	withIP := storedIP(&requestLogData{modelID: uuid.NewString(), state: "pending", clientIP: "198.51.100.9"})
	if withIP == nil || *withIP != "198.51.100.9" {
		t.Errorf("client_ip = %v, want 198.51.100.9", withIP)
	}

	withoutIP := storedIP(&requestLogData{modelID: uuid.NewString(), state: "pending"})
	if withoutIP != nil {
		t.Errorf("client_ip = %q, want NULL for an empty client IP", *withoutIP)
	}
}

// TestNewPendingRequestLog_StampsClientIP verifies the pending log entry is
// created with the request's resolved client address.
func TestNewPendingRequestLog_StampsClientIP(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	req.RemoteAddr = "198.51.100.7:51234"

	logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "gpt-4", false)
	if logData.clientIP != "198.51.100.7" {
		t.Errorf("clientIP = %q, want 198.51.100.7", logData.clientIP)
	}
}

// TestUpdateRequestLog_StampsCost covers the cost the terminal write prices
// from the served model: a priced model yields the row's cost at its prices,
// an unpriced model and a row never dispatched to any candidate stay NULL
// rather than reading as free, and a dispatched request that charged nothing
// prices to 0.
func TestUpdateRequestLog_StampsCost(t *testing.T) {
	h := newIntegrationHandler()
	ctx := context.Background()
	f := func(v float64) *float64 { return &v }

	readCost := func(id string) *float64 {
		t.Helper()
		var cost *float64
		if err := h.dbPool.QueryRow(ctx, `SELECT cost_usd FROM request_logs WHERE id = $1`, id).Scan(&cost); err != nil {
			t.Fatalf("read cost_usd: %v", err)
		}
		return cost
	}
	newRow := func() *requestLogData {
		logEntry := &requestLogData{
			modelID:               uuid.NewString(),
			virtualKeyName:        "cost-key",
			virtualKeyID:          uuid.NewString(),
			state:                 "pending",
			tokensPrompt:          1_000_000,
			tokensPromptCacheHit:  800_000,
			tokensPromptCacheMiss: 200_000,
			tokensCompletion:      250_000,
			// Reasoning is a breakdown of completion: recorded, never priced again.
			tokensCompletionReasoning: 200_000,
		}
		h.insertRequestLogAsync(logEntry)
		h.WaitForInsert(logEntry)
		t.Cleanup(func() {
			_, _ = h.dbPool.Exec(context.Background(), `DELETE FROM request_logs WHERE id = $1`, logEntry.id)
		})
		return logEntry
	}

	priced := newRow()
	priced.servedModel = &model.Model{InputPricePerMillion: f(1), InputPricePerMillionCacheHit: f(0.1), OutputPricePerMillion: f(4)}
	priced.state = "completed"
	h.updateRequestLog(priced)
	// 0.8M hits at $0.1 + 0.2M misses at $1 + 0.25M completion at $4.
	if got := readCost(priced.id); got == nil || *got < 1.28-1e-9 || *got > 1.28+1e-9 {
		t.Errorf("priced cost_usd = %v, want 1.28", got)
	}
	var reasoning int
	if err := h.dbPool.QueryRow(ctx, `SELECT tokens_completion_reasoning FROM request_logs WHERE id = $1`, priced.id).Scan(&reasoning); err != nil {
		t.Fatalf("read reasoning tokens: %v", err)
	}
	if reasoning != 200_000 {
		t.Errorf("tokens_completion_reasoning = %d, want 200000 recorded as the breakdown", reasoning)
	}

	// An interim streaming write stamps nothing: usage is not in yet.
	streaming := newRow()
	streaming.servedModel = priced.servedModel
	streaming.state = "streaming"
	h.updateRequestLog(streaming, updateLogOption{skipWaitForInsert: true})
	if got := readCost(streaming.id); got != nil {
		t.Errorf("streaming row cost_usd = %v, want NULL until terminal", *got)
	}

	unpriced := newRow()
	unpriced.servedModel = &model.Model{InputPricePerMillion: f(1)}
	unpriced.state = "completed"
	h.updateRequestLog(unpriced)
	if got := readCost(unpriced.id); got != nil {
		t.Errorf("unpriced model cost_usd = %v, want NULL", *got)
	}

	undispatched := newRow()
	undispatched.state = "failed"
	h.updateRequestLog(undispatched)
	if got := readCost(undispatched.id); got != nil {
		t.Errorf("undispatched request cost_usd = %v, want NULL", *got)
	}

	nothingCharged := newRow()
	nothingCharged.tokensPrompt, nothingCharged.tokensPromptCacheHit, nothingCharged.tokensPromptCacheMiss = 0, 0, 0
	nothingCharged.tokensCompletion, nothingCharged.tokensCompletionReasoning = 0, 0
	nothingCharged.servedModel = priced.servedModel
	nothingCharged.state = "failed"
	h.updateRequestLog(nothingCharged)
	if got := readCost(nothingCharged.id); got == nil || *got != 0 {
		t.Errorf("dispatched request that charged nothing cost_usd = %v, want 0", got)
	}
}
