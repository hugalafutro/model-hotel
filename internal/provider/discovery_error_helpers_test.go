package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// errorStatusHandler returns an httptest handler that always replies with the
// given HTTP status, used to drive discovery's non-2xx error paths.
func errorStatusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(status), status)
	}
}

// invalidJSONHandler returns an httptest handler that replies 200 with a
// truncated JSON body, used to drive discovery's decode-error paths.
func invalidJSONHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{ invalid json "))
	}
}

// assertDiscoverHTTPError points a discovery provider at a test server running
// handler, invokes discover (a thin wrapper around the provider-specific
// discoverX call), and asserts it returns a non-nil error. wantErr names the
// failure mode for the message. Discovery error tests across providers share
// this scaffold; only the handler and the discover call vary.
func assertDiscoverHTTPError(
	t *testing.T,
	wantErr string,
	handler http.HandlerFunc,
	discover func(svc *DiscoveryService, p *Provider) ([]*model.Model, error),
) {
	t.Helper()

	server := httptest.NewServer(handler)
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	p := &Provider{ID: uuid.New(), BaseURL: server.URL}

	if _, err := discover(svc, p); err == nil {
		t.Errorf("Expected error for %s, got nil", wantErr)
	}
}
