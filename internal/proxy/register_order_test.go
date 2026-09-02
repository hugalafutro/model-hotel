package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// authOKRepo answers every key lookup with a live key, so a request through
// Register reaches the middlewares mounted after the key check.
type authOKRepo struct{ *mockVirtualKeyRepoWithFuncs }

func (authOKRepo) FindByKeyHash(_ context.Context, keyHash string) (*VirtualKeyInfo, error) {
	return &VirtualKeyInfo{ID: "vk-1", Name: "order-test", KeyHash: keyHash}, nil
}

// The middlewares Register is handed run after the virtual-key check and
// before the handler: an unauthenticated request is refused with its body
// untouched, and an authenticated one reaches them with the key in context.
func TestRegister_AfterAuthMiddlewaresSeeOnlyAuthenticatedRequests(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.virtualKeyRepo = authOKRepo{&mockVirtualKeyRepoWithFuncs{}}

	var ran int
	var sawKey bool
	probe := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran++
			_, sawKey = r.Context().Value(virtualKeyNameKey).(string)
			// Consume the body the way the real peek does, then answer here:
			// the ordering is the subject, not the handler behind it (the
			// unit handler has no database to serve a request with).
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	r := chi.NewRouter()
	h.Register(r, probe)

	// No key: 401 from the headers, the probe never runs, the body is unread.
	body := &countingReader{Reader: strings.NewReader(`{"model":"p/m","messages":[]}`)}
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", body)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status = %d, want 401", rr.Code)
	}
	if ran != 0 || body.read != 0 {
		t.Fatalf("no key: probe ran %d times and %d body bytes were read, want none of either", ran, body.read)
	}
	// The refusal closes the connection, so net/http does not drain the
	// unread body before answering: a trickling client is told 401 at once.
	if rr.Header().Get("Connection") != "close" {
		t.Fatalf("no key: Connection = %q, want close", rr.Header().Get("Connection"))
	}

	// A key: the probe runs, after auth (the key is already in context).
	req = httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"p/m","messages":[]}`))
	req.Header.Set("Authorization", "Bearer sk-order-test-key-0123456789")
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if ran != 1 {
		t.Fatalf("with a key: probe ran %d times, want once", ran)
	}
	if !sawKey {
		t.Fatal("with a key: the probe ran before the key check")
	}
}

type countingReader struct {
	io.Reader
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.read += n
	return n, err
}
