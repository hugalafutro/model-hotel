package frontdesk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// stubMember is a fake Model Hotel member exposing /api/admin/token-hash with
// the same auth contract the real endpoint has (Bearer must equal the member's
// current admin token). It records the last hash pushed to it.
type stubMember struct {
	mu       sync.Mutex
	token    string // current admin token the member accepts
	hash     string // current sha256:<hex>
	srv      *httptest.Server
	gotPush  string // last hash POSTed
	pushAuth string // Authorization seen on the last POST
}

func newStubMember(t *testing.T, token, hash string) *stubMember {
	t.Helper()
	sm := &stubMember{token: token, hash: hash}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/token-hash" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{"hash": sm.hash})
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			sm.gotPush = req["hash"]
			sm.pushAuth = r.Header.Get("Authorization")
			sm.hash = req["hash"]
			_ = json.NewEncoder(w).Encode(map[string]string{"hash": sm.hash})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func TestAdminTokenPreviewClassifies(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubMember(t, "ptoken", "sha256:aaa")
	secondary := newStubMember(t, "stoken", "sha256:bbb")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	sm, _ := store.CreateMember(t.Context(), "secondary", secondary.srv.URL, "stoken")
	nm, _ := store.CreateMember(t.Context(), "no-token", "http://127.0.0.1:1", "")

	rec := do(t, srv, http.MethodGet, "/api/admin-token/preview?primary="+pm.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		PrimaryID string            `json:"primary_id"`
		Items     []syncPreviewItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	disp := map[string]string{}
	for _, it := range resp.Items {
		disp[it.MemberID] = it.Disposition
	}
	if disp[pm.ID] != dispMatches {
		t.Errorf("primary disposition = %q, want matches", disp[pm.ID])
	}
	if disp[sm.ID] != dispOverwrite {
		t.Errorf("secondary disposition = %q, want overwrite", disp[sm.ID])
	}
	if disp[nm.ID] != dispBlocked {
		t.Errorf("no-token disposition = %q, want blocked", disp[nm.ID])
	}
}

// A member whose current hash can't be read is classified "overwrite" (unknown,
// so the sync will attempt it) rather than silently "matches".
func TestAdminTokenPreviewFetchFailureIsOverwrite(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubMember(t, "ptoken", "sha256:aaa")
	// A member whose token-hash GET errors (wrong token here => 401).
	broken := newStubMember(t, "real-token", "sha256:bbb")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	bm, _ := store.CreateMember(t.Context(), "broken", broken.srv.URL, "stale-token")

	rec := do(t, srv, http.MethodGet, "/api/admin-token/preview?primary="+pm.ID, "", true)
	var resp struct {
		Items []syncPreviewItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, it := range resp.Items {
		if it.MemberID == bm.ID && it.Disposition != dispOverwrite {
			t.Errorf("unreadable member disposition = %q, want overwrite", it.Disposition)
		}
	}
}

// A member already on the primary's hash is left untouched (not in the results).
func TestAdminTokenSyncSkipsAlreadyMatching(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubMember(t, "ptoken", "sha256:same")
	matching := newStubMember(t, "mtoken", "sha256:same")
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	mm, _ := store.CreateMember(t.Context(), "matching", matching.srv.URL, "mtoken")

	rec := do(t, srv, http.MethodPost, "/api/admin-token/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, r := range resp.Results {
		if r.MemberID == mm.ID {
			t.Errorf("already-matching member should be skipped, got result %+v", r)
		}
	}
	if matching.gotPush != "" {
		t.Errorf("already-matching member should not be pushed to, got %q", matching.gotPush)
	}
}

// When the member rejects the hash push, the result is a failure and Front Desk
// leaves its stored token unchanged.
func TestApplyTokenHashPushFailure(t *testing.T) {
	srv, store := newTestServer(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer stub.Close()
	mem, _ := store.CreateMember(t.Context(), "m", stub.URL, "tok")

	res := srv.applyTokenHash(t.Context(), mem, "tok", "sha256:new", "newplain", "admin_token.synced")
	if res.OK {
		t.Error("expected OK=false when the member rejects the push")
	}
	if !strings.Contains(res.Error, "could not update") {
		t.Errorf("error = %q, want it to mention the failed update", res.Error)
	}
	// The stored token must be unchanged (push failed before any store write).
	tok, ok, _ := store.MemberToken(t.Context(), mem.ID)
	if !ok || tok != "tok" {
		t.Errorf("stored token = %q (ok=%v), want it unchanged", tok, ok)
	}
}

func TestAdminTokenSyncOverwritesAndRestoresStoredToken(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubMember(t, "ptoken", "sha256:aaa")
	secondary := newStubMember(t, "stoken", "sha256:bbb")
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	sm, _ := store.CreateMember(t.Context(), "secondary", secondary.srv.URL, "stoken")

	rec := do(t, srv, http.MethodPost, "/api/admin-token/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || !resp.Results[0].OK || resp.Results[0].MemberID != sm.ID {
		t.Fatalf("unexpected results: %+v", resp.Results)
	}
	// The secondary received the primary's hash, authenticated with its OLD token.
	if secondary.gotPush != "sha256:aaa" {
		t.Errorf("secondary got hash %q, want sha256:aaa", secondary.gotPush)
	}
	if secondary.pushAuth != "Bearer stoken" {
		t.Errorf("push auth = %q, want Bearer stoken", secondary.pushAuth)
	}
	// Front Desk now stores the primary's token for the secondary.
	tok, ok, _ := store.MemberToken(t.Context(), sm.ID)
	if !ok || tok != "ptoken" {
		t.Errorf("stored secondary token = %q (ok=%v), want ptoken", tok, ok)
	}
}

func TestAdminTokenResetGeneratesAndPushes(t *testing.T) {
	srv, store := newTestServer(t)
	m1 := newStubMember(t, "t1", "sha256:111")
	m2 := newStubMember(t, "t2", "sha256:222")
	id1, _ := store.CreateMember(t.Context(), "m1", m1.srv.URL, "t1")
	_, _ = store.CreateMember(t.Context(), "m2", m2.srv.URL, "t2")
	// A token-less member should be reported skipped, not crash the reset.
	nm, _ := store.CreateMember(t.Context(), "m3", "http://127.0.0.1:1", "")

	rec := do(t, srv, http.MethodPost, "/api/admin-token/reset", `{"confirm":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset = %d (%s)", rec.Code, rec.Body.String())
	}
	// The plaintext-bearing response must not be cacheable.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var resp struct {
		Token   string           `json:"token"`
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Token) != 32 {
		t.Errorf("token length = %d, want 32", len(resp.Token))
	}
	// Both reachable members got the SAME new hash; it is the hash of the token.
	if m1.gotPush == "" || m1.gotPush != m2.gotPush {
		t.Errorf("members got differing/empty hashes: %q vs %q", m1.gotPush, m2.gotPush)
	}
	// The stored token for a synced member is now the new plaintext.
	tok, ok, _ := store.MemberToken(t.Context(), id1.ID)
	if !ok || tok != resp.Token {
		t.Errorf("stored m1 token = %q, want the new token", tok)
	}
	// The token-less member is reported as skipped.
	for _, r := range resp.Results {
		if r.MemberID == nm.ID && r.OK {
			t.Error("token-less member should be reported ok=false")
		}
	}
}

func TestAdminTokenResetRequiresConfirm(t *testing.T) {
	srv, _ := newTestServer(t)
	// No body and confirm=false must both be refused without minting a token.
	if rec := do(t, srv, http.MethodPost, "/api/admin-token/reset", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("reset with no body = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/admin-token/reset", `{"confirm":false}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("reset with confirm=false = %d, want 400", rec.Code)
	}
}

// When the member accepts the new hash but Front Desk fails to persist the new
// token (here: the member row is gone, so SetMemberToken returns ErrNotFound),
// the result must report failure with a remedy, not a silent success.
func TestApplyTokenHashSurfacesStoreFailure(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubMember(t, "tok", "sha256:old")
	mem, err := store.CreateMember(t.Context(), "m", stub.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	token, _, _ := store.MemberToken(t.Context(), mem.ID)
	// Remove the row so the post-push SetMemberToken fails.
	if err := store.DeleteMember(t.Context(), mem.ID); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}

	res := srv.applyTokenHash(t.Context(), mem, token, "sha256:new", "newplain", "admin_token.reset")
	if res.OK {
		t.Error("expected OK=false when the token store write fails")
	}
	if !strings.Contains(res.Error, "could not save") {
		t.Errorf("error = %q, want it to mention the store-write failure", res.Error)
	}
	// The member did receive the new hash (the push happened before the store write).
	if stub.gotPush != "sha256:new" {
		t.Errorf("member got hash %q, want sha256:new", stub.gotPush)
	}
}
