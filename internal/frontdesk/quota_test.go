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
	require.JSONEq(t, `{"refreshed":0,"failed":0,"skipped":0}`, rr.Body.String())
}
