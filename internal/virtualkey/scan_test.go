package virtualkey

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockVKScanner implements scanner for testing.
type mockVKScanner struct {
	values []any
	err    error
}

func (m *mockVKScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	for i, d := range dest {
		if i >= len(m.values) {
			break
		}
		switch dst := d.(type) {
		case *uuid.UUID:
			*dst = m.values[i].(uuid.UUID)
		case *string:
			*dst = m.values[i].(string)
		case *int64:
			*dst = m.values[i].(int64)
		case *time.Time:
			*dst = m.values[i].(time.Time)
		case **time.Time:
			*dst = m.values[i].(*time.Time)
		case **float64:
			*dst = m.values[i].(*float64)
		case **int:
			*dst = m.values[i].(*int)
		case **[]string:
			*dst = m.values[i].(*[]string)
		case *bool:
			*dst = m.values[i].(bool)
		}
	}
	return nil
}

func TestScanVirtualKey_Success(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Millisecond).UTC()
	rps := 10.5
	burst := 20
	providers := []string{"provider-1", "provider-2"}

	row := &mockVKScanner{
		values: []any{
			id,                // ID
			"test-key",        // Name
			"hash123",         // KeyHash
			"sk-...ab",        // KeyPreview
			int64(42),         // TokensUsed
			(*time.Time)(nil), // LastUsedAt
			now,               // CreatedAt
			&rps,              // RateLimitRPS
			&burst,            // RateLimitBurst
			&providers,        // AllowedProviders
			true,              // StripReasoning
		},
	}

	vk, err := scanVirtualKey(row)
	if err != nil {
		t.Fatalf("scanVirtualKey() error: %v", err)
	}

	if vk.ID != id {
		t.Errorf("ID = %v, want %v", vk.ID, id)
	}
	if vk.Name != "test-key" {
		t.Errorf("Name = %q, want %q", vk.Name, "test-key")
	}
	if vk.KeyHash != "hash123" {
		t.Errorf("KeyHash = %q, want %q", vk.KeyHash, "hash123")
	}
	if vk.KeyPreview != "sk-...ab" {
		t.Errorf("KeyPreview = %q, want %q", vk.KeyPreview, "sk-...ab")
	}
	if vk.TokensUsed != 42 {
		t.Errorf("TokensUsed = %d, want 42", vk.TokensUsed)
	}
	if vk.LastUsedAt != nil {
		t.Error("LastUsedAt should be nil")
	}
	if vk.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", vk.CreatedAt, now)
	}
	if vk.RateLimitRPS == nil || *vk.RateLimitRPS != 10.5 {
		t.Errorf("RateLimitRPS = %v, want 10.5", vk.RateLimitRPS)
	}
	if vk.RateLimitBurst == nil || *vk.RateLimitBurst != 20 {
		t.Errorf("RateLimitBurst = %v, want 20", vk.RateLimitBurst)
	}
	if vk.AllowedProviders == nil || len(*vk.AllowedProviders) != 2 {
		t.Errorf("AllowedProviders = %v, want [provider-1, provider-2]", vk.AllowedProviders)
	}
	if !vk.StripReasoning {
		t.Error("StripReasoning should be true")
	}
}

func TestScanVirtualKey_ScanError(t *testing.T) {
	scanErr := errors.New("db scan failure")
	row := &mockVKScanner{err: scanErr}

	vk, err := scanVirtualKey(row)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want %v", err, scanErr)
	}
	if vk != nil {
		t.Error("expected nil VirtualKey on error")
	}
}

func TestScanVirtualKey_NilOptionalFields(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Millisecond).UTC()

	row := &mockVKScanner{
		values: []any{
			id,                // ID
			"minimal-key",     // Name
			"hash-min",        // KeyHash
			"sk-...mn",        // KeyPreview
			int64(0),          // TokensUsed
			(*time.Time)(nil), // LastUsedAt
			now,               // CreatedAt
			(*float64)(nil),   // RateLimitRPS
			(*int)(nil),       // RateLimitBurst
			(*[]string)(nil),  // AllowedProviders
			false,             // StripReasoning
		},
	}

	vk, err := scanVirtualKey(row)
	if err != nil {
		t.Fatalf("scanVirtualKey() error: %v", err)
	}

	if vk.RateLimitRPS != nil {
		t.Errorf("RateLimitRPS should be nil, got %v", *vk.RateLimitRPS)
	}
	if vk.RateLimitBurst != nil {
		t.Errorf("RateLimitBurst should be nil, got %v", *vk.RateLimitBurst)
	}
	if vk.AllowedProviders != nil {
		t.Errorf("AllowedProviders should be nil, got %v", *vk.AllowedProviders)
	}
	if vk.StripReasoning {
		t.Error("StripReasoning should be false")
	}
}
