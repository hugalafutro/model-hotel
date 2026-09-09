package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// OpenCode Go refuses every chat request that carries no x-opencode-session,
// so the gateway always sends one: the client's own id when it is safe to
// forward, otherwise a stable id it derives itself. The mh- namespace is the
// gateway's own (admin chat, the retirement probe, the Test button, and the
// per-key derived ids), so a client id claiming it is treated as absent rather
// than letting a key holder share a session that is not its own.
func TestOpenCodeGoSession(t *testing.T) {
	t.Parallel()

	const vkHash = "6f1b2c3d4e5f60718293a4b5c6d7e8f900112233445566778899aabbccddeeff"
	fallback := OpenCodeGoSession("", vkHash)

	tests := []struct {
		name          string
		clientSession string
		vkHash        string
		want          string
	}{
		{"client id is forwarded unchanged", "ses_abc123", vkHash, "ses_abc123"},
		{"client id is trimmed", "  ses_abc123\t", vkHash, "ses_abc123"},
		{"blank client id falls back", "   ", vkHash, fallback},
		{"over-long client id is treated as absent", strings.Repeat("a", 129), vkHash, fallback},
		{"client id at the bound is kept", strings.Repeat("a", 128), vkHash, strings.Repeat("a", 128)},
		{"header injection is treated as absent", "ses\r\nx-evil: 1", vkHash, fallback},
		{"an interior tab is treated as absent", "ses\tabc", vkHash, fallback},
		{"DEL is treated as absent", "ses\x7fabc", vkHash, fallback},
		{"non-ascii is treated as absent", "sesión", vkHash, fallback},
		{"multi-byte utf-8 is treated as absent", "セッション", vkHash, fallback},
		{"the reserved prefix is refused", "mh-admin", vkHash, fallback},
		{"the reserved prefix is refused whatever its case", "MH-Probe", vkHash, fallback},
		{"keyless traffic that claims a gateway id still gets the admin one", "mh-model-test", "", OpenCodeGoAdminSession},
		{"keyless admin traffic gets the fixed id", "", "", OpenCodeGoAdminSession},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := OpenCodeGoSession(tc.clientSession, tc.vkHash); got != tc.want {
				t.Errorf("OpenCodeGoSession(%q, %q) = %q, want %q", tc.clientSession, tc.vkHash, got, tc.want)
			}
		})
	}
}

// The derived id has to be stable for a key (so a conversation keeps one
// upstream route and prompt cache) and opaque (so neither the key nor its hash
// leaves the gateway).
func TestOpenCodeGoSession_FallbackIsStableOpaqueAndPerKey(t *testing.T) {
	t.Parallel()

	const hashA = "6f1b2c3d4e5f60718293a4b5c6d7e8f900112233445566778899aabbccddeeff"
	const hashB = "ff00112233445566778899aabbccddeeff6f1b2c3d4e5f60718293a4b5c6d7e8"

	first := OpenCodeGoSession("", hashA)
	if second := OpenCodeGoSession("", hashA); first != second {
		t.Errorf("the same key must always map to the same session, got %q then %q", first, second)
	}
	if other := OpenCodeGoSession("", hashB); other == first {
		t.Errorf("two keys must not share a session, both got %q", first)
	}
	if !strings.HasPrefix(first, "mh-") || len(first) != len("mh-")+32 {
		t.Errorf("unexpected session shape: %q", first)
	}
	if strings.Contains(first, hashA) {
		t.Errorf("the session must not carry the key hash: %q", first)
	}
}

// The header is an OpenCode Go requirement; every other upstream must see the
// request it saw before.
func TestSetOpenCodeGoSession_OnlyOpenCodeGo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerType string
		session      string
		want         string
	}{
		{"opencode-go is stamped", "opencode-go", "ses_abc", "ses_abc"},
		{"openai is not", "openai", "ses_abc", ""},
		{"opencode-zen is not", "opencode-zen", "ses_abc", ""},
		{"an empty session is never stamped", "opencode-go", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", http.NoBody)
			SetOpenCodeGoSession(req, tc.providerType, tc.session)
			if got := req.Header.Get(OpenCodeGoSessionHeader); got != tc.want {
				t.Errorf("%s header = %q, want %q", OpenCodeGoSessionHeader, got, tc.want)
			}
		})
	}
}
