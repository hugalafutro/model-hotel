package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestServerWithPrimary builds a Server whose Store has one member
// designated as the fleet primary, pointed at memberURL with a fixed token,
// mirroring the primary+token fixture pattern used by DistributeQuotaOnce's
// tests (quota_distribute_test.go).
func newTestServerWithPrimary(t *testing.T, memberURL string) *Server {
	t.Helper()
	srv, store := newTestServer(t)
	pm, err := store.CreateMember(t.Context(), "primary", memberURL, "ptoken")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	return srv
}

// newTestServerNoPrimary builds a Server whose Store has no designated
// primary (standalone / not set up).
func newTestServerNoPrimary(t *testing.T) *Server {
	t.Helper()
	srv, _ := newTestServer(t)
	return srv
}

func TestHandleQuota_ProxiesPrimaryExport(t *testing.T) {
	// A member httptest server that answers the export path with one snapshot.
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/config/quota-snapshots", r.URL.Path)
		writeJSON(w, http.StatusOK, map[string]any{"snapshots": []map[string]any{
			{"provider_name": "OR", "type": "openrouter", "kind": "usage",
				"payload": json.RawMessage(`{"usage":1.5}`), "http_status": 200},
		}})
	}))
	defer member.Close()

	s := newTestServerWithPrimary(t, member.URL) // store: one member = primary, token set
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))

	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Quota []map[string]any `json:"quota"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Len(t, body.Quota, 1)
	require.Equal(t, "openrouter", body.Quota[0]["type"])
}

func TestHandleQuota_NoPrimaryReturnsEmpty(t *testing.T) {
	s := newTestServerNoPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"quota":[]}`, rr.Body.String())
}

// newTestServerWithMissingPrimary designates a primary ID no member row
// matches, the state left behind when a designated member is deleted.
func newTestServerWithMissingPrimary(t *testing.T) *Server {
	t.Helper()
	srv, store := newTestServer(t)
	if err := store.SetAutoSync(t.Context(), true, "gone"); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	return srv
}

// newTestServerWithFailingPrimary points the primary at a member that answers
// every request with a 500.
func newTestServerWithFailingPrimary(t *testing.T) *Server {
	t.Helper()
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(member.Close)
	return newTestServerWithPrimary(t, member.URL)
}

// newTestServerWithTokenlessPrimary designates a member Front Desk holds no
// admin token for, so memberTokenOrErr fails with ErrValidation: the primary
// exists, we just cannot authenticate to it.
func newTestServerWithTokenlessPrimary(t *testing.T) *Server {
	t.Helper()
	srv, store := newTestServer(t)
	pm, err := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	return srv
}

// newTestServerWithBrokenStore designates a primary and then closes the store
// underneath the Server, so the primary lookup fails the way an unreadable
// database file would.
func newTestServerWithBrokenStore(t *testing.T) *Server {
	t.Helper()
	srv, store := newTestServer(t)
	if err := store.SetAutoSync(t.Context(), true, "primary"); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return srv
}

func TestHandleQuota_MissingPrimaryMemberReturnsEmpty(t *testing.T) {
	// A designated member that was deleted is a steady state, not a transient
	// failure: there is nobody left to ask, so an empty set is truthful.
	s := newTestServerWithMissingPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"quota":[]}`, rr.Body.String())
}

func TestHandleQuota_TokenlessPrimaryReturnsBadGateway(t *testing.T) {
	// The primary exists but Front Desk stores no admin token for it, so its
	// quota is unknown rather than absent.
	s := newTestServerWithTokenlessPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), `"error"`)
}

func TestHandleQuota_StoreFailureReturnsError(t *testing.T) {
	// Front Desk's own store is unreadable: our failure, still not an answer.
	s := newTestServerWithBrokenStore(t)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestHandleQuota_UnhappyPrimaryReturnsBadGateway(t *testing.T) {
	// A primary that answers 500 leaves its quota unknown. Answering an empty
	// 200 here would read on the device as "this fleet has no quota providers"
	// and wipe the last-good badges, so the read has to fail loudly.
	s := newTestServerWithFailingPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), `"error"`)
}

func TestHandleQuota_UndecodableExportReturnsBadGateway(t *testing.T) {
	// A 200 whose body isn't the export shape (a proxy's error page, say) tells
	// us nothing about the primary's quota, so it fails like an unreachable
	// primary instead of reading as "no snapshots".
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer member.Close()

	s := newTestServerWithPrimary(t, member.URL)
	rr := httptest.NewRecorder()
	s.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), `"error"`)
}

func TestHandleQuotaRefresh_ProxiesPrimary(t *testing.T) {
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/providers/refresh-quotas", r.URL.Path)
		writeJSON(w, http.StatusOK, map[string]any{"refreshed": 2, "failed": 0, "skipped": 1})
	}))
	defer member.Close()
	s := newTestServerWithPrimary(t, member.URL)

	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"refreshed":2`)
}

func TestHandleQuotaRefresh_NoPrimaryReturnsNoOp(t *testing.T) {
	s := newTestServerNoPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"results":[],"refreshed":0,"failed":0,"skipped":0}`, rr.Body.String())
}

func TestHandleQuotaRefresh_MissingPrimaryMemberReturnsNoOp(t *testing.T) {
	// Deleted designated member: nobody to refresh, so the no-op is honest.
	s := newTestServerWithMissingPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"results":[],"refreshed":0,"failed":0,"skipped":0}`, rr.Body.String())
}

func TestHandleQuotaRefresh_TokenlessPrimaryReturnsBadGateway(t *testing.T) {
	s := newTestServerWithTokenlessPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), `"error"`)
}

func TestHandleQuotaRefresh_StoreFailureReturnsError(t *testing.T) {
	s := newTestServerWithBrokenStore(t)
	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestHandleQuotaRefresh_UnhappyPrimaryReturnsBadGateway(t *testing.T) {
	// A primary that can't re-poll must not answer like a refresh that ran and
	// found nothing: the device has to be able to tell the operator it failed.
	s := newTestServerWithFailingPrimary(t)
	rr := httptest.NewRecorder()
	s.handleQuotaRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), `"error"`)
}
