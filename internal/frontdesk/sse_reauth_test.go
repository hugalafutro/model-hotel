package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// The SSE stream re-checks the caller's credentials on every heartbeat, so a
// credential revoked after connect stops the stream instead of living as long
// as the socket. These tests drive the handler's unexported cadence variant at
// a millisecond-scale tick and assert on observable writes and handler return,
// never on a sleep sized to the cadence.

// sseTick is the cadence the tests drive the heartbeat at. Long enough that a
// test reacting to one keep-alive comfortably wins the race against the next
// tick, short enough that a whole test runs in well under a second.
const sseTick = 100 * time.Millisecond

// sseRecorder is a ResponseWriter whose body can be read while the handler is
// still writing, and which signals every write, so a test can react to a
// keep-alive the moment it lands.
type sseRecorder struct {
	mu     sync.Mutex
	hdr    http.Header
	body   strings.Builder
	writes chan struct{}
	// failOn, set before the stream starts, stands in for a client that hung up:
	// the frames it selects fail to write.
	failOn func(frame string) bool
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{hdr: http.Header{}, writes: make(chan struct{}, 64)}
}

func (s *sseRecorder) Header() http.Header { return s.hdr }
func (s *sseRecorder) WriteHeader(int)     {}
func (s *sseRecorder) Flush()              {}

func (s *sseRecorder) Write(p []byte) (int, error) {
	if s.failOn != nil && s.failOn(string(p)) {
		return 0, syscall.EPIPE
	}
	s.mu.Lock()
	n, err := s.body.Write(p)
	s.mu.Unlock()
	select {
	case s.writes <- struct{}{}:
	default:
	}
	return n, err
}

func (s *sseRecorder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

// awaitWrite blocks until the handler writes to the stream, failing the test if
// nothing arrives within a deadline far longer than the tick.
func awaitWrite(t *testing.T, rec *sseRecorder, what string) {
	t.Helper()
	select {
	case <-rec.writes:
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s arrived: body=%q", what, rec.String())
	}
}

// awaitClosed blocks until the handler returns, failing the test otherwise.
func awaitClosed(t *testing.T, done <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("stream stayed open %s", why)
	}
}

// assertOpen fails if the handler has already returned.
func assertOpen(t *testing.T, done <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("stream closed %s", why)
	default:
	}
}

// startStream runs the SSE handler in the background at the test cadence and
// returns the live recorder plus a channel closed when the handler returns.
func startStream(t *testing.T, srv *Server, req *http.Request) (*sseRecorder, chan struct{}) {
	t.Helper()
	rec := newSSERecorder()
	return rec, startStreamWith(t, srv, req, rec, sseTick)
}

// startStreamWith is startStream against a recorder and a cadence the test has
// chosen, for the cases that need a configured writer or a wider tick.
func startStreamWith(t *testing.T, srv *Server, req *http.Request, rec *sseRecorder, every time.Duration) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.streamEvents(rec, req, every)
	}()
	return done
}

// sseRequest builds a GET /api/sse request on a context the test controls.
func sseRequest(t *testing.T) (*http.Request, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/sse", http.NoBody), cancel
}

// A stream opened while the server is shutting down ends at once: its re-auth
// ticker is refused, and a stream whose credentials are never re-checked would
// outlive the revocation it is there to notice.
func TestSSEStreamEndsWhileShuttingDown(t *testing.T) {
	srv, _ := newTestServer(t)
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	req, _ := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)
	rec := newSSERecorder()
	done := startStreamWith(t, srv, req, rec, sseTick)
	awaitClosed(t, done, "while the server was shutting down")
}

// A device unpaired while its stream is open must lose the stream: the next
// re-checks fail and the handler returns.
func TestSSEClosesAfterDeviceRevoked(t *testing.T) {
	srv, store := newTestServer(t)
	token, id := pairDevice(t, srv, RoleMonitor, "phone")

	req, cancel := sseRequest(t)
	defer cancel()
	req.Header.Set("Authorization", "Bearer "+token)

	rec, done := startStream(t, srv, req)
	awaitWrite(t, rec, "keep-alive on a live device token")

	if err := store.RevokePairedDevice(context.Background(), id); err != nil {
		t.Fatalf("RevokePairedDevice: %v", err)
	}
	awaitClosed(t, done, "after the device was unpaired")

	if body := rec.String(); !strings.Contains(body, ": keep-alive") {
		t.Errorf("expected keep-alives before revocation, got %q", body)
	}
}

