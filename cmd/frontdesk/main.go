// Command frontdesk is the HA "Front Desk" control-plane server: it stores the
// member list in an embedded SQLite file, serves the admin UI + REST/SSE API,
// and emits the Traefik dynamic config that the data-plane proxy polls. It is
// never in the request path.
//
// This file is env wiring only; all logic lives in internal/frontdesk (cmd/ is
// excluded from coverage).
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	gowa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/frontdesk"
	"github.com/hugalafutro/model-hotel/internal/otelexport"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// version is the running Front Desk build, set at build time via
// -ldflags -X main.version=... (see the Makefile / Dockerfile.frontdesk). It is
// surfaced read-only over GET /api/version so the UI footer can show which build
// is deployed. Defaults to "dev" for un-stamped builds.
var version = "dev"

func main() {
	dbg := os.Getenv("DEBUG_LOG")
	debuglog.Init(strings.EqualFold(dbg, "true") || dbg == "1")

	// Root context for process-lifetime background work and log-exporter shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// OTLP log export: when the standard OTEL_EXPORTER_OTLP_* endpoint vars are
	// set, fan the same structured records Front Desk already logs out to an
	// OpenTelemetry collector, mirroring the main server. Logs only — no spans,
	// no OTLP metrics (Prometheus stays the metrics path). Front Desk has no
	// app-log ring buffer, so the fan-out base is the plain stdout handler.
	var otelLogShutdown func(context.Context) error
	if otelexport.LogsEnabled() {
		otelHandler, shutdown, oerr := otelexport.NewSlogHandler(ctx, "front-desk", debuglog.Level())
		if oerr != nil {
			debuglog.Error("frontdesk: OTLP log export init failed; continuing without it", "error", oerr)
		} else {
			debuglog.SetHandler(debuglog.NewFanout(debuglog.StdoutHandler(), otelHandler))
			otelLogShutdown = shutdown
			// Logged after SetHandler installs the fan-out so the confirmation
			// itself is also exported to the OTLP collector.
			debuglog.Info("frontdesk: OTLP log export enabled")
		}
	}

	port := envOr("PORT", ":8090")
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	dataDir := envOr("DATA_DIR", "./data")
	masterKey := os.Getenv("FRONTDESK_MASTER_KEY")
	publicOrigin := os.Getenv("PUBLIC_ORIGIN")
	traefikAPI := os.Getenv("TRAEFIK_API_URL")
	// The host port the load balancer is published on (LB_PORT in the HA .env),
	// passed in so the wizard's final step can tell the operator exactly where to
	// point their clients. Informational only; Front Desk does not bind it.
	lbPort := envOr("FLEET_LB_PORT", "8080")
	allowHTTPMembers := strings.EqualFold(os.Getenv("FRONTDESK_ALLOW_HTTP_MEMBERS"), "true") ||
		os.Getenv("FRONTDESK_ALLOW_HTTP_MEMBERS") == "1"

	// HTTPS-only ingress: refuse to start without PUBLIC_ORIGIN so a misconfigured
	// plain-HTTP deployment fails loudly instead of silently weakening passkeys.
	if publicOrigin == "" {
		debuglog.Fatal("frontdesk: PUBLIC_ORIGIN is required (the public https:// hostname behind the TLS proxy)")
	}
	// FRONTDESK_MASTER_KEY encrypts member admin tokens and the TOTP secret at
	// rest; like the main server's MASTER_KEY it must be set out-of-band.
	if masterKey == "" {
		debuglog.Fatal("frontdesk: FRONTDESK_MASTER_KEY is required")
	}
	warnWeakMasterKey(masterKey)

	rp, err := newRelyingParty(publicOrigin)
	if err != nil {
		debuglog.Fatal("frontdesk: invalid PUBLIC_ORIGIN", "error", err)
	}

	store, err := frontdesk.Open(filepath.Join(dataDir, "frontdesk.db"), masterKey, allowHTTPMembers)
	if err != nil {
		debuglog.Fatal("frontdesk: failed to open store", "error", err)
	}
	// No deferred close: srv.Shutdown owns the store from here (it closes it after
	// draining), and every bail-out below is debuglog.Fatal, which is os.Exit and
	// runs no defers. A deferred close would only ever be a second, redundant one.

	adminMgr, isNew, err := admin.New(dataDir, os.Getenv("FRONTDESK_TOKEN"))
	if err != nil {
		debuglog.Fatal("frontdesk: failed to initialize admin token", "error", err)
	}
	if isNew {
		// Printed once so the operator can capture the generated UI login token.
		debuglog.Info("frontdesk: generated Front Desk login token", "token", adminMgr.Token())
	}

	bus := events.NewBus()
	poller := frontdesk.NewPoller(store, bus, traefikAPI)
	// Resolve this Front Desk's persistent identity once and stamp it onto every
	// announce. Members now reject announces without an id (it is how they know
	// which control plane owns them), so a Front Desk that cannot establish its
	// identity cannot manage any members: fail fast rather than run degraded.
	fdID, err := store.EnsureFrontdeskID(ctx)
	if err != nil {
		debuglog.Fatal("frontdesk: could not resolve Front Desk identity", "error", err)
	}
	poller.SetFrontdeskID(fdID)
	// One TRUSTED_PROXIES parse feeds both consumers: the login rate limiter
	// and the server's client-IP resolution for logs and session metadata.
	trustedProxies := config.LoadTrustedProxies()
	ipLimiter := ratelimit.NewIPLimiter(defaultIPRPS, defaultIPBurst, trustedProxies, nil)

	srv := frontdesk.NewServer(frontdesk.ServerConfig{
		Store:          store,
		Poller:         poller,
		Bus:            bus,
		AdminMgr:       adminMgr,
		MasterKey:      masterKey,
		RelyingParty:   rp,
		IPLimiter:      ipLimiter,
		TrustedProxies: trustedProxies,
		UI:             frontdesk.EmbeddedUI(),
		// Dedicated Prometheus scrape bearer; when unset, /metrics falls back to
		// the admin-or-session gate (never unauthenticated).
		MetricsToken: os.Getenv("FRONTDESK_METRICS_TOKEN"),
		// Dedicated bearer for Traefik's /traefik/config polls; when unset the
		// endpoint stays open for compose-internal use (Traefik cannot log in,
		// so there is no admin-auth fallback to give it).
		TraefikToken: os.Getenv("FRONTDESK_TRAEFIK_TOKEN"),
		LBPort:       lbPort,
		Version:      version,
		// Secure attribute on the fd_session/fd_csrf pair; shares COOKIE_SECURE
		// and its normalization with the dashboard so one fleet-wide knob covers
		// both surfaces.
		CookieSecure: config.NormalizeCookieSecure(os.Getenv("COOKIE_SECURE")),
	})

	// Every process-lifetime loop runs on the server's background group, so the
	// drain below joins them before the store closes. Each returns when ctx is
	// done, which the signal handler above does on SIGINT/SIGTERM.
	srv.StartBackground(ctx, poller.Run)
	srv.StartBackground(ctx, srv.RunAutoSync)
	srv.StartBackground(ctx, srv.RunQuotaDistribute)
	srv.StartBackground(ctx, srv.RunFleetState)
	srv.StartBackground(ctx, srv.RunBackupWatch)
	srv.StartBackground(ctx, srv.RunAlerts)

	httpServer := &http.Server{
		Addr:              port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		debuglog.Info("frontdesk: listening", "addr", port, "public_origin", publicOrigin)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			debuglog.Fatal("frontdesk: server error", "error", err)
		}
	}()

	<-ctx.Done()
	debuglog.Info("frontdesk: shutting down")
	// End every open SSE stream first: each is an in-flight request that
	// Shutdown would otherwise wait on until the deadline, so with a Front Desk
	// tab open every restart burned the full 10s.
	bus.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		debuglog.Error("frontdesk: graceful shutdown failed", "error", err)
	}
	// Nothing accepts requests any more, so no new background work can start:
	// drain what is in flight and close the store. Its own budget, not
	// shutdownCtx, which the HTTP drain may already have spent. Bounded, so a
	// loop that ignores its cancellation delays exit rather than hanging it.
	// 5s, not the HTTP drain's 10s: every loop returns on ctx within a tick, so the
	// budget only has to absorb a straggler. It keeps the worst case (10s HTTP + 5s
	// drain + 5s OTLP flush) at cmd/server's shape plus the drain, which the
	// stop_grace_period on the front-desk service in deploy/ha covers.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		debuglog.Error("frontdesk: store shutdown failed", "error", err)
	}
	debuglog.Info("frontdesk: stopped")
	// Flush and close the OTLP log exporter so batched records aren't lost. Use a
	// fresh context, not shutdownCtx: a slow HTTP drain can consume most or all of
	// that budget, leaving the exporter no time to flush (or an already-expired
	// context). Mirrors the main server's dedicated flush deadline.
	if otelLogShutdown != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := otelLogShutdown(flushCtx); err != nil {
			debuglog.Error("frontdesk: OTLP log export shutdown failed", "error", err)
		}
	}
}

