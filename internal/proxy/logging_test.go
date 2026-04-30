package proxy

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// insertRequestLog integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestInsertRequestLog_Success(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	err := h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Errorf("insertRequestLog failed: %v", err)
	}
}

func TestInsertRequestLog_SetsID(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	if logEntry.id != "" {
		t.Error("id should be empty before insert")
	}

	err := h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Fatalf("insertRequestLog failed: %v", err)
	}

	if logEntry.id == "" {
		t.Error("id should be populated after insert")
	}
	// Verify it is a valid UUID
	_, err = uuid.Parse(logEntry.id)
	if err != nil {
		t.Errorf("id should be a valid UUID, got %q: %v", logEntry.id, err)
	}
}

func TestInsertRequestLog_SetsRequestHash(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	err := h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Fatalf("insertRequestLog failed: %v", err)
	}

	if logEntry.requestHash == "" {
		t.Error("requestHash should be populated after insert")
	}
	// generateRequestHash returns 16 hex chars (8 bytes)
	if len(logEntry.requestHash) != 16 {
		t.Errorf("requestHash should be 16 hex chars, got %d chars: %q", len(logEntry.requestHash), logEntry.requestHash)
	}
}

func TestInsertRequestLog_EmptyVirtualKeyID(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       true,
		virtualKeyName:  "anonymous-key",
		virtualKeyID:    "", // empty — should be stored as NULL
		failoverAttempt: 1,
		state:           "pending",
	}

	err := h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Errorf("insertRequestLog with empty virtualKeyID failed: %v", err)
	}
}

func TestInsertRequestLog_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	logEntry := &requestLogData{
		modelID:         uuid.NewString(),
		streaming:       false,
		virtualKeyName:  "test-key",
		virtualKeyID:    uuid.NewString(),
		failoverAttempt: 0,
		state:           "pending",
	}

	err := h.insertRequestLog(ctx, logEntry)
	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// ---------------------------------------------------------------------------
// updateRequestLog integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestUpdateRequestLog_Success(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

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
		modelID:             uuid.NewString(),
		streaming:           false,
		virtualKeyName:      "test-key",
		virtualKeyID:        uuid.NewString(),
		failoverAttempt:     0,
		state:               "pending",
		statusCode:          200,
		durationMs:          150.0,
		proxyOverheadMs:     10.0,
		parseMs:             5.0,
		modelLookupMs:       1.0,
		providerLookupMs:    2.0,
		keyDecryptMs:        0.5,
		ttftMs:              100.0,
		tokensPerSecond:     50.0,
		tokensPrompt:        100,
		tokensCompletion:    200,
		tokensPromptCacheHit:  50,
		tokensPromptCacheMiss: 50,
		errorMessage:        "",
	}

	err = h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Fatalf("insertRequestLog failed: %v", err)
	}

	// Now update the log with a valid providerID
	logEntry.providerID = prov.ID
	logEntry.state = "completed"

	h.updateRequestLog(context.Background(), logEntry)
	// updateRequestLog does not return an error, just logs it.
	// If no panic occurred, the test passes.
}

func TestUpdateRequestLog_CalculatesLatency(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

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

	err = h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Fatalf("insertRequestLog failed: %v", err)
	}

	logEntry.providerID = prov.ID
	logEntry.state = "completed"

	h.updateRequestLog(context.Background(), logEntry)

	// Verify latencyMs was calculated: latencyMs = durationMs - proxyOverheadMs
	expectedLatency := 200.0 - 30.0
	if logEntry.latencyMs != expectedLatency {
		t.Errorf("latencyMs = %f, want %f", logEntry.latencyMs, expectedLatency)
	}
}

func TestUpdateRequestLog_NilProviderID(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

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

	err := h.insertRequestLog(context.Background(), logEntry)
	if err != nil {
		t.Fatalf("insertRequestLog failed: %v", err)
	}

	// Update with nil providerID (uuid.Nil)
	logEntry.state = "failed"
	logEntry.errorMessage = "connection refused"

	h.updateRequestLog(context.Background(), logEntry)
	// Should not panic with nil providerID
}

func TestUpdateRequestLog_NonexistentID(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

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

	h.updateRequestLog(context.Background(), logEntry)
	// No panic = pass
}
