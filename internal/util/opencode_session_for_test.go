package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenCodeGoSessionFor(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	r.Header.Set(OpenCodeGoSessionHeader, " abc-123 ")
	if got, want := OpenCodeGoSessionFor(r, "hash"), OpenCodeGoSession(" abc-123 ", "hash"); got != want {
		t.Fatalf("OpenCodeGoSessionFor = %q, want %q", got, want)
	}
	if got, want := OpenCodeGoSessionFor(httptest.NewRequest(http.MethodPost, "/", http.NoBody), "hash"), OpenCodeGoSession("", "hash"); got != want {
		t.Fatalf("no header: %q, want %q", got, want)
	}
}
