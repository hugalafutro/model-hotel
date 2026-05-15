package ctxkeys

import (
	"bytes"
	"context"
	"testing"
)

func TestContextKeyTypePreventsCollisions(t *testing.T) {
	// Verify that the unexported contextKey type prevents collisions with
	// plain string keys. A context value stored under a plain string key
	// must not be retrievable via our typed constants.
	//nolint:revive,staticcheck // intentionally using a plain string key to test type isolation
	ctx := context.WithValue(context.Background(), "virtual_key_hash", "wrong")
	if v := ctx.Value(VirtualKeyHashKey); v != nil {
		t.Error("VirtualKeyHashKey should not match a plain string key")
	}
}

func TestAllKeysAreDistinct(t *testing.T) {
	keys := []contextKey{
		VirtualKeyHashKey,
		RequestBodyKey,
		SettingsReadMsKey,
		SafeDialMsKey,
		VirtualKeyRateLimitRPSKey,
		VirtualKeyRateLimitBurstKey,
		CancelOriginKey,
	}
	seen := make(map[contextKey]string, len(keys))
	for _, k := range keys {
		if prev, ok := seen[k]; ok {
			t.Errorf("duplicate context key %q also used by %s", k, prev)
		}
		seen[k] = string(k)
	}
}

func TestVirtualKeyHashKeyRoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), VirtualKeyHashKey, "sha256:abc123")
	v, ok := ctx.Value(VirtualKeyHashKey).(string)
	if !ok || v != "sha256:abc123" {
		t.Errorf("VirtualKeyHashKey round-trip failed: got %q, ok=%v", v, ok)
	}
}

func TestRequestBodyKeyRoundTrip(t *testing.T) {
	body := []byte(`{"model":"gpt-4"}`)
	ctx := context.WithValue(context.Background(), RequestBodyKey, body)
	v, ok := ctx.Value(RequestBodyKey).([]byte)
	if !ok || !bytes.Equal(v, body) {
		t.Errorf("RequestBodyKey round-trip failed: got %v, ok=%v", v, ok)
	}
}

func TestSettingsReadMsKeyRoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), SettingsReadMsKey, float64(3.14))
	v, ok := ctx.Value(SettingsReadMsKey).(float64)
	if !ok || v != 3.14 {
		t.Errorf("SettingsReadMsKey round-trip failed: got %v, ok=%v", v, ok)
	}
}

func TestSafeDialMsKeyRoundTrip(t *testing.T) {
	var val float64
	ctx := context.WithValue(context.Background(), SafeDialMsKey, &val)
	p, ok := ctx.Value(SafeDialMsKey).(*float64)
	if !ok {
		t.Fatalf("SafeDialMsKey round-trip failed: not a *float64")
	}
	*p = 42.0
	if val != 42.0 {
		t.Errorf("SafeDialMsKey pointer write failed: got %v, want 42.0", val)
	}
}

func TestVirtualKeyRateLimitRPSKeyRoundTrip(t *testing.T) {
	val := 10.0
	ctx := context.WithValue(context.Background(), VirtualKeyRateLimitRPSKey, &val)
	p, ok := ctx.Value(VirtualKeyRateLimitRPSKey).(*float64)
	if !ok || *p != 10.0 {
		t.Errorf("VirtualKeyRateLimitRPSKey round-trip failed: got %v, ok=%v", p, ok)
	}
}

func TestVirtualKeyRateLimitBurstKeyRoundTrip(t *testing.T) {
	val := 20
	ctx := context.WithValue(context.Background(), VirtualKeyRateLimitBurstKey, &val)
	p, ok := ctx.Value(VirtualKeyRateLimitBurstKey).(*int)
	if !ok || *p != 20 {
		t.Errorf("VirtualKeyRateLimitBurstKey round-trip failed: got %v, ok=%v", p, ok)
	}
}

func TestCancelOriginKeyRoundTrip(t *testing.T) {
	for _, origin := range []string{"client_disconnect", "failover_timeout", "retry_timeout"} {
		ctx := context.WithValue(context.Background(), CancelOriginKey, origin)
		v, ok := ctx.Value(CancelOriginKey).(string)
		if !ok || v != origin {
			t.Errorf("CancelOriginKey round-trip failed for %q: got %q, ok=%v", origin, v, ok)
		}
	}
}

func TestCancelOriginKeyNilWhenUnset(t *testing.T) {
	ctx := context.Background()
	if v := ctx.Value(CancelOriginKey); v != nil {
		t.Errorf("CancelOriginKey should be nil when unset, got %v", v)
	}
}

func TestContextKeyStringValues(t *testing.T) {
	// Verify the string values are stable — they're used in cross-package
	// context lookups and must not change without coordination.
	tests := []struct {
		key  contextKey
		want string
	}{
		{VirtualKeyHashKey, "virtual_key_hash"},
		{RequestBodyKey, "request_body"},
		{SettingsReadMsKey, "settings_read_ms"},
		{SafeDialMsKey, "safe_dial_ms"},
		{VirtualKeyRateLimitRPSKey, "virtual_key_rate_limit_rps"},
		{VirtualKeyRateLimitBurstKey, "virtual_key_rate_limit_burst"},
		{CancelOriginKey, "cancel_origin"},
	}
	for _, tt := range tests {
		if string(tt.key) != tt.want {
			t.Errorf("contextKey string value: got %q, want %q", tt.key, tt.want)
		}
	}
}
