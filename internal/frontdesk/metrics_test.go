package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newMetricsTestServer builds a test server with a dedicated scrape token so
// the FRONTDESK_METRICS_TOKEN auth path can be exercised (newTestServer leaves
// it empty, which is the admin-fallback path).
//
// The member-metrics source is a package-global bound at NewServer time (last
// constructed server wins), so tests that scrape /metrics must not run in
// parallel with other server constructions. The cleanup detaches this server's
// source so a later test cannot scrape a torn-down store by accident.
func newMetricsTestServer(t *testing.T, metricsToken string) (*Server, *Store) {
	t.Helper()
	t.Cleanup(func() { setMemberMetricsSource(func() []memberMetricState { return nil }) })
	store := newTestStore(t)
	bus := events.NewBus()
	poller := NewPoller(store, bus, "")

	adminMgr, _, err := admin.New(t.TempDir(), testFrontdeskToken)
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	rp, err := webauthn.NewRelyingParty("localhost", "Front Desk", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	srv := NewServer(ServerConfig{
		Store:        store,
		Poller:       poller,
		Bus:          bus,
		AdminMgr:     adminMgr,
		MasterKey:    testMasterKey,
		RelyingParty: rp,
		IPLimiter:    ratelimit.NewIPLimiter(1000, 1000, nil, nil),
		MetricsToken: metricsToken,
	})
	t.Cleanup(srv.Wait)
	return srv, store
}

// scrape issues GET /metrics with an arbitrary bearer (empty = none).
func scrape(t *testing.T, srv *Server, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestMetricsAdminFallbackAuth covers the no-scrape-token configuration: the
// endpoint is gated by the admin-or-session auth, never open.
func TestMetricsAdminFallbackAuth(t *testing.T) {
	srv, _ := newTestServer(t)

	if rec := scrape(t, srv, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /metrics = %d, want 401", rec.Code)
	}
	rec := scrape(t, srv, testFrontdeskToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET /metrics = %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "frontdesk_members_total") {
		t.Errorf("scrape body missing frontdesk_members_total")
	}
}

// TestMetricsTokenAuth covers the dedicated-token configuration: only the
// scrape token is accepted, including over the admin token (so the scrape
// config never needs, and cannot substitute, admin credentials).
func TestMetricsTokenAuth(t *testing.T) {
	srv, _ := newMetricsTestServer(t, "scrape-secret")

	if rec := scrape(t, srv, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer = %d, want 401", rec.Code)
	}
	if rec := scrape(t, srv, "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer = %d, want 401", rec.Code)
	}
	if rec := scrape(t, srv, testFrontdeskToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin bearer with metrics token set = %d, want 401", rec.Code)
	}
	if rec := scrape(t, srv, "scrape-secret"); rec.Code != http.StatusOK {
		t.Fatalf("scrape token = %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestMetricsWhitespaceTokenFallsBackToAdmin guards against a blank-looking
// FRONTDESK_METRICS_TOKEN (only spaces) being stored as a live bearer: the value
// is normalized to unset, so the endpoint keeps the admin-or-session gate and the
// whitespace string is not itself a valid credential.
func TestMetricsWhitespaceTokenFallsBackToAdmin(t *testing.T) {
	srv, _ := newMetricsTestServer(t, "   ")

	if rec := scrape(t, srv, "   "); rec.Code != http.StatusUnauthorized {
		t.Fatalf("whitespace bearer = %d, want 401", rec.Code)
	}
	if rec := scrape(t, srv, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer = %d, want 401", rec.Code)
	}
	if rec := scrape(t, srv, testFrontdeskToken); rec.Code != http.StatusOK {
		t.Fatalf("admin bearer with whitespace token = %d (%s), want 200", rec.Code, rec.Body.String())
	}
}

// TestMetricsScrapeMemberSeries seeds a member with live poller health and a
// persisted last-sync stamp and asserts the scrape-time collector reports the
// fleet gauges from current state.
func TestMetricsScrapeMemberSeries(t *testing.T) {
	srv, store := newMetricsTestServer(t, "scrape-secret")
	ctx := context.Background()

	m, err := store.CreateMember(ctx, "alpha", "http://127.0.0.1:9", "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	syncAt := time.Unix(1700000000, 0).UTC()
	if err := store.SetMemberLastSync(ctx, m.ID, syncAt, "test"); err != nil {
		t.Fatalf("SetMemberLastSync: %v", err)
	}
	srv.poller.mu.Lock()
	srv.poller.statuses[m.ID] = MemberStatus{
		Health: HealthStatus{Known: true, Healthy: true, LatencyMs: 42},
	}
	srv.poller.mu.Unlock()

	body := scrape(t, srv, "scrape-secret").Body.String()
	for _, want := range []string{
		`frontdesk_members_total{state="active"} 1`,
		`frontdesk_members_total{state="drained"} 0`,
		`frontdesk_member_up{member="alpha"} 1`,
		`frontdesk_member_health_latency_seconds{member="alpha"} 0.042`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape body missing %q", want)
		}
	}
	// The timestamp is parsed rather than string-matched: client_golang renders
	// large floats in scientific notation and the exact formatting is not part
	// of its API contract.
	if got := seriesValue(t, body, `frontdesk_last_config_sync_timestamp_seconds{member="alpha"}`); got != float64(syncAt.Unix()) {
		t.Errorf("last_config_sync = %v, want %v", got, syncAt.Unix())
	}
}

// seriesValue extracts and parses one series' value from a text-format scrape.
func seriesValue(t *testing.T, body, series string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if rest, found := strings.CutPrefix(line, series+" "); found {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				t.Fatalf("parse %q value %q: %v", series, rest, err)
			}
			return v
		}
	}
	t.Fatalf("scrape body missing series %q", series)
	return 0
}

// TestMetricsScrapeSkipsUnknownHealth confirms a never-probed member gets no
// up/latency series (rather than a fabricated "down") while still counting in
// the fleet total.
func TestMetricsScrapeSkipsUnknownHealth(t *testing.T) {
	srv, store := newMetricsTestServer(t, "scrape-secret")
	if _, err := store.CreateMember(context.Background(), "beta", "http://127.0.0.1:9", "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	body := scrape(t, srv, "scrape-secret").Body.String()
	if !strings.Contains(body, `frontdesk_members_total{state="active"} 1`) {
		t.Errorf("scrape body missing members_total for unprobed member")
	}
	if strings.Contains(body, `frontdesk_member_up{member="beta"}`) {
		t.Errorf("unprobed member must not report frontdesk_member_up")
	}
}

// TestMetricsEventSeams exercises the event-updated instruments through their
// package seams and asserts the series appear on a scrape. The registry is
// package-global, so assertions are presence-based, not exact counts.
func TestMetricsEventSeams(t *testing.T) {
	srv, _ := newMetricsTestServer(t, "scrape-secret")

	observePollDuration("health", 12*time.Millisecond)
	observePollDuration("traefik", 3*time.Millisecond)
	recordConfigSync("ok")
	recordConfigSync("err")
	recordConfigSync("superseded")
	recordAlertDispatch(true)
	recordAlertDispatch(false)

	body := scrape(t, srv, "scrape-secret").Body.String()
	for _, want := range []string{
		`frontdesk_poll_duration_seconds_count{kind="health"}`,
		`frontdesk_poll_duration_seconds_count{kind="traefik"}`,
		`frontdesk_config_sync_total{result="ok"}`,
		`frontdesk_config_sync_total{result="err"}`,
		`frontdesk_config_sync_total{result="superseded"}`,
		`frontdesk_alerts_dispatched_total{result="ok"}`,
		`frontdesk_alerts_dispatched_total{result="err"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape body missing %q", want)
		}
	}
}