// A session revoked while its stream is open must lose the stream too, whether
// the token rode a cookie or the Authorization header.
func TestSSEClosesAfterSessionRevoked(t *testing.T) {
	for _, carrier := range []string{"cookie", "bearer"} {
		t.Run(carrier, func(t *testing.T) {
			srv, _ := newTestServer(t)
			ctx := context.Background()
			token, err := srv.SessionManager().CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
			if err != nil {
				t.Fatalf("CreateAuthToken: %v", err)
			}

			req, cancel := sseRequest(t)
			defer cancel()
			if carrier == "cookie" {
				req.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: token})
			} else {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			rec, done := startStream(t, srv, req)
			awaitWrite(t, rec, "keep-alive on a live session")

			if !srv.SessionManager().RevokeAuthToken(ctx, token) {
				t.Fatal("RevokeAuthToken did not find the session")
			}
			awaitClosed(t, done, "after its session was revoked")
		})
	}
}

// A credential that stays valid must keep its stream: many re-checks in a row
// pass, the failure counter never builds, and only the client hanging up (the
// request context) ends the stream.
func TestSSEValidCredentialStaysConnected(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancel := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

	rec, done := startStream(t, srv, req)
	for range 3 {
		awaitWrite(t, rec, "keep-alive")
		assertOpen(t, done, "while its admin token was still valid")
	}

	cancel()
	awaitClosed(t, done, "after the client hung up")

	if got := strings.Count(rec.String(), ": keep-alive"); got < 3 {
		t.Errorf("keep-alives = %d, want at least 3 on a long-lived valid stream", got)
	}
}

// The re-check must not stamp last_seen_at: that column records requests the
// device made, and a heartbeat this server drives would make an idle phone look
// permanently active in the Paired devices list.
func TestSSEReauthDoesNotStampDeviceLastSeen(t *testing.T) {
	srv, store := newTestServer(t)
	token, _ := pairDevice(t, srv, RoleMonitor, "phone")

	req, cancel := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+token)

	rec, done := startStream(t, srv, req)
	awaitWrite(t, rec, "keep-alive")
	awaitWrite(t, rec, "second keep-alive")
	cancel()
	awaitClosed(t, done, "after the client hung up")

	devices, err := store.ListPairedDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("ListPairedDevices = %v, %v", devices, err)
	}
	if devices[0].LastSeenAt != nil {
		t.Errorf("re-auth stamped last_seen_at = %v, want it untouched", devices[0].LastSeenAt)
	}
}

// One failed re-check must not close the stream: the tolerance is there so a
// transient store failure does not log the operator out. A stream whose
// credential never resolves therefore writes exactly one keep-alive (failure 1)
// and closes on the second failure.
func TestSSESingleFailureDoesNotClose(t *testing.T) {
	srv, _ := newTestServer(t)

	// No bearer and no cookie: every re-check fails, from the first tick.
	req, cancel := sseRequest(t)
	defer cancel()

	rec, done := startStream(t, srv, req)
	awaitClosed(t, done, "on a credential that never resolved")

	if got := strings.Count(rec.String(), ": keep-alive"); got != 1 {
		t.Errorf("keep-alives = %d, want exactly 1 (the first failure is tolerated, the second closes)", got)
	}
}

// A failed re-check followed by a passing one resets the counter, so the stream
// survives. TOTP is flipped on to make the raw admin token stop resolving (with
// TOTP enabled it is a first factor only) and back off to heal it, which is the
// in-memory equivalent of a store blip clearing on the next tick.
//
// The last flip is what pins the reset: it is the second failure of the stream
// but the first since the passing check, so the stream must live through it. A
// counter that only ever climbed would close here.
//
// Each healing flip has to land before the tick after the one it reacts to, so
// this is the one test that runs at a wider cadence than the rest.
func TestSSEFailedCheckRecovers(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancel := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

	rec := newSSERecorder()
	done := startStreamWith(t, srv, req, rec, 500*time.Millisecond)
	awaitWrite(t, rec, "keep-alive on a live admin token")

	srv.totpStatus.val.Store(true)
	awaitWrite(t, rec, "keep-alive after the tolerated failure")
	assertOpen(t, done, "on a single failed re-check")

	srv.totpStatus.val.Store(false)
	awaitWrite(t, rec, "keep-alive after the credential recovered")
	assertOpen(t, done, "after a passing re-check followed the failure")

	srv.totpStatus.val.Store(true)
	awaitWrite(t, rec, "keep-alive after the second isolated failure")
	assertOpen(t, done, "on a failure the passing re-check should have reset the counter for")

	cancel()
	awaitClosed(t, done, "after the client hung up")
}

