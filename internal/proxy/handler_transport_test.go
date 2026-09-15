package proxy

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/settings"
)

// TestTransportFor_FollowsUpstreamHeaderTimeout pins the contract that turned
// the compiled-in two-minute ResponseHeaderTimeout into a setting: the default
// rides on the shared Transport itself, any other value lives on one clone that
// keeps the guarded dialer, and moving the setting back to the default returns
// the shared Transport rather than a second clone.
func TestTransportFor_FollowsUpstreamHeaderTimeout(t *testing.T) {
	ctx := context.Background()
	pool := testDB.Pool()
	repo := settings.NewRepository(pool)
	t.Cleanup(func() {
		_ = repo.DeleteKey(ctx, "upstream_header_timeout")
		repo.InvalidateCache("upstream_header_timeout")
	})
	set := func(v string) {
		t.Helper()
		if err := repo.Set(ctx, "upstream_header_timeout", v); err != nil {
			t.Fatalf("set: %v", err)
		}
		repo.InvalidateCache("upstream_header_timeout")
	}

	dialed := false
	base := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = true
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		ResponseHeaderTimeout: defaultUpstreamHeaderTimeout,
	}
	h := &Handler{settingsRepo: repo, upstreamTransport: base}

	if got := h.transportFor(ctx); got != base {
		t.Fatalf("unset setting: want the shared Transport, got a clone with %v", got.ResponseHeaderTimeout)
	}

	set("5m")
	clone := h.transportFor(ctx)
	if clone == base || clone.ResponseHeaderTimeout != 5*time.Minute {
		t.Fatalf("5m: want a clone at 5m, got shared=%v timeout=%v", clone == base, clone.ResponseHeaderTimeout)
	}
	if again := h.transportFor(ctx); again != clone {
		t.Fatalf("5m again: want the same clone reused, got a new one")
	}
	if clone.DialContext == nil {
		t.Fatal("clone lost the base DialContext, the SSRF guard rides on it")
	}
	if _, err := clone.DialContext(ctx, "tcp", "127.0.0.1:1"); err == nil || !dialed {
		t.Fatalf("clone DialContext: want the base dialer to run (dialed=%v), err=%v", dialed, err)
	}

	set("0s")
	if got := h.transportFor(ctx); got == base || got == clone || got.ResponseHeaderTimeout != 0 {
		t.Fatalf("0s: want a fresh clone with no header timeout, got shared=%v old=%v timeout=%v", got == base, got == clone, got.ResponseHeaderTimeout)
	}

	set("-1s")
	if got := h.transportFor(ctx); got.ResponseHeaderTimeout != 0 {
		t.Fatalf("negative: want clamped to no limit, got %v", got.ResponseHeaderTimeout)
	}
	set("2m")
	if got := h.transportFor(ctx); got != base {
		t.Fatalf("back at the default: want the shared Transport, got a clone")
	}
	if h.headerTransport != nil {
		t.Fatal("back at the default: the superseded clone should be dropped, not kept")
	}

	// Close releases the clone's idle connections along with the base's, and
	// must cope without a clone; the next read after Close still serves.
	set("3m")
	withClone := h.transportFor(ctx)
	h.Close()
	if got := h.transportFor(ctx); got != withClone {
		t.Fatal("after Close: the clone is still the current Transport until the setting moves")
	}
	noClone := &Handler{settingsRepo: repo, upstreamTransport: base}
	noClone.Close()

	bare := &Handler{settingsRepo: repo}
	if got := bare.upstreamClient(ctx).Transport; got != (*http.Transport)(nil) {
		t.Fatalf("no Transport: want nil passed through, got %v", got)
	}
	noRepo := &Handler{upstreamTransport: base}
	if got := noRepo.transportFor(ctx); got != base {
		t.Fatal("no settings repository: want the shared Transport untouched")
	}
}
