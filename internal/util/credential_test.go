package util

import (
	"strings"
	"testing"
)

// The shapes an upstream has actually been seen to quote back, plus the prose
// that must survive. A provider that echoes the operator's key in an auth
// failure is the whole reason this layer exists.
func TestMaskKeyShapedTokens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     string
		masked bool
	}{
		{"openai style", `{"message":"Incorrect API key provided: sk-test-1234567890abcdefghij."}`, true},
		{"project key", "sk-proj-abcdefghij1234567890abcdef", true},
		{"anthropic style", "sk-ant-api03-abcdefghij1234567890", true},
		{"groq", "gsk_abcdefghij1234567890abcd", true},
		{"xai", "xai-abcdefghij1234567890abcd", true},
		{"huggingface", "hf_abcdefghij1234567890abcd", true},
		{"fireworks", "fw_abcdefghij1234567890abcd", true},
		{"replicate", "r8_abcdefghij1234567890abcd", true},
		{"google", "AIzaSyA1234567890abcdefghijklmnopqrstuv", true},
		{"aws access key id", "AKIA1234567890ABCDEF", true},
		{"bare jwt", "eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0.SflKxwRJSMeKK", true},
		{"bearer token", "Authorization: Bearer abcdefghij1234567890", true},

		// Prose and identifiers keep their text: a match with no digit is not a
		// credential, and a short tail is not one either.
		{"prose without digits", "sk_business_unit_identifier_for_billing", false},
		// The same prose in a body that DOES carry digits elsewhere, so the
		// whole-body fast path cannot short-circuit it and the per-match digit
		// rule is what has to spare it.
		{"prose without digits beside a number", "sk_business_unit_identifier_for_billing was charged 42 times", false},
		{"bearer prose", "Bearer authentication-required", false},
		{"short tail", "the sk-abc prefix", false},
		{"plain sentence", "the provider rejected this request", false},
		{"model id", "openai/gpt-4o-mini-2024-07-18", false},
		{"hotel group", "hotel/glm53", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(MaskKeyShapedTokens([]byte(tc.in)))
			if masked := strings.Contains(got, "[redacted]"); masked != tc.masked {
				t.Errorf("MaskKeyShapedTokens(%q) = %q, masked=%v want %v", tc.in, got, masked, tc.masked)
			}
			if !tc.masked && got != tc.in {
				t.Errorf("unmasked input was rewritten: %q -> %q", tc.in, got)
			}
		})
	}
}

// The exact layer is what covers a key shape the prefix list cannot anticipate,
// which is the case for every self-hosted gateway.
func TestMaskCredential(t *testing.T) {
	const custom = "my-custom-gateway-credential-2026"
	body := `{"error":"bad key ` + custom + ` supplied"}`
	got := MaskCredential(custom, body)
	if strings.Contains(got, custom) {
		t.Errorf("exact credential survived: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("no redaction marker: %q", got)
	}

	// Both layers, in one body: the gateway's own key and a different one.
	two := MaskCredential(custom, custom+" and sk-other-1234567890abcdefgh")
	if strings.Contains(two, custom) || strings.Contains(two, "sk-other-1234567890abcdefgh") {
		t.Errorf("a layer did not run: %q", two)
	}

	// A key too short to mask safely is left alone rather than shredding the
	// body: rewriting every occurrence of a few characters protects nothing.
	if got := MaskCredential("abc", "abcdefg is prose containing abc"); strings.Contains(got, "[redacted]") {
		t.Errorf("a sub-minimum credential was masked: %q", got)
	}

	// An empty key (a keyless local provider) masks by shape only.
	if got := MaskCredential("", "key sk-test-1234567890abcdefghij here"); !strings.Contains(got, "[redacted]") {
		t.Errorf("shape layer did not run for an empty credential: %q", got)
	}
}

// SanitizeLogBody is what every path outside internal/proxy reaches for before
// writing an upstream body to a log or a column, so the credential layer has
// to be part of it.
func TestSanitizeLogBody_ScrubsCredentials(t *testing.T) {
	body := `{"error":{"message":"Incorrect API key provided: sk-test-1234567890abcdefghij."}}`
	got := SanitizeLogBody(body, 10000)
	if strings.Contains(got, "sk-test-1234567890abcdefghij") {
		t.Errorf("key survived SanitizeLogBody: %q", got)
	}
	if !strings.Contains(got, "Incorrect API key provided") {
		t.Errorf("the diagnostic text was lost: %q", got)
	}
}

// The UUID layer still runs, and truncation still happens before the scrub.
func TestSanitizeLogBody_KeepsItsExistingJobs(t *testing.T) {
	got := SanitizeLogBody("team 793ac38b-0211-43e6-baa7-aa7054c39931 denied", 10000)
	if strings.Contains(got, "793ac38b") || !strings.Contains(got, "[REDACTED]") {
		t.Errorf("UUID redaction regressed: %q", got)
	}
	if got := SanitizeLogBody(strings.Repeat("a", 50), 10); len([]rune(got)) != 11 {
		t.Errorf("truncation regressed: %d runes", len([]rune(got)))
	}
}

// The classification paths read a sanitized body and look for the requested
// model's own id beside a retirement phrase, so a scrub that ate model ids
// would silently change routing. None of the ids on the live fleet matches.
func TestSanitizeLogBody_LeavesModelIdsIntact(t *testing.T) {
	for _, id := range []string{
		"gpt-5.1-codex", "claude-sonnet-4-20250514", "glm-5.3", "hotel/glm53",
		"deepseek-v4-pro", "grok-4-0709", "gemini-2.5-pro", "kimi-k2.7-code",
		"zai-org/GLM-4.5-Air:thinking", "Doctor-Shotgun/MS3.2-24B-Magnum-Diamond",
		"minimax-m3", "qwen3-coder-480b-a35b-instruct",
	} {
		body := `{"error":{"message":"The model ` + id + ` does not exist"}}`
		if got := SanitizeLogBody(body, 10000); !strings.Contains(got, id) {
			t.Errorf("model id %q was scrubbed out of %q", id, got)
		}
	}
}
