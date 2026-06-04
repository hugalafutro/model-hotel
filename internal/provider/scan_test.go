package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockScanner implements scanner for testing.
type mockScanner struct {
	values []any
	err    error
}

func (m *mockScanner) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	for i, d := range dest {
		if i >= len(m.values) {
			break
		}
		// Simple pointer assignment — dest are pointers to struct fields.
		switch dst := d.(type) {
		case *uuid.UUID:
			*dst = m.values[i].(uuid.UUID)
		case *string:
			*dst = m.values[i].(string)
		case *[]byte:
			*dst = m.values[i].([]byte)
		case **string:
			*dst = m.values[i].(*string)
		case *bool:
			*dst = m.values[i].(bool)
		case *time.Time:
			*dst = m.values[i].(time.Time)
		case **time.Time:
			*dst = m.values[i].(*time.Time)
		}
	}
	return nil
}

func TestScanProvider_Success(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Millisecond).UTC()
	masked := "sk...ab"

	row := &mockScanner{
		values: []any{
			id,                        // ID
			"test-provider",           // Name
			"https://api.example.com", // BaseURL
			[]byte("encrypted"),       // EncryptedKey
			[]byte("nonce"),           // KeyNonce
			[]byte("salt"),            // KeySalt
			&masked,                   // MaskedKey
			true,                      // Enabled
			true,                      // AutodiscoveryEnabled
			(*time.Time)(nil),         // LastDiscoveredAt
			(*time.Time)(nil),         // LastUsedAt
			now,                       // CreatedAt
			now,                       // UpdatedAt
		},
	}

	p, err := scanProvider(row)
	if err != nil {
		t.Fatalf("scanProvider() error: %v", err)
	}

	if p.ID != id {
		t.Errorf("ID = %v, want %v", p.ID, id)
	}
	if p.Name != "test-provider" {
		t.Errorf("Name = %q, want %q", p.Name, "test-provider")
	}
	if p.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, "https://api.example.com")
	}
	if !p.Enabled {
		t.Error("Enabled should be true")
	}
	if !p.AutodiscoveryEnabled {
		t.Error("AutodiscoveryEnabled should be true")
	}
	if p.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", p.CreatedAt, now)
	}
	if p.UpdatedAt != now {
		t.Errorf("UpdatedAt = %v, want %v", p.UpdatedAt, now)
	}
	if p.LastDiscoveredAt != nil {
		t.Error("LastDiscoveredAt should be nil")
	}
	if p.LastUsedAt != nil {
		t.Error("LastUsedAt should be nil")
	}
	if p.MaskedKey == nil || *p.MaskedKey != masked {
		t.Errorf("MaskedKey = %v, want %q", p.MaskedKey, masked)
	}
}

func TestScanProvider_ScanError(t *testing.T) {
	scanErr := errors.New("db scan failure")
	row := &mockScanner{err: scanErr}

	p, err := scanProvider(row)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want %v", err, scanErr)
	}
	if p != nil {
		t.Error("expected nil provider on error")
	}
}
