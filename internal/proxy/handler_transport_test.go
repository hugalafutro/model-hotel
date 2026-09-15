package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

// TestUpstreamClient_HeaderTimeoutCutsSlowHeaders is the end-to-end half: the
// setting has to reach the wire. A provider that sits on its headers longer
// than upstream_header_timeout fails the round trip; lifting the limit with 0s
// lets the same provider answer.
func TestUpstreamClient_HeaderTimeoutCutsSlowHeaders(t *testing.T) {
	ctx := context.Background()
	repo := settings.NewRepository(testDB.Pool())
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	h := &Handler{settingsRepo: repo, upstreamTransport: dialToTestServer(t, srv)}
	t.Cleanup(h.Close)

	set("50ms")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	if resp, err := h.upstreamClient(ctx).Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("50ms header timeout: want the slow provider cut off, got a response")
	} else if ne := net.Error(nil); !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("50ms header timeout: want a timeout error, got %v", err)
	}

	set("0s")
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	resp, err := h.upstreamClient(ctx).Do(req)
	if err != nil {
		t.Fatalf("no header timeout: want the slow provider to answer, got %v", err)
	}
	resp.Body.Close()
}

// TestTransportFor_ConcurrentSettingChanges runs readers against a setting
// that keeps moving, under -race in CI: every reader must get a Transport
// carrying a value the setting held, and the handler ends with one clone.
func TestTransportFor_ConcurrentSettingChanges(t *testing.T) {
	ctx := context.Background()
	repo := settings.NewRepository(testDB.Pool())
	t.Cleanup(func() {
		_ = repo.DeleteKey(ctx, "upstream_header_timeout")
		repo.InvalidateCache("upstream_header_timeout")
	})
	base := &http.Transport{ResponseHeaderTimeout: defaultUpstreamHeaderTimeout}
	h := &Handler{settingsRepo: repo, upstreamTransport: base}
	t.Cleanup(h.Close)

	values := []string{"1m", "3m", "0s", "2m", "4m"}
	allowed := map[time.Duration]bool{time.Minute: true, 3 * time.Minute: true, 0: true, 2 * time.Minute: true, 4 * time.Minute: true}
	// Readers spin until the writer is done, so every change lands while
	// they are inside transportFor and the swap under headerMu is contended.
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if tr := h.transportFor(ctx); !allowed[tr.ResponseHeaderTimeout] {
					t.Errorf("reader got a Transport at %v, not a value the setting held", tr.ResponseHeaderTimeout)
					return
				}
			}
		}()
	}
	for _, v := range values {
		if err := repo.Set(ctx, "upstream_header_timeout", v); err != nil {
			t.Errorf("set: %v", err)
			break
		}
		repo.InvalidateCache("upstream_header_timeout")
		time.Sleep(5 * time.Millisecond)
	}
	close(done)
	wg.Wait()
	last := h.transportFor(ctx)
	h.headerMu.Lock()
	defer h.headerMu.Unlock()
	if last != h.headerTransport || last.ResponseHeaderTimeout != 4*time.Minute {
		t.Fatalf("after the last change: want the one clone at 4m, got %v", last.ResponseHeaderTimeout)
	}
}

// TestTransportFor_FailedReadKeepsCurrent: a settings read that fails (here
// the pool behind the repository is closed after the cache is evicted) is not
// an operator change. The live clone stays, its pool with it, instead of the
// fallback default evicting it; a handler with no clone yet serves the base.
func TestTransportFor_FailedReadKeepsCurrent(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, testDB.Pool().Config())
	if err != nil {
		t.Fatalf("second pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := settings.NewRepository(pool)
	t.Cleanup(func() {
		live := settings.NewRepository(testDB.Pool())
		_ = live.DeleteKey(ctx, "upstream_header_timeout")
	})
	if err := repo.Set(ctx, "upstream_header_timeout", "5m"); err != nil {
		t.Fatalf("set: %v", err)
	}
	repo.InvalidateCache("upstream_header_timeout")
	base := &http.Transport{ResponseHeaderTimeout: defaultUpstreamHeaderTimeout}
	h := &Handler{settingsRepo: repo, upstreamTransport: base}
	t.Cleanup(h.Close)

	clone := h.transportFor(ctx)
	if clone == base || clone.ResponseHeaderTimeout != 5*time.Minute {
		t.Fatalf("want a 5m clone first, got shared=%v timeout=%v", clone == base, clone.ResponseHeaderTimeout)
	}
	// Close first: InvalidateCache re-reads the key after evicting it, so the
	// eviction only sticks once the read behind it fails.
	pool.Close()
	repo.InvalidateCache("upstream_header_timeout")
	if got := h.transportFor(ctx); got != clone {
		t.Fatalf("failed read: want the live clone kept, got shared=%v timeout=%v", got == base, got.ResponseHeaderTimeout)
	}
	fresh := &Handler{settingsRepo: repo, upstreamTransport: base}
	if got := fresh.transportFor(ctx); got != base {
		t.Fatal("failed read with no clone yet: want the shared Transport")
	}
}
