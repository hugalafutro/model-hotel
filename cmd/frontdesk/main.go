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
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

func main() {
	dbg := os.Getenv("DEBUG_LOG")
	debuglog.Init(strings.EqualFold(dbg, "true") || dbg == "1")

	port := envOr("PORT", ":8090")
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	dataDir := envOr("DATA_DIR", "./data")
	masterKey := os.Getenv("FRONTDESK_MASTER_KEY")
	publicOrigin := os.Getenv("PUBLIC_ORIGIN")
	traefikAPI := os.Getenv("TRAEFIK_API_URL")
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

	rp, err := newRelyingParty(publicOrigin)
	if err != nil {
		debuglog.Fatal("frontdesk: invalid PUBLIC_ORIGIN", "error", err)
	}

	store, err := frontdesk.Open(filepath.Join(dataDir, "frontdesk.db"), masterKey, allowHTTPMembers)
	if err != nil {
		debuglog.Fatal("frontdesk: failed to open store", "error", err)
	}
	defer func() { _ = store.Close() }()

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
	ipLimiter := ratelimit.NewIPLimiter(defaultIPRPS, defaultIPBurst, config.LoadTrustedProxies(), nil)

	srv := frontdesk.NewServer(frontdesk.ServerConfig{
		Store:        store,
		Poller:       poller,
		Bus:          bus,
		AdminMgr:     adminMgr,
		MasterKey:    masterKey,
		RelyingParty: rp,
		IPLimiter:    ipLimiter,
		UI:           frontdesk.EmbeddedUI(),
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go poller.Run(ctx)

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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		debuglog.Error("frontdesk: graceful shutdown failed", "error", err)
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
