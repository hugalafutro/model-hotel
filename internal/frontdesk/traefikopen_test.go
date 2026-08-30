package frontdesk

import (
	"net/http"
	"testing"
)

const traefikOpenWarning = "frontdesk: /traefik/config is unauthenticated, set FRONTDESK_TRAEFIK_TOKEN to gate it"

// TestTraefikConfigOpenByDefault_IsAnnounced pins that the open default is said
// out loud once.
//
// The endpoint stays open when no token is set, which is deliberate, but serving
// it stamps the poll the Traefik-stalled watchdog measures silence against. An
// operator who never set the token has a watchdog anyone can keep quiet, and
// before this line nothing anywhere said so.
func TestTraefikConfigOpenByDefault_IsAnnounced(t *testing.T) {
	capt := captureLogs(t)
	newTestServer(t)

	if _, ok := capt.find(traefikOpenWarning); !ok {
		t.Error("a Front Desk with no Traefik token started without warning that the endpoint is open")
	}
}

// TestTraefikConfigGated_IsNotAnnounced is the other half: an operator who did
// set the token must not be nagged about a hole they closed.
func TestTraefikConfigGated_IsNotAnnounced(t *testing.T) {
	capt := captureLogs(t)
	newTestServerTraefikToken(t, "a-real-traefik-token")

	if _, ok := capt.find(traefikOpenWarning); ok {
		t.Error("warned about an open /traefik/config on a server whose token gates it")
	}
}

// TestTraefikConfigGated_RejectsAnUnauthenticatedPoll is why the warning is
// worth acting on: with the token set, a caller without it cannot reach the
// handler and so cannot stamp the watchdog.
func TestTraefikConfigGated_RejectsAnUnauthenticatedPoll(t *testing.T) {
	srv, _ := newTestServerTraefikToken(t, "a-real-traefik-token")

	if rec := do(t, srv, http.MethodGet, "/traefik/config", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated poll got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
