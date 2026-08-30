package adminauth

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// captureLogLines installs a text handler over a buffer as the process logger
// and returns the lines it collected. The rendered line, not the record, is
// what a log reader sees, so the assertions below are about its exact shape.
func captureLogLines(t *testing.T) func() []string {
	t.Helper()
	var buf bytes.Buffer
	debuglog.SetHandler(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { debuglog.Init(false) })
	return func() []string {
		var lines []string
		for _, l := range strings.Split(buf.String(), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
		return lines
	}
}

// findLine returns the first captured line whose message is msg, matched on the
// rendered msg="..." field so a message quoted inside an attribute value cannot
// stand in for the real one.
func findLine(lines []string, msg string) (string, bool) {
	want := `msg="` + msg + `"`
	for _, l := range lines {
		if strings.Contains(l, want) {
			return l, true
		}
	}
	return "", false
}

// assertAddressBeforePath pins the two rules the CrowdSec collection relies on:
// the line names the real client under remote_addr, and the caller-controlled
// request path comes after it, so a reader meets every field the binary
// vouches for before anything a visitor wrote.
func assertAddressBeforePath(t *testing.T, line, wantAddr string) {
	t.Helper()
	addr := strings.Index(line, " remote_addr="+wantAddr)
	if addr < 0 {
		t.Fatalf("line does not name the client under remote_addr=%s: %s", wantAddr, line)
	}
	path := strings.Index(line, " path=")
	if path < 0 {
		t.Fatalf("line carries no path attribute: %s", line)
	}
	if addr > path {
		t.Errorf("remote_addr comes after the caller-controlled path: %s", line)
	}
}

// A rejected admin request has to leave a trace naming the caller, or admin
// token brute force against Front Desk (whose whole control plane sits behind
// this middleware) is invisible to anything reading the logs. The two
// rejections carry the two distinct messages the collection's admin_token
// classification anchors on.
func TestRequireAdminOrSession_LogsRejectedCredentials(t *testing.T) {
	mgr, adminTok, _ := newValidAdminSession(t)
	adminOnly := &mockAdminAuth{validateFn: func(token string) bool { return token == "raw-admin" }}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name    string
		method  string
		build   func(r *http.Request)
		wantMsg string
		status  int
	}{
		{
			name:    "no bearer at all",
			build:   func(*http.Request) {},
			wantMsg: "auth: admin request missing bearer token",
			status:  http.StatusUnauthorized,
		},
		{
			name:    "a bearer that resolves to nothing",
			build:   func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
			wantMsg: "auth: admin request with invalid token",
			status:  http.StatusUnauthorized,
		},
		{
			name:   "an admin cookie on an unsafe method with no CSRF header",
			method: http.MethodPost,
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: adminTok})
			},
			wantMsg: "auth: CSRF check failed",
			status:  http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := captureLogLines(t)
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "/api/members", http.NoBody)
			r.RemoteAddr = "203.0.113.42:51000"
			tc.build(r)
			w := httptest.NewRecorder()

			RequireAdminOrSession(adminOnly, mgr, nil, authcookie.FrontDesk, "auto", next).ServeHTTP(w, r)

			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			line, ok := findLine(lines(), tc.wantMsg)
			if !ok {
				t.Fatalf("no line with msg=%q; got %v", tc.wantMsg, lines())
			}
			if !strings.Contains(line, "level=WARN") {
				t.Errorf("rejection logged at the wrong level (abuse detection reads warnings): %s", line)
			}
			assertAddressBeforePath(t, line, "203.0.113.42")
		})
	}
}

// An admitted request must stay silent: a line per successful admin call would
// bury the rejections and fill the brute-force bucket with legitimate traffic.
func TestRequireAdminOrSession_AdmittedRequestLogsNothing(t *testing.T) {
	mgr, adminTok, _ := newValidAdminSession(t)
	adminOnly := &mockAdminAuth{validateFn: func(token string) bool { return token == "raw-admin" }}
	lines := captureLogLines(t)

	r := httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	r.RemoteAddr = "203.0.113.42:51000"
	r.Header.Set("Authorization", "Bearer "+adminTok)
	w := httptest.NewRecorder()
	RequireAdminOrSession(adminOnly, mgr, nil, authcookie.FrontDesk, "auto", w2h(t)).ServeHTTP(w, r)

	for _, msg := range []string{
		"auth: admin request missing bearer token",
		"auth: admin request with invalid token",
		"auth: CSRF check failed",
	} {
		if line, ok := findLine(lines(), msg); ok {
			t.Errorf("an admitted request logged a rejection: %s", line)
		}
	}
}

// w2h is a next handler that records nothing and answers 200.
func w2h(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}
