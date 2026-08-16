package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeNotConfigured(t *testing.T) {
	d := New(fakeCfg{cfg: Config{}}, nil) // no APIBaseURL
	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if st.Configured {
		t.Errorf("expected not configured, got %+v", st)
	}
}

func TestProbeHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	d := New(fakeCfg{cfg: Config{APIBaseURL: srv.URL}}, srv.Client())
	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !st.Configured || !st.Reachable || !st.Healthy {
		t.Errorf("expected healthy, got %+v", st)
	}
}

func TestProbeReachableButUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusExpectationFailed) // 417 — apprise-api "up but has issues"
	}))
	defer srv.Close()

	d := New(fakeCfg{cfg: Config{APIBaseURL: srv.URL}}, srv.Client())
	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !st.Reachable || st.Healthy {
		t.Errorf("expected reachable but unhealthy, got %+v", st)
	}
}

func TestProbeUnreachable(t *testing.T) {
	// Port 1 is not listening → connection refused.
	d := New(fakeCfg{cfg: Config{APIBaseURL: "http://127.0.0.1:1"}}, &http.Client{})
	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !st.Configured || st.Reachable {
		t.Errorf("expected configured+unreachable, got %+v", st)
	}
}

func TestProbeConfigError(t *testing.T) {
	d := New(fakeCfg{err: context.Canceled}, nil)
	if _, err := d.Probe(context.Background()); err == nil {
		t.Error("expected config-load error to propagate")
	}
}

func TestProbeURLReasons(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("OK")) }))
	defer healthy.Close()
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusExpectationFailed) }))
	defer sick.Close()

	d := New(fakeCfg{cfg: Config{}}, healthy.Client())
	cases := []struct {
		name, base, reason string
		healthyWant        bool
	}{
		{"blank", "   ", ReasonNotConfigured, false},
		{"invalid", "://nope", ReasonInvalidURL, false},
		{"no scheme", "apprise:8000", ReasonInvalidURL, false},
		{"non-http scheme", "ftp://h", ReasonInvalidURL, false},
		{"unreachable", "http://127.0.0.1:1", ReasonUnreachable, false},
		{"unhealthy", sick.URL, ReasonUnhealthy, false},
		{"healthy", healthy.URL, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := d.ProbeURL(context.Background(), tc.base)
			if st.Reason != tc.reason || st.Healthy != tc.healthyWant {
				t.Errorf("ProbeURL(%q) = %+v, want reason %q healthy %v", tc.base, st, tc.reason, tc.healthyWant)
			}
		})
	}
}

// Probe stays a thin wrapper: the config's URL goes through ProbeURL unchanged.
func TestProbeDelegatesToProbeURL(t *testing.T) {
	d := New(fakeCfg{cfg: Config{APIBaseURL: "http://127.0.0.1:1"}}, &http.Client{})
	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Configured || st.Reason != ReasonUnreachable {
		t.Errorf("got %+v, want configured+unreachable", st)
	}
}

// TestProbeIgnoresCorruptTarget guards against regressing to AlertConfig (which
// decrypts the target): a target that can't be decrypted must not fail a
// reachability probe when the URL is valid and reachable.
func TestProbeIgnoresCorruptTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	p := NewSettingsConfigProvider(fakeSettings{vals: map[string]string{
		"alert_apprise_api_url": srv.URL,
		"alert_apprise_targets": "enc:v1:!!!:@@@:###", // undecryptable
	}}, "any-master-key-at-least-32-bytes-long!!!")
	d := New(p, srv.Client())

	st, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe must not error on a corrupt target: %v", err)
	}
	if !st.Configured || !st.Reachable || !st.Healthy {
		t.Errorf("expected reachable despite corrupt target, got %+v", st)
	}
}
