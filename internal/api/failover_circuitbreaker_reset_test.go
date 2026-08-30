package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newTestHandlerWithBreaker builds the admin router backed by a REAL
// *failover.CircuitBreaker rather than a stub, so the reset tests below assert
// against the breaker's own state machine (IsOpen / Status) instead of against
// a recording mock that could report a reset that never happened.
func newTestHandlerWithBreaker(t *testing.T) (*Handler, chi.Router, *failover.CircuitBreaker) {
	t.Helper()
	h := newTestHandler(t)
	cb := failover.NewCircuitBreaker(nil) // defaults: threshold 5, cooldown 60s
	h.SetCircuitBreaker(cb)

	r := chi.NewRouter()
	h.Register(r)
	h.SetDockerStatsCollector(func(util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	})
	return h, r, cb
}

// openCircuit drives a provider's circuit to open through the ordinary failure
// path, so the state under test is one the breaker really produces.
func openCircuit(t *testing.T, cb *failover.CircuitBreaker, providerID uuid.UUID) {
	t.Helper()
	for i := 0; i < cb.Threshold; i++ {
		cb.RecordFailure(providerID, "test-provider", "")
	}
	if !cb.IsOpen(providerID, "test-provider", "") {
		t.Fatalf("setup: circuit for %s should be open after %d failures", providerID, cb.Threshold)
	}
}

