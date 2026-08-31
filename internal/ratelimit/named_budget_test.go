package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// A deployment can run several IP limiters at once (Front Desk runs three, two
// of them with identical rps and burst). Without a name their throttle lines
// are byte-identical apart from the address, and "a prober was refused" is not
// the same event as "the data plane may be losing its config source".
func TestThrottleLogCtx_LogAttrs(t *testing.T) {
	unnamed := throttleLogCtx{label: "ip", id: "203.0.113.1", rps: 2, burst: 5}
	got := unnamed.logAttrs()
	if slices.Contains(got, "budget") {
		t.Errorf("an unnamed limiter should not carry a budget attr: %v", got)
	}
	if len(got) != 6 || got[0] != "ip" || got[1] != "203.0.113.1" {
		t.Errorf("logAttrs() = %v, want the identity then the limits", got)
	}

	named := throttleLogCtx{label: "ip", id: "203.0.113.1", budget: "healthz", rps: 2, burst: 5}
	got = named.logAttrs()
	i := slices.Index(got, "budget")
	if i < 0 || got[i+1] != "healthz" {
		t.Fatalf("logAttrs() = %v, want a budget=healthz pair", got)
	}
	// The identity still leads and the limits still trail, so the line reads
	// the same way with or without a name.
	if got[0] != "ip" || got[1] != "203.0.113.1" || !slices.Contains(got, "rps") || !slices.Contains(got, "burst") {
		t.Errorf("logAttrs() = %v, want identity first and limits last", got)
	}
}

func TestIPLimiter_Named(t *testing.T) {
	lim := NewIPLimiter(1, 1, nil, nil)
	defer lim.Stop()

	if got := lim.Named("healthz"); got != lim {
		t.Error("Named should return the same limiter so it can be used at construction")
	}
	if lim.budget != "healthz" {
		t.Errorf("budget = %q, want healthz", lim.budget)
	}
}

// The name has to reach the per-address entry, which is what actually builds
// the log context, or naming the limiter would have no effect on its output.
func TestIPLimiter_NamePropagatesToTheThrottledEntry(t *testing.T) {
	lim := NewIPLimiter(0.01, 1, nil, nil).Named("traefik-config")
	defer lim.Stop()

	handler := lim.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for range 3 {
		req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
		req.RemoteAddr = "203.0.113.44:5000"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	lim.mu.Lock()
	entry, ok := lim.limiters["203.0.113.44"]
	lim.mu.Unlock()
	if !ok {
		t.Fatal("no entry was created for the flooding address")
	}
	if entry.budget != "traefik-config" {
		t.Errorf("entry budget = %q, want the limiter's name", entry.budget)
	}
	if got := entry.throttleCtx("203.0.113.44").budget; got != "traefik-config" {
		t.Errorf("throttleCtx budget = %q, want the limiter's name", got)
	}
}
