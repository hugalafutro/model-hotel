package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Go's *url.Error renders the whole request URL in Error(), redacting only
// userinfo passwords, so a key in the query string would reach the app log,
// the discovery HTTP response and the discovery.provider_failed SSE event on
// any transport failure. The key travels in a header; this pins that.
//
// A shapeless key, so the shape layer cannot hide a regression.
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
		t.Errorf("the API key reached the error text: %q", err.Error())
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
