package adminauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newLoginSession stores a live login ceremony and returns its id, so a test
// can drive LoginFinish past the session lookup and into the assertion check.
func newLoginSession(t *testing.T, repo *webauthn.Repository) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	rec := &webauthn.SessionRecord{
		ID:          id,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"challenge":"test"}`),
		Type:        "login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if err := repo.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, id) })
	return id
}

// A rejected passkey assertion is an unauthenticated caller failing a login, so
// the line has to name the caller. Without an address it tells an operator (and
// a log-driven ban) nothing at all, and the credential blob it reports on is
// caller-supplied, so the error it produces goes last.
func TestWebAuthnHandler_LoginFinish_LogsTheClientAddress(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	h := newTestWebAuthnHandler(repo, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }})
	sessionID := newLoginSession(t, repo)

	lines := captureLogLines(t)

	body := `{"session_id": "` + sessionID.String() + `", "credential": {"nonsense": true}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.77:44100"
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	line, ok := findLine(lines(), "webauthn: passkey login failed")
	if !ok {
		t.Fatalf(`no line with msg="webauthn: passkey login failed"; got %v`, lines())
	}
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("a rejected assertion is a caller failure and belongs at warning: %s", line)
	}
	addr := strings.Index(line, " remote_addr=203.0.113.77")
	if addr < 0 {
		t.Fatalf("line does not name the client: %s", line)
	}
	errIdx := strings.Index(line, " error=")
	if errIdx < 0 {
		t.Fatalf("line carries no error attribute: %s", line)
	}
	if addr > errIdx {
		t.Errorf("remote_addr comes after the caller-influenced error text: %s", line)
	}
	if !strings.Contains(line[:errIdx], `reason=assertion_unparseable`) {
		t.Errorf("line does not say why the login failed ahead of the error text: %s", line)
	}
}

// A well-formed assertion that does not verify is the shape a real credential
// stuffing attempt takes, and it has to name its caller the same way.
func TestWebAuthnHandler_LoginFinish_LogsRejectedAssertions(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	repo := webauthn.NewRepository(pool)
	rp, err := webauthn.NewRelyingParty("localhost", "Model Hotel Test", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	h := newTestWebAuthnHandler(repo, rp, nil, &mockAdminAuth{validateFn: func(string) bool { return true }})

	req, w := newChiRequest(http.MethodPost, "/webauthn/login/start", http.NoBody)
	h.LoginStart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LoginStart = %d: %s", w.Code, w.Body.String())
	}
	var startResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&startResp); err != nil {
		t.Fatalf("decode LoginStart: %v", err)
	}
	sessionID, _ := startResp["session_id"].(string)
	if sessionID == "" {
		t.Fatal("LoginStart returned no session_id")
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, uuid.MustParse(sessionID)) })

	lines := captureLogLines(t)

	finishBytes, err := json.Marshal(map[string]any{
		"session_id": sessionID,
		"credential": buildFakeLoginCredential(t, "dGVzdA"),
	})
	if err != nil {
		t.Fatalf("marshal finish body: %v", err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(string(finishBytes)))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "203.0.113.79:44100"
	w2 := httptest.NewRecorder()

	h.LoginFinish(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w2.Code, http.StatusBadRequest, w2.Body.String())
	}
	line, ok := findLine(lines(), "webauthn: passkey login failed")
	if !ok {
		t.Fatalf(`no line with msg="webauthn: passkey login failed"; got %v`, lines())
	}
	addr := strings.Index(line, " remote_addr=203.0.113.79")
	errIdx := strings.Index(line, " error=")
	if addr < 0 || errIdx < 0 {
		t.Fatalf("line is missing the address or the error: %s", line)
	}
	if addr > errIdx {
		t.Errorf("remote_addr comes after the caller-influenced error text: %s", line)
	}
	if !strings.Contains(line[:errIdx], "reason=assertion_rejected") {
		t.Errorf("a verification failure is not distinguished from an unparseable one: %s", line)
	}
}

// A successful ceremony logs no failure line, so the brute-force bucket never
// fills from legitimate logins.
func TestWebAuthnHandler_LoginFinish_SessionMissLogsNoPasskeyFailure(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	h := newTestWebAuthnHandler(repo, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }})

	lines := captureLogLines(t)

	body := `{"session_id": "` + uuid.New().String() + `", "credential": {}}`
	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.78:44100"
	w := httptest.NewRecorder()

	h.LoginFinish(w, req)

	if line, ok := findLine(lines(), "webauthn: passkey login failed"); ok {
		t.Errorf("an unknown ceremony id is not a rejected passkey: %s", line)
	}
}
