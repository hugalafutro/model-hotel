package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestPublishRequestStreamingEvent verifies the mid-stream "request.streaming"
// event carries the request id plus the now-committed provider/model so a live
// dashboard row can replace its "Resolving" placeholder before completion.
func TestPublishRequestStreamingEvent(t *testing.T) {
	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	logData := &requestLogData{
		id:              "req-stream-123",
		modelID:         "hotel/gpt-4",
		providerName:    "OpenAI",
		resolvedModelID: "gpt-4o",
		state:           "streaming",
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
			if ev.Metadata["resolved_model_id"] != "gpt-4o" {
				t.Errorf("resolved_model_id = %v, want gpt-4o", ev.Metadata["resolved_model_id"])
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
	logData.insertWg.Add(1)

	// Call Done() in a goroutine after a brief delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		logData.insertWg.Done()
	}()

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
