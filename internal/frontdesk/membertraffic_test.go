package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// A member whose stats timeseries is slow to assemble (an hour of buckets under
// load) must still be reported reachable: traffic reads use the longer-deadline
// read client, not the fast health probe. The probe is deliberately set shorter
// than the member's delay; a reachable result proves the read did not route
// through it.
func TestMemberTrafficUsesLongerDeadlineThanProbe(t *testing.T) {
	srv, store := newTestServer(t)
	srv.probe = newProbeClient(50 * time.Millisecond)
	srv.readClient = newProbeClient(3 * time.Second)

	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stats/timeseries" {
			time.Sleep(200 * time.Millisecond) // longer than the probe, within the read client
			_, _ = w.Write([]byte(`{"points":[{"bucket":"b1","count":3,"errors":1}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	m, err := store.CreateMember(t.Context(), "slow-hotel", member.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic = %d", rec.Code)
	}
	var resp memberTrafficResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Reachable {
		t.Fatal("slow timeseries must be reachable (read must use the long-deadline client)")
	}
	if resp.TotalRequests != 3 {
		t.Errorf("totals = %d req, want 3", resp.TotalRequests)
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

// A ?window=<minutes> filter trims the member's ~24h series to the requested
// tail: buckets older than the window are dropped and totals count only the
// kept ones, so window_minutes matches what's charted.
func TestMemberTrafficWindowTrimsToRequestedSpan(t *testing.T) {
	srv, store := newTestServer(t)
	now := time.Now().UTC()
	inWindow := now.Add(-10 * time.Minute).Format(time.RFC3339)
	outWindow := now.Add(-90 * time.Minute).Format(time.RFC3339)
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stats/timeseries" {
			_, _ = w.Write([]byte(`{"points":[` +
				`{"bucket":"` + outWindow + `","count":7,"errors":3},` +
				`{"bucket":"` + inWindow + `","count":4,"errors":1}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	m, err := store.CreateMember(t.Context(), "hotel", member.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic?window=60", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic = %d", rec.Code)
	}
	var resp memberTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.WindowMinutes != 60 {
		t.Errorf("window_minutes = %d, want 60", resp.WindowMinutes)
	}
	if len(resp.Points) != 1 {
		t.Fatalf("points = %d, want 1 (out-of-window bucket trimmed)", len(resp.Points))
	}
	if resp.TotalRequests != 4 || resp.TotalErrors != 1 {
		t.Errorf("totals = %d/%d, want 4/1 (only the in-window bucket counted)", resp.TotalRequests, resp.TotalErrors)
	}
}

// No ?window= keeps the legacy full series and window_minutes=60, even for
// buckets whose labels aren't timestamps: Front Desk's own Traffic page, which
// never sends the param, must be unaffected.
func TestMemberTrafficNoWindowKeepsFullSeries(t *testing.T) {
	srv, store := newTestServer(t)
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stats/timeseries" {
			_, _ = w.Write([]byte(`{"points":[{"bucket":"b1","count":10,"errors":2},{"bucket":"b2","count":5,"errors":0}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	m, err := store.CreateMember(t.Context(), "hotel", member.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/members/"+m.ID+"/traffic", "", true)
	var resp memberTrafficResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Points) != 2 || resp.WindowMinutes != 60 || resp.TotalRequests != 15 {
		t.Errorf("no-window = %d points / window %d / %d req, want 2/60/15",
			len(resp.Points), resp.WindowMinutes, resp.TotalRequests)
	}
}

func TestParseTrafficWindowClampsAndDefaults(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 0},          // absent -> legacy full series
		{"abc", 0},       // non-numeric -> legacy
		{"0", 0},         // non-positive -> legacy
		{"-30", 0},       // negative -> legacy
		{"1", 5},         // below the floor -> clamped up
		{"60", 60},       // in range -> kept
		{"1440", 1440},   // ceiling -> kept
		{"100000", 1440}, // above ceiling -> clamped down
	}
	for _, c := range cases {
		target := "/api/members/x/traffic"
		if c.query != "" {
			target += "?window=" + c.query
		}
		r := httptest.NewRequest(http.MethodGet, target, http.NoBody)
		if got := parseTrafficWindow(r); got != c.want {
			t.Errorf("window=%q -> %d, want %d", c.query, got, c.want)
		}
	}
}
