package adminauth

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/db"
	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// Shared test harness for the adminauth package. The WebAuthn and TOTP handler
// suites moved here from internal/api during the auth-handler extraction; this
// file provides the bits they used to borrow from the api test package (the
// per-package test DB, newChiRequest, the admin-token mock) plus a small
// TOTP-enabled cache shim that stands in for *api.Handler's cache.

// testMasterKey matches the api test harness value; the TOTP secret is
// encrypted with it at rest.
const testMasterKey = "testmasterkey1234567890abcdef"

// apiTestDBURL / apiTestDB are the per-package test database, set up in TestMain.
// They keep the original variable names the migrated suites reference.
var (
	apiTestDBURL string
	apiTestDB    *db.DB
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var setupErr error
	apiTestDBURL, setupErr = db.SetupTestDB("adminauth")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("adminauth")

	var err error
	apiTestDB, err = db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer apiTestDB.Close()

	util.CloseDockerClient()
	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// newChiRequest builds a JSON request + recorder. Copied from the api test
// helpers it used to share.
func newChiRequest(method, path string, body io.Reader) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req, httptest.NewRecorder()
}

// newClosedPool returns an already-closed pool for error-path tests. Copied
// from the api test helpers.
func newClosedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close()
	return pool
}

// failingResponseWriter always fails on Write, for JSON-encode error-path tests.
// Copied from the api test helpers.
type failingResponseWriter struct {
	header http.Header
}

func (f *failingResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *failingResponseWriter) WriteHeader(_ int) {}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, &mockWriteError{"write failed"}
}

type mockWriteError struct {
	msg string
}

func (e *mockWriteError) Error() string { return e.msg }

// mockAdminAuth is a configurable AdminAuthenticator for tests.
type mockAdminAuth struct {
	validateFn func(token string) bool
}

func (m *mockAdminAuth) Validate(token string) bool {
	if m.validateFn != nil {
		return m.validateFn(token)
	}
	return false
}

// totpEnabledShim stands in for *api.Handler in the TOTP suite: it holds the
// shared TOTP-enabled cache (TotpEnabled/RefreshTotpEnabled, mirroring the main
// server's cache) and exposes an AuthMiddleware built from the same
// admin-or-session gate the handlers use, so tests can assert that a
// /totp/login-minted session token is accepted by that gate.
type totpEnabledShim struct {
	repo        *totpsvc.Repository
	adminMgr    AdminAuthenticator
	sessionMgr  *webauthn.SessionManager
	totpEnabled atomic.Bool
}

func (s *totpEnabledShim) TotpEnabled() bool { return s.totpEnabled.Load() }

func (s *totpEnabledShim) RefreshTotpEnabled(ctx context.Context) {
	enabled, _ := s.repo.IsEnabled(ctx)
	s.totpEnabled.Store(enabled)
}

func (s *totpEnabledShim) AuthMiddleware(next http.Handler) http.Handler {
	return RequireAdminOrSession(s.adminMgr, s.sessionMgr, s.TotpEnabled, next)
}
