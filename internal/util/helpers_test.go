package util

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMillisSinceAndMs(t *testing.T) {
	if got := Ms(1500 * time.Microsecond); got != 1.5 {
		t.Errorf("Ms(1500us) = %v, want 1.5", got)
	}
	if got := Ms(0); got != 0 {
		t.Errorf("Ms(0) = %v, want 0", got)
	}
	start := time.Now().Add(-2 * time.Millisecond)
	if got := MillisSince(start); got < 2 {
		t.Errorf("MillisSince(2ms ago) = %v, want >= 2", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"within cap", "hello", 10, "hello"},
		{"at cap", "hello", 5, "hello"},
		{"cut marks ellipsis", "hello", 3, "hel…"},
		{"multi-byte not split", "héllo wörld", 4, "héll…"},
		{"zero cap", "hello", 0, ""},
		{"negative cap", "hello", -1, ""},
		{"empty input", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateRunes(tt.in, tt.n); got != tt.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestTruncateBytes(t *testing.T) {
	if got := TruncateBytes("hello", 10); got != "hello" {
		t.Errorf("within cap = %q", got)
	}
	// "é" is two bytes: a cut at 2 must not land inside it.
	if got := TruncateBytes("aé", 2); got != "a" {
		t.Errorf("mid-rune cut = %q, want %q", got, "a")
	}
	if got := TruncateBytes("a\xffb", 10); got != "ab" {
		t.Errorf("invalid UTF-8 not dropped: %q", got)
	}
	if got := TruncateBytes("abc", 0); got != "" {
		t.Errorf("zero cap = %q", got)
	}
}

func TestCollapseSpace(t *testing.T) {
	if got := CollapseSpace("  a\n\tb   c "); got != "a b c" {
		t.Errorf("CollapseSpace = %q", got)
	}
	if got := CollapseSpace("   "); got != "" {
		t.Errorf("all whitespace = %q", got)
	}
}

func TestSleepContext(t *testing.T) {
	if err := SleepContext(context.Background(), 0); err != nil {
		t.Errorf("zero duration returned %v", err)
	}
	if err := SleepContext(context.Background(), -time.Second); err != nil {
		t.Errorf("negative duration returned %v", err)
	}
	if err := SleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("short sleep returned %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepContext(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context returned %v, want context.Canceled", err)
	}
}

func TestSHA256Hex(t *testing.T) {
	// Known digest of the empty string.
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := SHA256Hex(""); got != empty {
		t.Errorf("SHA256Hex(\"\") = %q", got)
	}
	if SHA256Hex("a") == SHA256Hex("b") {
		t.Error("different inputs produced the same digest")
	}
}

func TestRandomHexAndMintHexToken(t *testing.T) {
	s, err := RandomHex(16)
	if err != nil {
		t.Fatalf("RandomHex: %v", err)
	}
	if len(s) != 32 {
		t.Errorf("RandomHex(16) length = %d, want 32", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		t.Errorf("RandomHex output is not hex: %v", err)
	}
	other, err := RandomHex(16)
	if err != nil {
		t.Fatalf("RandomHex: %v", err)
	}
	if s == other {
		t.Error("two RandomHex calls returned the same value")
	}

	token, hash, err := MintHexToken(32)
	if err != nil {
		t.Fatalf("MintHexToken: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}
	if hash != SHA256Hex(token) {
		t.Error("MintHexToken hash does not match the token digest")
	}
}

func TestDecodeCountsTolerant(t *testing.T) {
	type usage struct {
		Prompt int `json:"prompt_tokens"`
	}
	var u usage
	// A member with no struct counterpart whose type is wrong is tolerated,
	// and the members that did decode survive.
	if err := DecodeCountsTolerant([]byte(`{"prompt_tokens":7,"extra":{"a":1}}`), &u); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Prompt != 7 {
		t.Errorf("prompt = %d, want 7", u.Prompt)
	}
	var bad usage
	if err := DecodeCountsTolerant([]byte(`{"prompt_tokens":`), &bad); err == nil {
		t.Error("malformed JSON was tolerated")
	}
	var notObject usage
	if err := DecodeCountsTolerant([]byte(`42`), &notObject); err == nil {
		t.Error("a non-object document was tolerated")
	}
	var quoted usage
	if err := DecodeCountsTolerant([]byte(`{"prompt_tokens":"7"}`), &quoted); err != nil {
		t.Fatalf("quoted count not coerced: %v", err)
	}
	if quoted.Prompt != 7 {
		t.Errorf("coerced prompt = %d, want 7", quoted.Prompt)
	}
}

func TestErrorEnvelopeMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"object form", `{"error":{"message":"bad param"}}`, "bad param"},
		{"array form", `[{"error":{"message":"bad param"}}]`, "bad param"},
		{"array joins", `[{"error":{"message":"a"}},{"error":{"message":"b"}}]`, "a; b"},
		{"no error member", `{"ok":true}`, ""},
		{"malformed", `not json`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorEnvelopeMessage([]byte(tt.body)); got != tt.want {
				t.Errorf("ErrorEnvelopeMessage(%s) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestSecretMaskAndUnstampedCommit(t *testing.T) {
	if strings.TrimLeft(SecretMask, "*") != "" || SecretMask == "" {
		t.Errorf("SecretMask = %q, want a run of asterisks", SecretMask)
	}
	if ShortCommit(UnstampedCommit) != UnstampedCommit {
		t.Error("ShortCommit truncated the unstamped sentinel")
	}
}
