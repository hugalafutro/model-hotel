package provider

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Go's *url.Error renders the whole request URL in Error(), redacting only
// userinfo passwords. Pins the class end to end: the key travels in a header,
// and the shared retry path masks whatever the transport error quotes.
//
// A shapeless key, so the shape layer cannot hide a regression.
func TestDiscoverGoogle_TransportErrorDoesNotCarryTheKey(t *testing.T) {
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

// The non-200 path logs the upstream body; the echoed key must be redacted in
// that log line.
func TestDiscoverGoogle_ErrorBodyDoesNotCarryTheKey(t *testing.T) {
	var logged strings.Builder
	prev := slog.Default()
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	defer slog.SetDefault(prev)

	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverGoogleAIStudio(context.Background(), &Provider{ID: uuid.New(), Name: "google-leak", BaseURL: srv.URL}, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	if strings.Contains(err.Error(), leakedKey) {
		t.Errorf("the API key survived into the error: %q", err.Error())
	}
	assertScrubbed(t, logged.String())
}
