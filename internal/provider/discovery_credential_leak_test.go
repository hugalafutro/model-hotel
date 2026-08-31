package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Discovery decrypts the provider credential to talk to the upstream, and an
// upstream that quotes it back in an auth failure used to reach app_logs
// verbatim through these error paths. The error a discovery function returns
// is logged by its caller, so the credential must not be in it.
const leakedKey = "sk-discovery-1234567890abcdefghij"

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
