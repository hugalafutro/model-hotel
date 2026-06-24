package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemberTrafficProxiesAndReshapes(t *testing.T) {
	srv, store := newTestServer(t)

	// A stub member exposing the admin time-series API.
	var gotAuth string
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stats/timeseries" {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"points":[{"bucket":"b1","count":10,"errors":2},{"bucket":"b2","count":5,"errors":0}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	m, err := store.CreateMember(t.Context(), "hotel-1", member.URL, "member-token")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic = %d", rec.Code)
	}
	var resp memberTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Reachable {
		t.Fatal("expected reachable=true")
	}
	if resp.TotalRequests != 15 || resp.TotalErrors != 2 {
		t.Errorf("totals = %d req / %d err, want 15/2", resp.TotalRequests, resp.TotalErrors)
	}
	if len(resp.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(resp.Points))
	}
	if gotAuth != "Bearer member-token" {
		t.Errorf("member saw auth %q, want Bearer member-token", gotAuth)
	}
}

func TestMemberTrafficNoTokenIsUnreachable(t *testing.T) {
	srv, store := newTestServer(t)
	m, err := store.CreateMember(t.Context(), "hotel-1", "http://127.0.0.1:1", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic = %d", rec.Code)
	}
	var resp memberTrafficResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Reachable {
		t.Error("expected reachable=false for a member with no stored token")
	}
}

func TestMemberTrafficUnknownMemberIs404(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodGet, "/api/members/nope/traffic", "", true)
	if rec.Code != http.StatusNotFound {
		t.Errorf("traffic = %d, want 404", rec.Code)
	}
}

// A member whose stats API errors or returns junk is reported unreachable (200
// with reachable=false), so the Traffic tab degrades per member.
func TestMemberTrafficUnreadableIsUnreachable(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"non-200": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"bad-json": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv, store := newTestServer(t)
			member := httptest.NewServer(handler)
			defer member.Close()
			m, err := store.CreateMember(t.Context(), "hotel", member.URL, "tok")
			if err != nil {
				t.Fatalf("CreateMember: %v", err)
			}
			rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("traffic = %d", rec.Code)
			}
			var resp memberTrafficResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp.Reachable {
				t.Errorf("%s: expected reachable=false", name)
			}
		})
	}
}
