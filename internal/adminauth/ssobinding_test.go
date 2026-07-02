package adminauth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// fakeResolver implements SSOUserResolver in memory (emails pre-normalized).
type fakeResolver struct {
	byEmail map[string]*user.User
}

func (f *fakeResolver) GetByEmail(_ context.Context, email string) (*user.User, error) {
	if u, ok := f.byEmail[strings.ToLower(strings.TrimSpace(email))]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}

func boundUser(email string, enabled bool) (*user.User, *fakeResolver) {
	u := &user.User{
		ID:       uuid.New(),
		Username: "bound-" + email,
		Role:     user.RoleUser,
		Grants:   []string{"chat"},
		Enabled:  enabled,
	}
	return u, &fakeResolver{byEmail: map[string]*user.User{email: u}}
}

func TestResolveSSOUser(t *testing.T) {
	u, res := boundUser("worker@example.com", true)
	if got := resolveSSOUser(context.Background(), res, "worker@example.com"); got == nil || got.ID != u.ID {
		t.Errorf("expected binding, got %v", got)
	}
	if got := resolveSSOUser(context.Background(), res, "other@example.com"); got != nil {
		t.Errorf("unexpected binding for unknown email: %v", got)
	}
	if got := resolveSSOUser(context.Background(), nil, "worker@example.com"); got != nil {
		t.Errorf("nil resolver must yield nil, got %v", got)
	}
	_, disabledRes := boundUser("off@example.com", false)
	if got := resolveSSOUser(context.Background(), disabledRes, "off@example.com"); got != nil {
		t.Errorf("disabled user must not bind, got %v", got)
	}
}

// errResolver returns a caller-chosen (user, error) pair so the lookup-failure
// paths of resolveSSOUser can be exercised.
type errResolver struct {
	u   *user.User
	err error
}

func (e *errResolver) GetByEmail(context.Context, string) (*user.User, error) {
	return e.u, e.err
}

// A transient lookup error (e.g. a DB blip during the OIDC/GitHub callback)
// must deny the login cleanly, never panic dereferencing a nil user.
func TestResolveSSOUser_LookupErrorDenies(t *testing.T) {
	res := &errResolver{err: errors.New("db unavailable")}
	if got := resolveSSOUser(context.Background(), res, "worker@example.com"); got != nil {
		t.Errorf("lookup error must yield nil, got %v", got)
	}
	// Defensive: a resolver that returns (nil, nil) must also be handled without
	// a panic, regardless of the repository's not-found contract.
	if got := resolveSSOUser(context.Background(), &errResolver{}, "worker@example.com"); got != nil {
		t.Errorf("nil user with nil error must yield nil, got %v", got)
	}
}

// oidcTokenFromFragment extracts and unescapes the token or fails the test.
func oidcTokenFromFragment(t *testing.T, frag string) string {
	t.Helper()
	const prefix = "oidc_token="
	if !strings.HasPrefix(frag, prefix) {
		t.Fatalf("expected token fragment, got %q", frag)
	}
	token, err := url.QueryUnescape(strings.TrimPrefix(frag, prefix))
	if err != nil {
		t.Fatalf("unescape token: %v", err)
	}
	return token
}

// A verified email outside the admin allowlist logs in as the bound user: the
// session handle is the user's UUID, not "admin".
func TestOIDCUserEmailBinding(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, sessionMgr := newOIDCTestHandler(t, idp, "admin@example.com")
	u, res := boundUser("worker@example.com", true)
	h.SetUserResolver(res)

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "worker@example.com", true)

	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	token := oidcTokenFromFragment(t, frag)
	handle, ok := sessionMgr.TokenUser(context.Background(), token)
	if !ok {
		t.Fatal("bound-user token failed validation")
	}
	if string(handle) != u.ID.String() {
		t.Errorf("session handle = %q, want user uuid %q", handle, u.ID)
	}
}

// The admin allowlist wins over a user binding for the same email.
func TestOIDCAllowlistBeatsUserBinding(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, sessionMgr := newOIDCTestHandler(t, idp, "admin@example.com")
	_, res := boundUser("admin@example.com", true)
	h.SetUserResolver(res)

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)

	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	token := oidcTokenFromFragment(t, frag)
	handle, ok := sessionMgr.TokenUser(context.Background(), token)
	if !ok || string(handle) != "admin" {
		t.Errorf("allowlisted email should stay admin, got handle %q (ok=%v)", handle, ok)
	}
}

// A disabled bound user is denied, not downgraded or admin-escalated.
func TestOIDCDisabledUserBindingDenied(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, _ := newOIDCTestHandler(t, idp, "admin@example.com")
	_, res := boundUser("worker@example.com", false)
	h.SetUserResolver(res)

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "worker@example.com", true)

	frag := runCallback(t, h, cookie, url.Values{"state": {state}, "code": {"c"}})
	if !strings.Contains(frag, "oidc_error=") {
		t.Fatalf("expected error fragment for disabled bound user, got %q", frag)
	}
}

// GitHub: a verified email outside the allowlist binds to a user account.
func TestGitHubUserEmailBinding(t *testing.T) {
	m := newGitHubMock(t)
	m.behavior(func(g *githubMock) {
		g.emails = []githubEmail{{Email: "worker@example.com", Primary: true, Verified: true}}
	})
	h, _, sessionMgr := newGitHubTestHandler(t, m, "admin@example.com")
	u, res := boundUser("worker@example.com", true)
	h.SetUserResolver(res)

	loc, cookie := runGitHubStart(t, h)
	state := loc.Query().Get("state")
	frag := runGitHubCallback(t, h, cookie, url.Values{"state": {state}, "code": {"auth-code"}})
	token := oidcTokenFromFragment(t, frag)
	handle, ok := sessionMgr.TokenUser(context.Background(), token)
	if !ok {
		t.Fatal("bound-user token failed validation")
	}
	if string(handle) != u.ID.String() {
		t.Errorf("session handle = %q, want user uuid %q", handle, u.ID)
	}
}

// GitHub: an unverified email must not bind even when a user row matches.
func TestGitHubUnverifiedEmailNeverBinds(t *testing.T) {
	m := newGitHubMock(t)
	m.behavior(func(g *githubMock) {
		g.emails = []githubEmail{{Email: "worker@example.com", Primary: true, Verified: false}}
	})
	h, _, _ := newGitHubTestHandler(t, m, "admin@example.com")
	_, res := boundUser("worker@example.com", true)
	h.SetUserResolver(res)

	loc, cookie := runGitHubStart(t, h)
	state := loc.Query().Get("state")
	frag := runGitHubCallback(t, h, cookie, url.Values{"state": {state}, "code": {"auth-code"}})
	if !strings.Contains(frag, "oidc_error=") {
		t.Fatalf("expected error fragment for unverified email, got %q", frag)
	}
}
