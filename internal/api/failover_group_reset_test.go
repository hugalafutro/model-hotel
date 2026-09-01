package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// ?model= scopes a provider reset to one circuit: the handler passes the model
// through and reports it, and the provider's other circuits stay.
func TestResetCircuitBreaker_ModelScoped(t *testing.T) {
	h := newTestHandler(t)
	pid := uuid.New()
	mockCB := &mockCircuitBreaker{statuses: []failover.ProviderStatus{{
		ProviderID: pid.String(), State: failover.StateOpen.String(), ProviderOpen: true, OpenModels: []string{"m1", "m2"},
	}}}
	h.SetCircuitBreaker(mockCB)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("POST", "/failover-groups/circuit-breaker/"+pid.String()+"/reset?model=m1", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var resp CircuitBreakerResetResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Model != "m1" || !resp.Reset || resp.PreviousState != "open" || resp.ProviderID != pid.String() {
		t.Errorf("response = %+v", resp)
	}
	if len(mockCB.modelResets) != 1 || mockCB.modelResets[0] != pid.String()+"/m1" {
		t.Errorf("model resets = %v, want exactly the scoped one", mockCB.modelResets)
	}
	if len(mockCB.statuses) != 1 || len(mockCB.statuses[0].OpenModels) != 1 || mockCB.statuses[0].OpenModels[0] != "m2" {
		t.Errorf("provider after a scoped reset = %+v, want m2 still open", mockCB.statuses)
	}
}

// The group reset clears exactly the group's (provider, model) circuits on a
// real breaker: an entry's open circuit recovers, a circuit of the same
// provider for a model outside the group survives, and a second reset finds
// nothing to clear.
func TestResetGroupCircuitBreakers(t *testing.T) {
	h := newIntegrationFailoverHandler()
	groupID, _ := enableGuardSeed(t, h, true, true)
	ctx := context.Background()
	g, err := h.failoverRepo.GetByID(ctx, groupID)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	models, err := h.modelRepo.GetByIDs(ctx, g.PriorityOrder)
	if err != nil || len(models) != 2 {
		t.Fatalf("models: %v (%d)", err, len(models))
	}
	first := models[g.PriorityOrder[0]]

	cb := failover.NewCircuitBreaker(nil)
	cb.Threshold = 1
	h.cb = cb
	cb.RecordFailure(first.ProviderID, "p", first.ModelID, failover.UpstreamStatus(503, ""))
	cb.RecordFailure(first.ProviderID, "p", "outside-the-group", failover.UpstreamStatus(503, ""))
	if cb.GetState(first.ProviderID, first.ModelID) != failover.StateOpen {
		t.Fatal("setup: entry circuit not open")
	}

	call := func() CircuitBreakerGroupResetResponse {
		t.Helper()
		req, w := newChiRequest(http.MethodPost, "/failover-groups/"+groupID.String()+"/circuit-breaker/reset", http.NoBody)
		req = setChiURLParam(req, "id", groupID.String())
		h.ResetGroupCircuitBreakers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var resp CircuitBreakerGroupResetResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	resp := call()
	if resp.GroupID != groupID.String() || resp.Entries != 2 || resp.Cleared != 1 || resp.Recovered != 1 {
		t.Errorf("first reset = %+v, want entries 2, cleared 1, recovered 1", resp)
	}
	if cb.GetState(first.ProviderID, first.ModelID) != failover.StateClosed {
		t.Error("the entry's circuit is still open after the group reset")
	}
	if cb.GetState(first.ProviderID, "outside-the-group") != failover.StateOpen {
		t.Error("a circuit outside the group was cleared by the group reset")
	}
	if again := call(); again.Cleared != 0 || again.Recovered != 0 {
		t.Errorf("second reset = %+v, want nothing left to clear", again)
	}

	// An unknown group is a 404, not a reset of nothing.
	req, w := newChiRequest(http.MethodPost, "/failover-groups/"+uuid.New().String()+"/circuit-breaker/reset", http.NoBody)
	req = setChiURLParam(req, "id", uuid.New().String())
	h.ResetGroupCircuitBreakers(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown group = %d, want 404", w.Code)
	}
}
