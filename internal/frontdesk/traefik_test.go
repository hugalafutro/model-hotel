package frontdesk

import (
	"encoding/json"
	"strings"
	"testing"
)

func defaultSettings() Settings {
	return Settings{
		HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 30,
		EventRetentionDays: 90, RetryAttempts: 2,
	}
}

func TestBuildTraefikConfigActiveOnly(t *testing.T) {
	members := []*Member{
		{Name: "a", URL: "http://a:8081", State: StateActive},
		{Name: "b", URL: "http://b:8081", State: StateDrained},
		{Name: "c", URL: "http://c:8081", State: StateActive},
	}
	cfg := BuildTraefikConfig(members, defaultSettings())

	lb := cfg.HTTP.Services[traefikServiceName].LoadBalancer
	if len(lb.Servers) != 2 {
		t.Fatalf("expected 2 active servers, got %d", len(lb.Servers))
	}
	for _, s := range lb.Servers {
		if s.URL == "http://b:8081" {
			t.Error("drained member must not appear in servers")
		}
	}
}

// The data plane must route the OpenAI-compatible proxy only. A catch-all
// `PathPrefix(/)` would load-balance the dashboard/SPA/admin paths too, so
// hitting the LB host in a browser would drop you on a random member's login.
// The router rule must stay scoped to /v1 (404 for everything else).
func TestBuildTraefikConfigRoutesV1Only(t *testing.T) {
	cfg := BuildTraefikConfig(
		[]*Member{{Name: "a", URL: "http://a:8081", State: StateActive}},
		defaultSettings(),
	)
	rule := cfg.HTTP.Routers[traefikRouterName].Rule
	if rule != "PathPrefix(`/v1`)" {
		t.Errorf("router rule must scope the LB to /v1, got %q", rule)
	}
	if strings.Contains(rule, "PathPrefix(`/`)") {
		t.Error("router rule must not be the catch-all that exposes member dashboards via the LB")
	}
}

// Members are often fronted by a Host-routing reverse proxy, so the LB must
// address each backend by its own host rather than forward the client's Host
// (the LB's public name), which would loop back into the LB. Traefik defaults
// passHostHeader to true, so the generated config must emit an explicit false.
func TestBuildTraefikConfigDoesNotPassHostHeader(t *testing.T) {
	cfg := BuildTraefikConfig(
		[]*Member{{Name: "a", URL: "https://member1.example.com", State: StateActive}},
		defaultSettings(),
	)
	if cfg.HTTP.Services[traefikServiceName].LoadBalancer.PassHostHeader {
		t.Error("passHostHeader must be false so members are addressed by their own host")
	}
	// It must also be present in the serialized config: Traefik treats a missing
	// passHostHeader as true, so omitting it would silently reintroduce the loop.
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"passHostHeader":false`) {
		t.Errorf("serialized config missing explicit passHostHeader:false: %s", out)
	}
}

func TestBuildTraefikConfigHealthCheck(t *testing.T) {
	cfg := BuildTraefikConfig(nil, defaultSettings())
	lb := cfg.HTTP.Services[traefikServiceName].LoadBalancer

	if lb.HealthCheck == nil || lb.HealthCheck.Path != "/health" {
		t.Fatalf("health check missing: %+v", lb.HealthCheck)
	}
	if lb.HealthCheck.Interval != "5s" || lb.HealthCheck.Timeout != "2s" {
		t.Errorf("health check timing: interval=%q timeout=%q", lb.HealthCheck.Interval, lb.HealthCheck.Timeout)
	}
}

// Sticky sessions were removed: the LB serves only /v1, which never carries the
// dashboard cookie, so the generated config must not emit a sticky stanza.
func TestBuildTraefikConfigNoSticky(t *testing.T) {
	out, err := json.Marshal(BuildTraefikConfig(nil, defaultSettings()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "sticky") {
		t.Errorf("serialized config still mentions sticky: %s", out)
	}
}

func TestBuildTraefikConfigRetry(t *testing.T) {
	// Retry present with attempts >= 1, router references it.
	cfg := BuildTraefikConfig(nil, defaultSettings())
	mw, ok := cfg.HTTP.Middlewares[traefikRetryMiddleware]
	if !ok || mw.Retry == nil || mw.Retry.Attempts != 2 {
		t.Fatalf("retry middleware: ok=%v mw=%+v", ok, mw)
	}
	router := cfg.HTTP.Routers[traefikRouterName]
	if len(router.Middlewares) != 1 || router.Middlewares[0] != traefikRetryMiddleware {
		t.Errorf("router does not reference retry: %+v", router.Middlewares)
	}

	// Zero attempts: no middleware, router has none.
	set := defaultSettings()
	set.RetryAttempts = 0
	cfg = BuildTraefikConfig(nil, set)
	if len(cfg.HTTP.Middlewares) != 0 {
		t.Errorf("retry should be omitted at 0 attempts: %+v", cfg.HTTP.Middlewares)
	}
	if len(cfg.HTTP.Routers[traefikRouterName].Middlewares) != 0 {
		t.Error("router should reference no middleware at 0 attempts")
	}
}

func TestBuildTraefikConfigHealthCheckTimeoutClamp(t *testing.T) {
	set := defaultSettings()
	set.HealthPollSecs = 1 // timeout (2s default) must be clamped below interval
	cfg := BuildTraefikConfig(nil, set)
	hc := cfg.HTTP.Services[traefikServiceName].LoadBalancer.HealthCheck
	if hc.Interval != "1s" || hc.Timeout != "500ms" {
		t.Errorf("clamp: interval=%q timeout=%q, want 1s/500ms", hc.Interval, hc.Timeout)
	}
}

func TestBuildTraefikConfigJSONShape(t *testing.T) {
	cfg := BuildTraefikConfig([]*Member{{Name: "a", URL: "http://a:8081", State: StateActive}}, defaultSettings())
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Round-trip into a generic map and spot-check the Traefik structure.
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	http, ok := m["http"].(map[string]any)
	if !ok {
		t.Fatal("missing http key")
	}
	if _, ok := http["routers"]; !ok {
		t.Error("missing routers")
	}
	if _, ok := http["services"]; !ok {
		t.Error("missing services")
	}
}