// doAdminRequest issues an authenticated admin request and returns status and body.
func doAdminRequest(r chi.Router, method, path string) (int, string) {
	req := httptest.NewRequest(method, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// cbDetailStates reads the circuit-breaker status endpoint the dashboard polls
// and returns provider_id -> state for every tracked provider.
func cbDetailStates(t *testing.T, r chi.Router) map[string]string {
	t.Helper()
	code, body := doAdminRequest(r, http.MethodGet, "/failover-groups/circuit-breaker-status?detail=1")
	if code != http.StatusOK {
		t.Fatalf("circuit-breaker-status: expected 200, got %d: %s", code, body)
	}
	var resp struct {
		Providers []failover.ProviderStatus `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("circuit-breaker-status: decode failed: %v (body %q)", err, body)
	}
	states := make(map[string]string, len(resp.Providers))
	for _, p := range resp.Providers {
		states[p.ProviderID] = p.State
	}
	return states
}

// TestResetCircuitBreaker_OpenCircuitReturnsProviderToRotation is the whole
// point of the endpoint: a provider the breaker has sidelined starts passing
// traffic again immediately, without waiting out the cooldown. The status
// endpoint is read BEFORE the reset as well, so this also proves the reset
// invalidates the 5s status cache: a stale cached snapshot would still report
// the provider as open afterwards.
func TestResetCircuitBreaker_OpenCircuitReturnsProviderToRotation(t *testing.T) {
	_, r, cb := newTestHandlerWithBreaker(t)
	providerID := uuid.New()
	openCircuit(t, cb, providerID)

	if got := cbDetailStates(t, r)[providerID.String()]; got != "open" {
		t.Fatalf("before reset: expected status endpoint to report open, got %q", got)
	}

	code, body := doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/"+providerID.String()+"/reset")
	if code != http.StatusOK {
		t.Fatalf("reset: expected 200, got %d: %s", code, body)
	}

	var resp CircuitBreakerResetResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("reset: decode failed: %v (body %q)", err, body)
	}
	if resp.ProviderID != providerID.String() {
		t.Errorf("reset: provider_id = %q, want %q", resp.ProviderID, providerID)
	}
	if resp.PreviousState != "open" {
		t.Errorf("reset: previous_state = %q, want \"open\"", resp.PreviousState)
	}
	if !resp.Reset {
		t.Error("reset: expected reset=true for a circuit that was sidelining its provider")
	}

	// The breaker itself now routes to the provider again.
	if cb.IsOpen(providerID, "test-provider", "") {
		t.Error("after reset: breaker still blocks the provider")
	}
	if got := cb.GetState(providerID, ""); got != failover.StateClosed {
		t.Errorf("after reset: breaker state = %v, want closed", got)
	}
	// And the dashboard's own feed agrees, rather than serving the cached
	// pre-reset snapshot.
	if got, tracked := cbDetailStates(t, r)[providerID.String()]; tracked {
		t.Errorf("after reset: status endpoint still tracks the provider as %q", got)
	}
}

// TestResetCircuitBreaker_ClosedCircuitReportsNoChange covers the honest-no-op
// case: the provider was never being blocked, so the response must not claim a
// recovery. Both a tracked-but-closed circuit and a provider the breaker has
// never seen are the same healthy state and must answer identically.
func TestResetCircuitBreaker_ClosedCircuitReportsNoChange(t *testing.T) {
	_, r, cb := newTestHandlerWithBreaker(t)

	tracked := uuid.New()
	cb.RecordFailure(tracked, "test-provider", "") // one failure: circuit exists, still closed
	untracked := uuid.New()                        // never routed, so no circuit at all

	for name, providerID := range map[string]uuid.UUID{"tracked but closed": tracked, "never tracked": untracked} {
		t.Run(name, func(t *testing.T) {
			if cb.IsOpen(providerID, "test-provider", "") {
				t.Fatalf("setup: %s circuit must not be blocking before the reset", name)
			}

			code, body := doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/"+providerID.String()+"/reset")
			if code != http.StatusOK {
				t.Fatalf("reset: expected 200, got %d: %s", code, body)
			}
			var resp CircuitBreakerResetResponse
			if err := json.Unmarshal([]byte(body), &resp); err != nil {
				t.Fatalf("reset: decode failed: %v (body %q)", err, body)
			}
			if resp.PreviousState != "closed" {
				t.Errorf("reset: previous_state = %q, want \"closed\"", resp.PreviousState)
			}
			if resp.Reset {
				t.Error("reset: expected reset=false; nothing was sidelining this provider")
			}
			if cb.IsOpen(providerID, "test-provider", "") {
				t.Error("reset: a no-op reset must not start blocking the provider")
			}
		})
	}
}

// TestResetCircuitBreaker_InvalidProviderID rejects a non-UUID path segment
// before touching breaker state, so a typo cannot be reported as a successful
// reset of nothing.
func TestResetCircuitBreaker_InvalidProviderID(t *testing.T) {
	_, r, cb := newTestHandlerWithBreaker(t)
	providerID := uuid.New()
	openCircuit(t, cb, providerID)

	code, body := doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/not-a-uuid/reset")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-UUID provider id, got %d: %s", code, body)
	}
	// The real circuit is untouched: a rejected request resets nothing.
	if !cb.IsOpen(providerID, "test-provider", "") {
		t.Error("a rejected reset must leave existing circuits open")
	}
}

// TestResetCircuitBreaker_ManagedMemberNotBlocked pins the deliberate asymmetry
// with the neighbouring failover-group writes: a circuit is local runtime
// health, not synced config, so managedWriteGuard must not cover the reset. The
// guard is proven live in the same harness by a synced-entity write that IS
// refused, otherwise this test would pass on a router where the guard simply
// was not mounted.
func TestResetCircuitBreaker_ManagedMemberNotBlocked(t *testing.T) {
	h, r, cb := newTestHandlerWithBreaker(t)
	ctx := context.Background()

	// Enroll this instance as a fresh, non-primary fleet member.
	if err := h.settingsRepo.Set(ctx, keyFleetManagedSeenAt, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := h.settingsRepo.Set(ctx, keyFleetIsPrimary, "false"); err != nil {
		t.Fatal(err)
	}

	// Control: a synced-entity write on the same router is refused, so the guard
	// is definitely active here.
	req := httptest.NewRequest(http.MethodPost, "/failover-groups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("setup: managed member should be refused POST /failover-groups (403), got %d", w.Code)
	}

	providerID := uuid.New()
	openCircuit(t, cb, providerID)

	code, body := doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/"+providerID.String()+"/reset")
	if code != http.StatusOK {
		t.Fatalf("managed member reset: expected 200, got %d: %s", code, body)
	}
	if cb.IsOpen(providerID, "test-provider", "") {
		t.Error("managed member reset: breaker still blocks the provider")
	}

	// The bulk reset is exempt for the same reason.
	openCircuit(t, cb, providerID)
	code, body = doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/reset")
	if code != http.StatusOK {
		t.Fatalf("managed member reset-all: expected 200, got %d: %s", code, body)
	}
	if cb.IsOpen(providerID, "test-provider", "") {
		t.Error("managed member reset-all: breaker still blocks the provider")
	}
}

// TestResetAllCircuitBreakers_ClearsEveryCircuit checks the bulk lever returns
// every sidelined provider at once and counts honestly: cleared covers every
// discarded circuit, recovered only the ones that were actually blocking.
func TestResetAllCircuitBreakers_ClearsEveryCircuit(t *testing.T) {
	_, r, cb := newTestHandlerWithBreaker(t)

	openA, openB, healthy := uuid.New(), uuid.New(), uuid.New()
	openCircuit(t, cb, openA)
	openCircuit(t, cb, openB)
	cb.RecordFailure(healthy, "test-provider", "") // tracked, below threshold, still closed

	code, body := doAdminRequest(r, http.MethodPost, "/failover-groups/circuit-breaker/reset")
	if code != http.StatusOK {
		t.Fatalf("reset-all: expected 200, got %d: %s", code, body)
	}
	var resp CircuitBreakerResetAllResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("reset-all: decode failed: %v (body %q)", err, body)
	}
	if resp.Cleared != 3 {
		t.Errorf("reset-all: cleared = %d, want 3 (two open plus one closed circuit)", resp.Cleared)
	}
	if resp.Recovered != 2 {
		t.Errorf("reset-all: recovered = %d, want 2 (only the circuits that were blocking)", resp.Recovered)
	}

	for _, id := range []uuid.UUID{openA, openB, healthy} {
		if cb.IsOpen(id, "test-provider", "") {
			t.Errorf("reset-all: breaker still blocks %s", id)
		}
	}
	if states := cbDetailStates(t, r); len(states) != 0 {
		t.Errorf("reset-all: status endpoint still tracks circuits: %v", states)
	}
}

// TestResetCircuitBreaker_NoBreakerWired reports the reset lever as unavailable
// instead of pretending it succeeded when no breaker is wired to the handler.
func TestResetCircuitBreaker_NoBreakerWired(t *testing.T) {
	_, r := newTestHandlerWithRouter(t) // no SetCircuitBreaker call

	for _, path := range []string{
		"/failover-groups/circuit-breaker/" + uuid.New().String() + "/reset",
		"/failover-groups/circuit-breaker/reset",
	} {
		code, body := doAdminRequest(r, http.MethodPost, path)
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503 with no breaker wired, got %d: %s", path, code, body)
		}
	}
}
