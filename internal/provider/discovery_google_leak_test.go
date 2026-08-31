package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Google discovery authenticates by query parameter, so the credential is in
// the request URL. Go's *url.Error renders the whole URL in Error() — it
// redacts userinfo passwords, nothing else — so any transport failure (DNS,
// TLS, timeout, a SafeDialer refusal) produced an error string carrying the
// key, and that string reached the app log, the discovery HTTP response and
// the discovery.provider_failed SSE event.
//
// A real Google key is AIza-prefixed, which the shape layer catches on its
// own; this uses a shapeless key so the assertion pins the exact-match layer
// at the call site rather than the shared scrub.
func TestDiscoverGoogle_TransportErrorDoesNotCarryTheKeyFromTheURL(t *testing.T) {
	const key = "selfhosted-gateway-secret"

	// A closed listener: the request is guaranteed to fail at the transport
	// layer, which is exactly the path that renders the URL into the error.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()

	svc := &DiscoveryService{httpClient: &http.Client{}}
	_, err := svc.discoverGoogleAIStudio(context.Background(), &Provider{ID: uuid.New(), Name: "google-leak", BaseURL: base}, key)
	if err == nil {
		t.Fatal("expected a transport error against a closed listener")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the API key survived from the URL into the error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("the URL-borne credential was not scrubbed at all: %q", err.Error())
	}
}

// The same body-echo shape as the other providers, on the non-200 path.
func TestDiscoverGoogle_ErrorBodyDoesNotCarryTheKey(t *testing.T) {
	const key = "selfhosted-gateway-secret"
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverGoogleAIStudio(context.Background(), &Provider{ID: uuid.New(), Name: "google-leak", BaseURL: srv.URL}, key)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the API key survived into the error: %q", err.Error())
	}
}
