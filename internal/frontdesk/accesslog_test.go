package frontdesk

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// captureAccessLines installs a text handler over a buffer as the process
// logger and returns the non-empty lines it collected. The rendered line, not
// the record, is what the log parser reads, so the assertions are about its
// exact shape.
func captureAccessLines(t *testing.T) func() []string {
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

// serveThrough runs one request through the access logger and returns the
// captured lines.
func serveThrough(t *testing.T, method, target string, status int) []string {
	t.Helper()
	lines := captureAccessLines(t)
	h := accessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("body"))
	}))
	r := httptest.NewRequest(method, target, http.NoBody)
	r.RemoteAddr = "203.0.113.11:40000"
	r.Host = "fd.example.com"
	h.ServeHTTP(httptest.NewRecorder(), r)
	return lines()
}

// onlyLine fails unless exactly one line was captured, and returns it.
func onlyLine(t *testing.T, lines []string) string {
	t.Helper()
	if len(lines) != 1 {
		t.Fatalf("want exactly one access line, got %d: %v", len(lines), lines)
	}
	return lines[0]
}

// Front Desk has no access log at all today, so nothing reading its container
// output can see who is knocking. The line it gains has to be the one the
// gateway already emits, field for field, or the shared parser sees Front Desk
// traffic as a different service.
func TestAccessLogger_RejectedRequestCarriesTheGatewayLineShape(t *testing.T) {
	line := onlyLine(t, serveThrough(t, http.MethodGet, "/api/members", http.StatusUnauthorized))

	if !strings.Contains(line, `msg="access: request"`) {
		t.Fatalf("wrong message: %s", line)
	}
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("a 4xx is a client failure and belongs at warning: %s", line)
	}
	for _, want := range []string{
		"method=GET",
		"host=fd.example.com",
		"remote=203.0.113.11",
		"status=401",
		"bytes=4",
	} {
		if !strings.Contains(line, " "+want) {
			t.Errorf("line is missing %q: %s", want, line)
		}
	}
	remote := strings.Index(line, " remote=203.0.113.11")
	path := strings.Index(line, " path=/api/members")
	if remote < 0 || path < 0 {
		t.Fatalf("line is missing the address or the path: %s", line)
	}
	if remote > path {
		t.Errorf("the address must precede the caller-controlled path: %s", line)
	}
	if !strings.HasSuffix(line, " path=/api/members") {
		t.Errorf("the caller-controlled path must be the last field: %s", line)
	}
}

// The level says how a reader should treat the line, and the collection's
// scenarios only ever care about the rejections.
func TestAccessLogger_LevelFollowsTheStatus(t *testing.T) {
	tests := []struct {
		name   string
		target string
		status int
		want   string
	}{
		{"server error", "/api/members", http.StatusInternalServerError, "level=ERROR"},
		{"client error", "/api/members", http.StatusUnauthorized, "level=WARN"},
		{"ordinary request", "/api/members", http.StatusOK, "level=INFO"},
		{"liveness probe", "/healthz", http.StatusOK, "level=DEBUG"},
		{"traefik provider poll", "/traefik/config", http.StatusOK, "level=DEBUG"},
		{"metrics scrape", "/metrics", http.StatusOK, "level=DEBUG"},
		{"event stream", "/api/sse", http.StatusOK, "level=DEBUG"},
		{"probe that was refused", "/healthz", http.StatusServiceUnavailable, "level=ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := onlyLine(t, serveThrough(t, http.MethodGet, tc.target, tc.status))
			if !strings.Contains(line, tc.want) {
				t.Errorf("level = not %s: %s", tc.want, line)
			}
		})
	}
}

// A trailing slash must not lift a polling endpoint back to info: the SPA and
// a reverse proxy both add one, and one line per poll drowns the log.
func TestAccessLogger_NoiseAllowlistIgnoresATrailingSlash(t *testing.T) {
	line := onlyLine(t, serveThrough(t, http.MethodGet, "/healthz/", http.StatusOK))
	if !strings.Contains(line, "level=DEBUG") {
		t.Errorf("a trailing slash defeated the noise allowlist: %s", line)
	}
}

// Serving the SPA's own bundle is not traffic anyone reads, so a successful
// asset fetch logs nothing at all; a missing one still does.
func TestAccessLogger_StaticAssets(t *testing.T) {
	if lines := serveThrough(t, http.MethodGet, "/assets/index-abc123.js", http.StatusOK); len(lines) != 0 {
		t.Errorf("a served asset logged a line: %v", lines)
	}
	line := onlyLine(t, serveThrough(t, http.MethodGet, "/assets/gone.js", http.StatusNotFound))
	if !strings.Contains(line, "level=WARN") {
		t.Errorf("a missing asset must still be logged: %s", line)
	}
}

// The middleware only helps if the router actually runs it, and it has to run
// inside the client-IP resolution so the address it reports is the visitor's
// and not the reverse proxy's.
func TestServer_LogsRejectedControlPlaneRequests(t *testing.T) {
	srv, _ := newTestServer(t)
	lines := captureAccessLines(t)

	rec := do(t, srv, http.MethodGet, "/api/members", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var access string
	for _, l := range lines() {
		if strings.Contains(l, `msg="access: request"`) {
			access = l
		}
	}
	if access == "" {
		t.Fatalf("the router logged no access line; got %v", lines())
	}
	if !strings.Contains(access, " status=401") || !strings.Contains(access, " remote=192.0.2.1") {
		t.Errorf("access line does not report the rejection and its client: %s", access)
	}
	if !strings.HasSuffix(access, " path=/api/members") {
		t.Errorf("the caller-controlled path must be the last field: %s", access)
	}
}

// A handler that writes nothing at all still answered 200, and the line has to
// say so rather than reporting a status of zero, which no reader can classify.
func TestAccessLogger_ImplicitStatus(t *testing.T) {
	lines := captureAccessLines(t)
	h := accessLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	r.RemoteAddr = "203.0.113.11:40000"
	h.ServeHTTP(httptest.NewRecorder(), r)

	line := onlyLine(t, lines())
	if !strings.Contains(line, " status=200") {
		t.Errorf("implicit 200 not reported: %s", line)
	}
}
