package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

func strs(v ...string) *[]string { s := v; return &s }

func TestEffectiveAllowedProviders(t *testing.T) {
	tests := []struct {
		name  string
		key   *[]string
		owner *[]string
		want  *[]string
	}{
		{"neither restricts", nil, nil, nil},
		{"only the key restricts", strs("p1"), nil, strs("p1")},
		{"only the owner restricts", nil, strs("p1"), strs("p1")},
		{"intersection", strs("p1", "p2"), strs("p2", "p3"), strs("p2")},
		{"owner narrowed below the key", strs("p1", "p2"), strs("p1"), strs("p1")},
		{"disjoint yields none", strs("p1"), strs("p2"), strs()},
		{"empty owner cap yields none", strs("p1"), strs(), strs()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveAllowedProviders(tt.key, tt.owner)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("got %v, want nil (unrestricted)", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("got nil (unrestricted), want %v", *tt.want)
			case tt.want == nil:
				return
			}
			if len(*got) != len(*tt.want) {
				t.Fatalf("got %v, want %v", *got, *tt.want)
			}
			for i := range *tt.want {
				if (*got)[i] != (*tt.want)[i] {
					t.Fatalf("got %v, want %v", *got, *tt.want)
				}
			}
		})
	}
}

// seedOwnedCappedKey creates a user with the given account cap and a virtual
// key owned by them carrying the given key-level list, both written straight
// through the repositories. That is deliberate: the dashboard endpoints would
// refuse a key list wider than the owner's cap, but a fleet config-sync import
// and an admin narrowing the cap after the fact both produce exactly this row,
// so the proxy has to be tested against it. Returns the key's plaintext.
func seedOwnedCappedKey(t *testing.T, ownerCap, keyAllowed []string) string {
	t.Helper()
	ctx := context.Background()
	pool := testDB.Pool()
	suffix := uuid.New().String()[:8]

	var ownerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, enabled, allowed_providers)
		 VALUES ($1, 'x', true, $2) RETURNING id`,
		"cap-owner-"+suffix, ownerCap).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, ownerID) })

	repo := virtualkey.NewRepository(pool)
	plaintext := "cap-key-" + suffix
	keyHash := virtualkey.Hash(plaintext)
	var allowed *[]string
	if keyAllowed != nil {
		allowed = &keyAllowed
	}
	created, err := repo.Create(ctx, plaintext, keyHash, "sk-...cp", nil, nil, nil, allowed, nil, &ownerID)
	if err != nil {
		t.Fatalf("seed owned key: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), created.ID) })
	return plaintext
}

// The whole wall, end to end through the real repository and the real auth
// middleware: the key names the provider serving the model, the owner's account
// cap does not, and the request is refused. Nothing but the runtime
// intersection stops this request, since the stored key list on its own would
// have allowed it.
func TestChatCompletions_OwnerCapDeniesProviderTheKeyAllows(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	defer env.Handler.upstreamTransport.CloseIdleConnections()

	// The cap names some other provider entirely, so the intersection is empty.
	plaintext := seedOwnedCappedKey(t, []string{uuid.New().String()}, []string{env.ProviderID.String()})

	w := doCappedRequest(t, env, plaintext)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "virtual key does not have access to any provider") {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
	// The refusal must not name the owner or their cap: a proxy client learns it
	// lacks access, not whose rule denied it.
	if strings.Contains(w.Body.String(), "owner") || strings.Contains(w.Body.String(), "account") {
		t.Errorf("403 body leaks which side denied: %s", w.Body.String())
	}
}

// The control for the test above: same shape, but the cap includes the
// provider, so the intersection is non-empty and the request goes upstream.
// Without this the 403 above could be produced by any unrelated breakage in the
// seeding or the middleware.
func TestChatCompletions_OwnerCapAllowsProviderInBothLists(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	defer env.Handler.upstreamTransport.CloseIdleConnections()

	plaintext := seedOwnedCappedKey(t, []string{env.ProviderID.String()}, []string{env.ProviderID.String()})

	w := doCappedRequest(t, env, plaintext)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
}

// An uncapped key still inherits its owner's cap: nil on the key side means the
// key adds no restriction of its own, not that it escapes the account's.
func TestChatCompletions_OwnerCapAppliesToUnrestrictedKey(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	defer env.Handler.upstreamTransport.CloseIdleConnections()

	plaintext := seedOwnedCappedKey(t, []string{uuid.New().String()}, nil)

	w := doCappedRequest(t, env, plaintext)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", w.Code, w.Body.String())
	}
}

// doCappedRequest drives a non-streaming completion for env's model through the
// real ProxyKeyMiddleware, so the owner cap travels the same repository →
// context → filter path it does in production.
func doCappedRequest(t *testing.T, env *testProxyEnv, plaintext string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	env.Handler.ProxyKeyMiddleware(http.HandlerFunc(env.Handler.ChatCompletions)).ServeHTTP(w, req)
	return w
}
