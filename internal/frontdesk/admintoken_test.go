package frontdesk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	rec := do(t, srv, http.MethodPost, "/api/admin-token/reset", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset = %d (%s)", rec.Code, rec.Body.String())
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
