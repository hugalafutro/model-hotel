package provider

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Discovery decrypts the provider credential to talk to the upstream, and an
// upstream that quotes it back in an auth failure used to reach app_logs
// verbatim through these error paths. The error a discovery function returns
// is logged by its caller, so the credential must not be in it.
// Deliberately shapeless: no known prefix, no digit, so MaskKeyShapedTokens
// cannot see it and only the exact-match layer can remove it. A self-hosted
// gateway's key looks like this, and it is the case the exact layer exists
// for. With an sk- key these tests passed even with MaskCredential removed,
// because the shape layer inside SanitizeLogBody caught it on its own.
const leakedKey = "selfhosted-gateway-secret"

func echoKeyHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + leakedKey + `"}}`))
	}
}

// The native endpoint answers an auth failure quoting the key; the returned
// error reaches the app log.
func TestDiscoverLMStudioNative_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioNative(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

// The OpenAI-compatible fallback listing does the same.
func TestDiscoverLMStudioOpenAI_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

func TestKoboldCPPLoadedModel_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

func assertScrubbed(t *testing.T, msg string) {
	t.Helper()
	if strings.Contains(msg, leakedKey) {
		t.Errorf("the provider key survived into the error: %q", msg)
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Errorf("no redaction marker, so the body was not scrubbed at all: %q", msg)
	}
}

// The shared helpers behind every provider family that lists models over a
// plain OpenAI-shaped endpoint. The vendor-specific paths above each learned
// to run MaskCredential over what the upstream said back; fetchURL and
// doDiscoveryRequest, which the openai/custom/azure/deepseek/... families all
// go through, still scrubbed with the shape layer only. A self-hosted gateway
// (provider type custom or openai, the arbitrary-endpoint fallback) whose error
// body quotes the bearer back therefore put the decrypted key into the returned
// error, and from there into app_logs, stdout and the discovery SSE event.
// Strix vuln-0005 (2026-09-01), the fourth site of the #836 class.
func TestDiscoverOpenAI_Non200DoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}

	_, err := svc.discoverOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, leakedKey)

	assertNoKey(t, err)
}

// A retryable status (429/5xx) is read into lastErr on every attempt and the
// final error wraps it, so the key rode out through a different string than the
// non-200 branch and needs its own assertion.
func TestFetchURL_RetryableStatusDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusServiceUnavailable))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()} // zero retryBaseDelay: instant backoffs
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/models", headers)

	assertNoKey(t, err)
}

// A transport error quotes the request URL, and one provider family (Google)
// authenticates by query parameter. The site logged that error at Info with the
// shape layer alone and returned it verbatim when it was not transient, so a
// custom-format key in ?key= reached the log line and the caller's error text.
func TestDoDiscoveryRequest_TransportErrorDoesNotCarryQueryKey(t *testing.T) {
	var logged strings.Builder
	prev := slog.Default()
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	defer slog.SetDefault(prev)

	// 127.0.0.1:1 refuses immediately on every platform CI runs on.
	svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
	_, err := svc.fetchURL(context.Background(), http.MethodGet, "http://127.0.0.1:1/v1beta/models?key="+leakedKey, http.Header{})

	assertNoKey(t, err)
	if strings.Contains(logged.String(), leakedKey) {
		t.Errorf("the query-parameter key reached the discovery log:\n%s", logged.String())
	}
}

func assertNoKey(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the upstream refusal")
	}
	if strings.Contains(err.Error(), leakedKey) {
		t.Errorf("error carries the decrypted key: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error should show the key was redacted, not silently dropped: %s", err.Error())
	}
}
