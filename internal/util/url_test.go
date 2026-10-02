package util

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestSanitizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"trailing slash", "https://api.example.com/", "https://api.example.com"},
		{"no trailing slash", "https://api.example.com", "https://api.example.com"},
		{"double trailing slash", "https://api.example.com//", "https://api.example.com/"},
		{"empty", "", ""},
		{"just slash", "/", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeBaseURL(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "hello", []string{"hello"}},
		{"comma separated", "a, b, c", []string{"a", "b", "c"}},
		{"with empty parts", "a,,b", []string{"a", "b"}},
		{"spaces only", "   ", nil},
		{"mixed", " a , , b ", []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitAndTrim(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("SplitAndTrim(%q) = %v (len=%d), want %v (len=%d)", tc.value, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("SplitAndTrim(%q)[%d] = %q, want %q", tc.value, i, got[i], tc.want[i])
				}
			}
		})
	}
}
func TestSanitizeAPIURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"trailing slash and /v1", "https://api.example.com/v1/", "https://api.example.com"},
		{"trailing /v1 no slash", "https://api.example.com/v1", "https://api.example.com"},
		{"trailing slash no /v1", "https://api.example.com/", "https://api.example.com"},
		{"no trailing slash or /v1", "https://api.example.com", "https://api.example.com"},
		{"double trailing slash /v1", "https://api.example.com/v1//", "https://api.example.com/v1/"},
		{"empty", "", ""},
		{"/v1 in path not suffix", "https://api.example.com/v1/models", "https://api.example.com/v1/models"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeAPIURL(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeAPIURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRedactURLUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		`parse "http://u:SUPERSECRET@[::1": missing ']' in host`:        `parse "http://***@[::1": missing ']' in host`,
		`https://user%40mail.com:pw@host.example/v1 and https://x:y@b/`: `https://***@host.example/v1 and https://***@b/`,
		`https://host.example/v1?token=abc`:                             `https://host.example/v1?token=abc`,
		`no url here`:                                                   `no url here`,
		`https://us,er;x:pw@host.example/`:                              `https://***@host.example/`,
	} {
		if got := RedactURLUserinfo(in); got != want {
			t.Errorf("RedactURLUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURLParseReason(t *testing.T) {
	reason := errors.New("net/url: invalid userinfo")
	err := fmt.Errorf("wrapped: %w", &url.Error{Op: "parse", URL: "http://operator:secret@example.invalid", Err: reason})
	if got := URLParseReason(err); !errors.Is(got, reason) || strings.Contains(got.Error(), "secret") {
		t.Errorf("URLParseReason = %q, want the reason alone", got)
	}
	other := errors.New("not a parse error")
	if got := URLParseReason(other); !errors.Is(got, other) {
		t.Errorf("URLParseReason changed a non-url error: %v", got)
	}
	// The reason quotes the bytes it refused, and a key pasted into the port or
	// the host lands there.
	for _, raw := range []string{"http://host:sk-SECRET123/v1", "http://[sk-SECRET123]/v1"} {
		_, parseErr := url.Parse(raw)
		got := URLParseReason(parseErr)
		if got == nil || strings.Contains(got.Error(), "SECRET") || !strings.Contains(got.Error(), `"***"`) {
			t.Errorf("URLParseReason(%q) = %v, want the quoted bytes masked", raw, got)
		}
	}
}

func TestIsCredentialQueryParam(t *testing.T) {
	for _, name := range []string{"key", "API_KEY", "Token", "password", "client_secret", "X-Goog-Api-Key", "X-Amz-Signature", "sig", "api_token", "api-token", "apiToken", "accessToken", "client_token", "auth-password"} {
		if !IsCredentialQueryParam(name) {
			t.Errorf("IsCredentialQueryParam(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"api-version", "alt", "keys", ""} {
		if IsCredentialQueryParam(name) {
			t.Errorf("IsCredentialQueryParam(%q) = true, want false", name)
		}
	}
}
