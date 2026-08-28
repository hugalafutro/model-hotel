package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// revocableSessionMgr is a session manager whose tokens stop resolving once
// revoke() is called, standing in for an operator revoking a session (or
// disabling the user behind it) while a stream is open.
type revocableSessionMgr struct {
	mu      sync.Mutex
	revoked bool
}

func (m *revocableSessionMgr) revoke() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked = true
}

// setRevoked flips validity in both directions, for staging a failed re-check
// that heals on the next tick.
func (m *revocableSessionMgr) setRevoked(revoked bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked = revoked
}

func (m *revocableSessionMgr) CreateAuthToken(_ context.Context, _, _ []byte, _ webauthn.SessionMeta) (string, error) {
	return "stream-token", nil
}

func (m *revocableSessionMgr) ListAuthSessions(context.Context, []byte, ...string) ([]webauthn.AuthSessionInfo, error) {
	return nil, nil
}

func (m *revocableSessionMgr) RevokeSessionByID(context.Context, []byte, uuid.UUID, ...string) error {
	return webauthn.ErrNotFound
}

func (m *revocableSessionMgr) Validate(_ context.Context, _ string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.revoked
}

func (m *revocableSessionMgr) TokenUser(ctx context.Context, token string) ([]byte, bool) {
	if m.Validate(ctx, token) {
		return []byte("admin"), true
	}
	return nil, false
}

// Authenticate and Verify mirror TokenUser with a plain (never sliding) expiry.
func (m *revocableSessionMgr) Authenticate(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	uid, ok := m.TokenUser(ctx, token)
	if !ok {
		return webauthn.AuthResult{}, false
	}
	return webauthn.AuthResult{UserID: uid, ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL)}, true
}

func (m *revocableSessionMgr) Verify(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	return m.Authenticate(ctx, token)
}

func (m *revocableSessionMgr) RevokeAuthToken(_ context.Context, _ string) bool { return true }

// streamRequest builds a cookie-authenticated SSE request carrying an admin
// identity, as AuthMiddleware would have left it at connect time.
func streamRequest(ctx context.Context) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "stream-token"})
	return req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))
}

// A session revoked after the stream opened must not keep the stream alive:
// the next heartbeat re-checks the credential and closes the connection.
func TestStreamEvents_ClosesWhenSessionRevokedMidStream(t *testing.T) {
	mgr := &revocableSessionMgr{}
	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.streamEvents(rec, streamRequest(ctx), 10*time.Millisecond)
		close(done)
	}()

	// Let a few heartbeats land while the session is still good, proving the
	// re-check does not close a valid stream.
	time.Sleep(60 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("stream closed while the session was still valid")
	default:
	}

	mgr.revoke()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stream stayed open after its session was revoked")
	}

	if body := rec.Body.String(); !strings.Contains(body, ": heartbeat") {
		t.Errorf("expected at least one heartbeat before revocation, got: %q", body)
	}
}

// syncRecorder is a ResponseWriter whose body can be read while the handler is
// still writing, so a test can assert on a live stream rather than only after
// it closes.
type syncRecorder struct {
	mu      sync.Mutex
	hdr     http.Header
	body    strings.Builder
	flushed chan struct{}
	once    sync.Once
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{hdr: http.Header{}, flushed: make(chan struct{})}
}

func (s *syncRecorder) Header() http.Header { return s.hdr }
func (s *syncRecorder) WriteHeader(_ int)   {}
func (s *syncRecorder) Flush()              { s.once.Do(func() { close(s.flushed) }) }

func (s *syncRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(p)
}

func (s *syncRecorder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

// uuidSessionMgr resolves its token to a fixed users-table handle, the shape a
// multi-user (non-admin) session carries.
type uuidSessionMgr struct{ id uuid.UUID }

func (m *uuidSessionMgr) CreateAuthToken(_ context.Context, _, _ []byte, _ webauthn.SessionMeta) (string, error) {
	return "stream-token", nil
}

func (m *uuidSessionMgr) ListAuthSessions(context.Context, []byte, ...string) ([]webauthn.AuthSessionInfo, error) {
	return nil, nil
}

func (m *uuidSessionMgr) RevokeSessionByID(context.Context, []byte, uuid.UUID, ...string) error {
	return webauthn.ErrNotFound
}
func (m *uuidSessionMgr) Validate(_ context.Context, _ string) bool { return true }
func (m *uuidSessionMgr) TokenUser(_ context.Context, _ string) ([]byte, bool) {
	return []byte(m.id.String()), true
}
func (m *uuidSessionMgr) Authenticate(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	uid, _ := m.TokenUser(ctx, token)
	return webauthn.AuthResult{UserID: uid, ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL)}, true
}