// A device-token lookup that errors for a reason other than "no such device"
// falls through to the admin/session gate, exactly as requireAuth does. An admin
// bearer never depended on paired_devices, so a broken table must not close its
// stream; a device token has nothing else to fall back on, so its stream closes
// once the tolerance is used up.
func TestSSEDeviceStoreErrorFallsThroughToTheAdminGate(t *testing.T) {
	t.Run("admin bearer survives", func(t *testing.T) {
		srv, store := newTestServer(t)

		req, cancel := sseRequest(t)
		req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

		// Closing the store makes every DeviceByTokenHash return a driver error.
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}

		rec, done := startStream(t, srv, req)
		for range 3 {
			awaitWrite(t, rec, "keep-alive")
			assertOpen(t, done, "on an admin bearer while the device table was unreadable")
		}

		cancel()
		awaitClosed(t, done, "after the client hung up")
	})

	t.Run("device token closes", func(t *testing.T) {
		srv, store := newTestServer(t)
		token, _ := pairDevice(t, srv, RoleMonitor, "phone")

		req, cancel := sseRequest(t)
		defer cancel()
		req.Header.Set("Authorization", "Bearer "+token)

		if err := store.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}

		rec, done := startStream(t, srv, req)
		awaitClosed(t, done, "on a device token the store could no longer confirm")
		if got := strings.Count(rec.String(), ": keep-alive"); got != 1 {
			t.Errorf("keep-alives = %d, want exactly 1 (the first failure is tolerated, the second closes)", got)
		}
	})
}

// Bus events still reach the stream alongside the re-auth tick.
func TestSSEDeliversBusEvents(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancel := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

	rec, done := startStream(t, srv, req)
	// The first keep-alive proves the handler is subscribed — it subscribes
	// before announcing, so anything written means the subscription exists.
	awaitWrite(t, rec, "keep-alive")
	srv.bus.Publish(events.Event{Type: "member.state_changed", Severity: "info", Source: "frontdesk"})

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(rec.String(), "member.state_changed") {
		if time.Now().After(deadline) {
			t.Fatalf("event never reached the stream: %q", rec.String())
		}
		select {
		case <-rec.writes:
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	awaitClosed(t, done, "after the client hung up")
}

// A client that hangs up mid-write ends the stream rather than looping on a
// dead socket, on both the keep-alive and the event frame.
func TestSSEStopsWhenTheClientIsGone(t *testing.T) {
	t.Run("keep-alive", func(t *testing.T) {
		srv, _ := newTestServer(t)
		req, cancel := sseRequest(t)
		defer cancel()
		req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

		rec := newSSERecorder()
		rec.failOn = func(string) bool { return true }
		done := startStreamWith(t, srv, req, rec, sseTick)
		awaitClosed(t, done, "after the keep-alive write failed")
	})

	t.Run("event", func(t *testing.T) {
		srv, _ := newTestServer(t)
		req, cancel := sseRequest(t)
		defer cancel()
		req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

		rec := newSSERecorder()
		rec.failOn = func(frame string) bool { return strings.HasPrefix(frame, "data: ") }
		done := startStreamWith(t, srv, req, rec, sseTick)
		awaitWrite(t, rec, "keep-alive")
		srv.bus.Publish(events.Event{Type: "member.state_changed", Severity: "info", Source: "frontdesk"})
		awaitClosed(t, done, "after the event write failed")
	})
}

// An event that cannot be encoded is skipped, not fatal: the stream keeps
// running for the events that follow.
func TestSSESkipsUnencodableEvent(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancel := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

	rec, done := startStream(t, srv, req)
	awaitWrite(t, rec, "keep-alive")

	// A func value in the metadata makes json.Marshal fail on this event only.
	srv.bus.Publish(events.Event{
		Type: "member.state_changed", Severity: "info", Source: "frontdesk",
		Metadata: map[string]any{"unencodable": func() {}},
	})
	awaitWrite(t, rec, "keep-alive after the unencodable event")
	assertOpen(t, done, "on an event it could not encode")
	if body := rec.String(); strings.Contains(body, "data: ") {
		t.Errorf("unencodable event was written anyway: %q", body)
	}

	cancel()
	awaitClosed(t, done, "after the client hung up")
}

// The non-streaming path stays a 500: a ResponseWriter that cannot flush cannot
// carry an event stream.
func TestSSERejectsNonFlushingWriter(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancel := sseRequest(t)
	defer cancel()
	rec := &nonFlushingWriter{hdr: http.Header{}}
	srv.streamEvents(rec, req, sseTick)

	if rec.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.status)
	}
}

// nonFlushingWriter is a ResponseWriter without http.Flusher.
type nonFlushingWriter struct {
	hdr    http.Header
	status int
}

func (w *nonFlushingWriter) Header() http.Header         { return w.hdr }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.status = code }
func (w *nonFlushingWriter) Write(p []byte) (int, error) { return len(p), nil }

// Closing the bus (the first step of graceful shutdown) must end an open
// stream even though its credentials are still valid: otherwise the idle
// connection holds http.Server.Shutdown until its deadline.
func TestSSEClosesWhenBusClosed(t *testing.T) {
	srv, _ := newTestServer(t)

	req, _ := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)

	rec, done := startStream(t, srv, req)
	awaitWrite(t, rec, "keep-alive")
	assertOpen(t, done, "before the bus was closed")

	srv.bus.Close()
	awaitClosed(t, done, "after the event bus was closed")
}