const (
	defaultIPRPS   = 5
	defaultIPBurst = 10
)

// newRelyingParty builds the WebAuthn relying party from PUBLIC_ORIGIN: the RP
// ID is the hostname and the expected origin is scheme://host.
func newRelyingParty(publicOrigin string) (*gowa.WebAuthn, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Hostname() == "" {
		return nil, errInvalidOrigin
	}
	// HTTPS-only ingress: refuse a plain-http origin so a misconfigured deploy
	// fails loudly instead of starting WebAuthn with an insecure expected origin.
	// http is allowed only for loopback hosts (localhost / 127.0.0.1 / ::1), which
	// browsers already treat as a secure context for WebAuthn, so local testing
	// without a TLS proxy still works.
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return nil, errInsecureOrigin
	}
	return webauthn.NewRelyingParty(u.Hostname(), "Front Desk", []string{u.Scheme + "://" + u.Host})
}

// isLoopbackHost reports whether host is localhost or a loopback IP literal.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var (
	errInvalidOrigin  = &originError{}
	errInsecureOrigin = errors.New("PUBLIC_ORIGIN must be https:// (http is allowed only for localhost); HTTPS-only ingress is required")
)

type originError struct{}

func (e *originError) Error() string {
	return "PUBLIC_ORIGIN must be an absolute URL like https://hotel.example.com"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// warnWeakMasterKey mirrors the main server's MASTER_KEY check: the at-rest
// KDF runs with deliberately low cost on the assumption that the key is
// high-entropy, so a short one is called out at boot. Warn, never fail:
// rotating the key would invalidate every stored member token and the TOTP
// secret, so an existing deployment keeps starting.
func warnWeakMasterKey(key string) {
	if !config.WeakMasterKey(key) {
		return
	}
	debuglog.Warn("frontdesk: FRONTDESK_MASTER_KEY is shorter than recommended — a low-entropy key weakens at-rest encryption of member tokens and the TOTP secret; generate a strong one with `openssl rand -base64 32`",
		"length", len(key), "recommended_min", config.RecommendedMasterKeyLength)
}