func (m *uuidSessionMgr) Verify(ctx context.Context, token string) (webauthn.AuthResult, bool) {
	return m.Authenticate(ctx, token)
}
func (m *uuidSessionMgr) RevokeAuthToken(_ context.Context, _ string) bool { return true }

// mutableUserStore serves one user whose grants a test can change mid-stream,
// standing in for an operator editing the account while a stream is open.
type mutableUserStore struct {
	mu sync.Mutex
	u  *user.User
}

func (s *mutableUserStore) setGrants(g []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.u.Grants = g
}

func (s *mutableUserStore) Get(_ context.Context, id uuid.UUID) (*user.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.u.ID {
		return nil, errors.New("user not found")
	}
	clone := *s.u
	clone.Grants = append([]string(nil), s.u.Grants...)
	return &clone, nil
}

func (s *mutableUserStore) List(context.Context) ([]*user.User, error) { return nil, nil }
func (s *mutableUserStore) Create(context.Context, string, string, *string, string, user.Role, []string, user.Limits, *[]string) (*user.User, error) {
	return nil, nil
}
func (s *mutableUserStore) Update(context.Context, uuid.UUID, string, string, *string, user.Role, []string, bool, user.Limits, *[]string) (*user.User, error) {
	return nil, nil
}
func (s *mutableUserStore) SetPassword(context.Context, uuid.UUID, string) error { return nil }
func (s *mutableUserStore) Delete(context.Context, uuid.UUID) error              { return nil }

// Stripping a grant from a user who already holds an open stream must take
// effect on that stream: the re-check refreshes the identity, so request events
// the user can no longer read over REST stop arriving over SSE too. Without the
// identity refresh the stream keeps delivering under its connect-time grants
// for as long as the client holds the socket.
func TestStreamEvents_RefreshesGrantsMidStream(t *testing.T) {
	uid := uuid.New()
	store := &mutableUserStore{u: &user.User{
		ID: uid, Username: "streamer", Role: user.RoleUser,
		Grants: []string{string(user.GrantLogs)}, Enabled: true,
	}}

	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(&uuidSessionMgr{id: uid})
	h.SetUserAuth(store, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "stream-token"})
	req = req.WithContext(user.WithIdentity(req.Context(),
		&user.Identity{Role: user.RoleUser, Grants: []string{string(user.GrantLogs)}, UserID: &uid}))

	rec := newSyncRecorder()
	done := make(chan struct{})
	go func() {
		h.streamEvents(rec, req, 20*time.Millisecond)
		close(done)
	}()
	awaitStreamOpen(t, rec, done)

	// The event is owned by this user, so the logs grant makes it visible.
	owned := events.Event{
		Type: "request.completed", Severity: "info", Source: "proxy",
		Metadata: map[string]any{"owner_user_id": uid.String()},
	}

	events.Publish(owned)
	if !waitFor(t, func() bool { return strings.Contains(rec.String(), "request.completed") }) {
		t.Fatalf("event never reached the stream while the grant was held: %q", rec.String())
	}

	// Operator strips the logs grant; the next re-check must pick it up.
	store.setGrants(nil)
	baseline := len(rec.String())
	if !waitFor(t, func() bool { return strings.Count(rec.String(), ": heartbeat") >= 2 }) {
		t.Fatal("no re-check happened after the grant was stripped")
	}

	events.Publish(owned)
	time.Sleep(100 * time.Millisecond)
	delivered := rec.String()[baseline:]
	if strings.Contains(delivered, "request.completed") {
		t.Errorf("stream kept delivering request events after the logs grant was revoked: %q", delivered)
	}

	cancel()
	<-done
}

