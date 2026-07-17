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

// fakeResolver implements SSOUserResolver in memory (emails pre-normalized),
// mirroring the repository's trust-on-first-use binding: the first (provider,
// subject) to log in for an account locks it, and a later mismatch is denied.
type fakeResolver struct {
	byEmail map[string]*user.User
	bound   map[uuid.UUID][2]string // user id -> {provider, subject}
}

func (f *fakeResolver) ResolveSSOIdentity(_ context.Context, provider, subject, email string) (*user.User, error) {
	u, ok := f.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil, user.ErrNotFound
	}
	if !u.Enabled {
		return nil, user.ErrNotFound
	}
	if f.bound == nil {
		f.bound = map[uuid.UUID][2]string{}
	}
	id := [2]string{provider, subject}
	if cur, ok := f.bound[u.ID]; ok {
		if cur != id {
			return nil, user.ErrSSOMismatch
		}
	} else {
		f.bound[u.ID] = id
	}
	return u, nil
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
	if got := resolveSSOUser(context.Background(), res, "oidc", "sub-1", "worker@example.com"); got == nil || got.ID != u.ID {
		t.Errorf("expected binding, got %v", got)
	}
	if got := resolveSSOUser(context.Background(), res, "oidc", "sub-1", "other@example.com"); got != nil {
		t.Errorf("unexpected binding for unknown email: %v", got)
	}
	if got := resolveSSOUser(context.Background(), nil, "oidc", "sub-1", "worker@example.com"); got != nil {
		t.Errorf("nil resolver must yield nil, got %v", got)
	}
	_, disabledRes := boundUser("off@example.com", false)
	if got := resolveSSOUser(context.Background(), disabledRes, "oidc", "sub-1", "off@example.com"); got != nil {
		t.Errorf("disabled user must not bind, got %v", got)
	}
}

// The core of vuln-0001: once an account is bound to one provider identity, a
// second provider asserting the same verified email is denied, even though the
// email matches a real, enabled account.
func TestResolveSSOUser_CrossProviderDenied(t *testing.T) {
	u, res := boundUser("worker@example.com", true)

	// First login via OIDC binds the account.
	if got := resolveSSOUser(context.Background(), res, "oidc", "iss#abc", "worker@example.com"); got == nil || got.ID != u.ID {
		t.Fatalf("first OIDC login should bind, got %v", got)
	}
	// Same identity logs in again: still fine.
	if got := resolveSSOUser(context.Background(), res, "oidc", "iss#abc", "worker@example.com"); got == nil {
		t.Fatalf("same identity re-login should succeed, got nil")
	}
	// A GitHub login for the same email is a different identity: denied.
	if got := resolveSSOUser(context.Background(), res, "github", "424242", "worker@example.com"); got != nil {
		t.Errorf("cross-provider login must be denied, got %v", got)
	}
	// A different OIDC subject (same provider) is also denied.
	if got := resolveSSOUser(context.Background(), res, "oidc", "iss#other", "worker@example.com"); got != nil {
		t.Errorf("same provider, different subject must be denied, got %v", got)
	}
}

// errResolver returns a caller-chosen (user, error) pair so the lookup-failure
// paths of resolveSSOUser can be exercised.
type errResolver struct {
	u   *user.User
	err error
}

func (e *errResolver) ResolveSSOIdentity(context.Context, string, string, string) (*user.User, error) {
	return e.u, e.err
}

// A transient lookup error (e.g. a DB blip during the OIDC/GitHub callback)
// must deny the login cleanly, never panic dereferencing a nil user.
func TestResolveSSOUser_LookupErrorDenies(t *testing.T) {
	res := &errResolver{err: errors.New("db unavailable")}
	if got := resolveSSOUser(context.Background(), res, "oidc", "sub-1", "worker@example.com"); got != nil {
		t.Errorf("lookup error must yield nil, got %v", got)
	}
	// Defensive: a resolver that returns (nil, nil) must also be handled without
	// a panic, regardless of the repository's not-found contract.
	if got := resolveSSOUser(context.Background(), &errResolver{}, "oidc", "sub-1", "worker@example.com"); got != nil {
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
