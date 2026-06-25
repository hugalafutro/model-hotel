package frontdesk

import "time"

// This file turns the member list + settings into a Traefik v3 dynamic
// configuration, served unauthenticated on the compose-internal network at
// GET /traefik/config and polled by Traefik's HTTP provider. Traefik owns the
// data path; if Front Desk dies, Traefik keeps the last config it fetched, so
// the control plane being down never interrupts traffic.

// Fixed names for the generated config. They are internal to the HA stack and
// never user-facing, so they are not translated.
const (
	traefikRouterName      = "hotel"
	traefikServiceName     = "hotel"
	traefikRetryMiddleware = "hotel-retry"
	traefikEntryPoint      = "web"
	traefikStickyCookie    = "mh_lb"
	traefikHealthPath      = "/health"
)

// TraefikConfig is the root of the Traefik HTTP-provider dynamic config.
type TraefikConfig struct {
	HTTP TraefikHTTP `json:"http"`
}

// TraefikHTTP holds the routers, services, and middlewares.
type TraefikHTTP struct {
	Routers     map[string]TraefikRouter     `json:"routers"`
	Services    map[string]TraefikService    `json:"services"`
	Middlewares map[string]TraefikMiddleware `json:"middlewares,omitempty"`
}

// TraefikRouter is the single catch-all router.
type TraefikRouter struct {
	Rule        string   `json:"rule"`
	Service     string   `json:"service"`
	EntryPoints []string `json:"entryPoints,omitempty"`
	Middlewares []string `json:"middlewares,omitempty"`
}

// TraefikService wraps a load balancer.
type TraefikService struct {
	LoadBalancer TraefikLoadBalancer `json:"loadBalancer"`
}

// TraefikLoadBalancer is the backend pool with optional health check + sticky.
type TraefikLoadBalancer struct {
	Servers     []TraefikServer     `json:"servers"`
	HealthCheck *TraefikHealthCheck `json:"healthCheck,omitempty"`
	Sticky      *TraefikSticky      `json:"sticky,omitempty"`
}

// TraefikServer is one backend URL.
type TraefikServer struct {
	URL string `json:"url"`
}

// TraefikHealthCheck configures active health checks against /health.
type TraefikHealthCheck struct {
	Path     string `json:"path"`
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
}

// TraefikSticky pins a browser session (and its SSE) to one backend via cookie.
type TraefikSticky struct {
	Cookie TraefikCookie `json:"cookie"`
}

// TraefikCookie names the sticky cookie.
type TraefikCookie struct {
	Name string `json:"name"`
}

// TraefikMiddleware is a single middleware entry; only retry is used.
type TraefikMiddleware struct {
	Retry *TraefikRetry `json:"retry,omitempty"`
}

// TraefikRetry retries requests that failed before any response byte was sent.
type TraefikRetry struct {
	Attempts int `json:"attempts"`
}

// BuildTraefikConfig renders the dynamic config from the current members and
// settings. Only active members enter the backend pool: a drained member is
// removed from servers so its established streams finish while new requests go
// elsewhere. Retry is included only when attempts >= 1; sticky only when the
// setting is on.
func BuildTraefikConfig(members []*Member, set Settings) TraefikConfig {
	servers := make([]TraefikServer, 0, len(members))
	for _, m := range members {
		if m.State == StateActive {
			servers = append(servers, TraefikServer{URL: m.URL})
		}
	}

	lb := TraefikLoadBalancer{
		Servers:     servers,
		HealthCheck: buildHealthCheck(set),
	}
	if set.StickyEnabled {
		lb.Sticky = &TraefikSticky{Cookie: TraefikCookie{Name: traefikStickyCookie}}
	}

	router := TraefikRouter{
		Rule:        "PathPrefix(`/`)",
		Service:     traefikServiceName,
		EntryPoints: []string{traefikEntryPoint},
	}

	cfg := TraefikConfig{
		HTTP: TraefikHTTP{
			Routers:  map[string]TraefikRouter{traefikRouterName: router},
			Services: map[string]TraefikService{traefikServiceName: {LoadBalancer: lb}},
		},
	}

	if set.RetryAttempts >= 1 {
		router.Middlewares = []string{traefikRetryMiddleware}
		cfg.HTTP.Routers[traefikRouterName] = router
		cfg.HTTP.Middlewares = map[string]TraefikMiddleware{
			traefikRetryMiddleware: {Retry: &TraefikRetry{Attempts: set.RetryAttempts}},
		}
	}

	return cfg
}

// buildHealthCheck derives Traefik's active health check from the health poll
// interval, keeping timeout strictly below interval (Traefik requires this).
func buildHealthCheck(set Settings) *TraefikHealthCheck {
	interval := time.Duration(set.HealthPollSecs) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := 2 * time.Second
	if timeout >= interval {
		timeout = interval / 2
	}
	return &TraefikHealthCheck{
		Path:     traefikHealthPath,
		Interval: interval.String(),
		Timeout:  timeout.String(),
	}
}
