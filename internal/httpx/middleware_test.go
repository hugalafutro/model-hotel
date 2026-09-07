package httpx

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"sub-second rounds up to 1", 200 * time.Millisecond, "1"},
		{"zero floors at 1", 0, "1"},
		{"negative floors at 1", -time.Second, "1"},
		{"exact seconds", 30 * time.Second, "30"},
		{"fraction rounds up", 30500 * time.Millisecond, "31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			SetRetryAfter(w, tt.d)
			if got := w.Header().Get("Retry-After"); got != tt.want {
				t.Errorf("Retry-After = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRespondTooManyAttempts(t *testing.T) {
	w := httptest.NewRecorder()
	RespondTooManyAttempts(w, 5*time.Second)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}
	if !strings.Contains(w.Body.String(), "too many failed attempts") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{
		"/":            "/",
		"/api/models/": "/api/models",
		"/api/models":  "/api/models",
		"//":           "",
	} {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccessLoggerLevels(t *testing.T) {
	noisy := func(_, path string) bool { return path == "/healthz" }
	tests := []struct {
		name      string
		path      string
		status    int
		writeBody bool
		wantLevel slog.Level
		wantLine  bool
	}{
		{"server error", "/api/x", 500, false, slog.LevelError, true},
		{"client error", "/api/x", 400, false, slog.LevelWarn, true},
		{"noisy poll", "/healthz/", 200, false, slog.LevelDebug, true},
		{"normal request", "/api/x", 200, false, slog.LevelInfo, true},
		{"implicit 200 from a body write", "/api/x", 0, true, slog.LevelInfo, true},
		{"successful asset is dropped", "/assets/app.js", 200, false, 0, false},
		{"failed asset is logged", "/assets/app.js", 404, false, slog.LevelWarn, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capt := captureLogs(t)
			h := AccessLogger(noisy)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.writeBody {
					_, _ = w.Write([]byte("ok"))
					return
				}
				w.WriteHeader(tt.status)
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.path, http.NoBody))

			if !tt.wantLine {
				if capt.msg != "" {
					t.Fatalf("expected no log line, got %q", capt.msg)
				}
				return
			}
			if capt.msg != "access: request" {
				t.Errorf("message = %q, want the access line", capt.msg)
			}
			if capt.last != tt.wantLevel {
				t.Errorf("level = %v, want %v", capt.last, tt.wantLevel)
			}
			if tt.writeBody && capt.status != 200 {
				t.Errorf("implicit 200 not normalized: status=%d", capt.status)
			}
		})
	}
}

func TestAccessLoggerNilPredicate(t *testing.T) {
	capt := captureLogs(t)
	h := AccessLogger(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if capt.last != slog.LevelInfo {
		t.Errorf("nil predicate should log at info, got %v", capt.last)
	}
}

func TestSecurityHeaders(t *testing.T) {
	serve := func(allowEmbed, overTLS bool) http.Header {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		if overTLS {
			r.TLS = &tls.ConnectionState{}
		}
		SecurityHeaders(allowEmbed)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)
		return w.Header()
	}

	h := serve(false, false)
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options")
	}
	if h.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Error("missing Referrer-Policy")
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want frame-ancestors", h.Get("Content-Security-Policy"))
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS set over plain HTTP")
	}

	embed := serve(true, false)
	if embed.Get("X-Frame-Options") != "" {
		t.Error("X-Frame-Options set with allowEmbed")
	}
	if strings.Contains(embed.Get("Content-Security-Policy"), "frame-ancestors") {
		t.Errorf("CSP = %q, want no frame-ancestors with allowEmbed", embed.Get("Content-Security-Policy"))
	}

	if serve(false, true).Get("Strict-Transport-Security") == "" {
		t.Error("HSTS not set over TLS")
	}
}