// A passing re-check resets the consecutive-failure count, so failures have to
// be consecutive to close a stream. The third phase is the assertion that
// matters: it is the stream's second failed check overall but the first since a
// passing one, so the stream must survive it. A counter that only ever climbed
// would drop a caller whose credential blipped twice over an afternoon.
func TestStreamEvents_PassingRecheckResetsFailureCount(t *testing.T) {
	mgr := &revocableSessionMgr{}
	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(mgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newSyncRecorder()
	done := make(chan struct{})
	go func() {
		// Wide enough that flipping the session in reaction to one heartbeat
		// comfortably lands before the next re-check.
		h.streamEvents(rec, streamRequest(ctx), 200*time.Millisecond)
		close(done)
	}()
	awaitStreamOpen(t, rec, done)

	// beat waits for the nth heartbeat and asserts the stream is still open.
	beat := func(n int, why string) {
		t.Helper()
		if !waitFor(t, func() bool { return strings.Count(rec.String(), ": heartbeat") >= n }) {
			t.Fatalf("heartbeat %d never arrived (%s): %q", n, why, rec.String())
		}
		select {
		case <-done:
			t.Fatalf("stream closed %s", why)
		default:
		}
	}

	beat(1, "while the session was valid")

	mgr.setRevoked(true)
	beat(2, "on the first failed re-check")

	mgr.setRevoked(false)
	beat(3, "after the credential recovered")

	mgr.setRevoked(true)
	beat(4, "on a failure the passing re-check should have reset the counter for")

	cancel()
	<-done
}

// waitFor polls cond until it holds or a generous deadline passes, so the
// timing-sensitive stream assertions do not depend on a fixed sleep.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// A bearer-authenticated stream is re-checked on the same tick. The admin token
// is validated in memory, so a stream on a still-valid token stays open.
func TestStreamEvents_ValidBearerStreamSurvivesHeartbeats(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(token string) bool { return token == "test-admin-token" },
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req = req.WithContext(user.WithIdentity(req.Context(), user.AdminIdentity()))

	rec := httptest.NewRecorder()
	h.streamEvents(rec, req, 10*time.Millisecond)

	// The handler only returns when the context expires, never earlier, so a
	// heartbeat re-check never rejected the still-valid admin token.
	if ctx.Err() == nil {
		t.Fatal("stream closed before its context expired, so a valid bearer token was rejected")
	}
	if body := rec.Body.String(); !strings.Contains(body, ": heartbeat") {
		t.Errorf("expected heartbeats on a long-lived valid stream, got: %q", body)
	}
}

// RevokeOtherSessions satisfies WebAuthnSessionManager; these tests never call it.
func (m *revocableSessionMgr) RevokeOtherSessions(context.Context, []byte, ...string) (int64, error) {
	return 0, nil
}

// RevokeOtherSessions satisfies WebAuthnSessionManager; these tests never call it.
func (m *uuidSessionMgr) RevokeOtherSessions(context.Context, []byte, ...string) (int64, error) {
	return 0, nil
}

// TestStreamEvents_ReauthVerifiesWithoutSliding pins the heartbeat's call
// site: the mid-stream re-check goes through Verify (a pure lookup) and never
// through Authenticate, so a tab that merely holds its event stream open does
// not keep its own session alive with nobody at it.
func TestStreamEvents_ReauthVerifiesWithoutSliding(t *testing.T) {
	var mu sync.Mutex
	authCalls := 0
	mgr := &mockWebAuthnSessionMgr{
		validateFn: func(_ context.Context, token string) bool { return token == "stream-token" },
		authFn: func(_ context.Context, token string) (webauthn.AuthResult, bool) {
			mu.Lock()
			authCalls++
			mu.Unlock()
			return webauthn.AuthResult{UserID: []byte("admin"), ExpiresAt: time.Now().Add(webauthn.AuthTokenTTL)}, token == "stream-token"
		},
	}
	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(mgr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.streamEvents(rec, streamRequest(ctx), 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if authCalls != 0 {
		t.Errorf("heartbeat re-checks called Authenticate %d times, want 0 (they must Verify)", authCalls)
	}
	if mgr.verifyCalls.Load() == 0 {
		t.Error("no heartbeat re-check ran through Verify")
	}
}

// Closing the event bus (the first step of graceful shutdown) must end an open
// stream promptly, even though the credential is still valid: otherwise the
// idle SSE connection holds http.Server.Shutdown until its deadline.
func TestStreamEvents_ClosesWhenBusClosed(t *testing.T) {
	mgr := &revocableSessionMgr{}
	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(mgr)
	h.eventBus = events.NewBus()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.streamEvents(rec, streamRequest(ctx), time.Hour)
		close(done)
	}()

	// The stream is idle (no heartbeat within the test) and its session valid,
	// so nothing but the bus closing can end it.
	time.Sleep(30 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("stream closed before the bus was closed")
	default:
	}

	h.eventBus.Close()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stream stayed open after the event bus was closed")
	}
}

// awaitStreamOpen blocks until the SSE handler has announced the stream, and
// fails rather than hanging if it never does.
//
// A bare `<-rec.flushed` deadlocks the whole package when the handler returns
// before its first flush — an auth rejection, a non-Flusher writer, any future
// early return. syncRecorder's WriteHeader is a no-op, so a 401 leaves no trace
// either: the symptom is the package timing out ten minutes later with a
// goroutine dump, where the sleep this replaced would have failed in a line.
func awaitStreamOpen(t *testing.T, rec *syncRecorder, done <-chan struct{}) {
	t.Helper()
	select {
	case <-rec.flushed:
	case <-done:
		// Both can be ready at once — a handler that flushes and returns
		// immediately — and select picks at random, so re-check before
		// reporting a failure that did not happen.
		select {
		case <-rec.flushed:
		default:
			t.Fatalf("handler returned before the stream opened: %q", rec.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("stream never opened: %q", rec.String())
	}
}
