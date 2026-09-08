package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// flakyProviderStore answers Get with a transient failure while every other
// call passes through to the real repository, reproducing the read that fails
// between "provider absent" and "the row is there but we could not read it".
type flakyProviderStore struct {
	ProviderStore
	getErr error
}

func (f flakyProviderStore) Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.ProviderStore.Get(ctx, id)
}

// TestPriorProviderSurfacesReadFailure: a nil prior is what "no provider" looks
// like, so a failed read must not be flattened into it. Doing so lets an update
// commit and then skip the enable-state settlement, rediscovery and failover
// synchronisation the save owes.
func TestPriorProviderSurfacesReadFailure(t *testing.T) {
	h := newTestHandler(t)
	base := h.providerRepo
	enabled := true
	req := provider.UpdateProviderRequest{Enabled: &enabled}

	// A save that touches nothing whose before-and-after matters needs no read.
	prior, err := h.priorProvider(context.Background(), uuid.New(), provider.UpdateProviderRequest{})
	if prior != nil || err != nil {
		t.Errorf("a rename needs no prior: got (%v, %v)", prior, err)
	}

	// A genuine miss stays (nil, nil): the update answers that with its own 404.
	h.providerRepo = flakyProviderStore{ProviderStore: base, getErr: pgx.ErrNoRows}
	if prior, err = h.priorProvider(context.Background(), uuid.New(), req); prior != nil || err != nil {
		t.Errorf("a missing row should be (nil, nil): got (%v, %v)", prior, err)
	}

	// Anything else is returned.
	boom := errors.New("connection reset")
	h.providerRepo = flakyProviderStore{ProviderStore: base, getErr: boom}
	if _, err = h.priorProvider(context.Background(), uuid.New(), req); !errors.Is(err, boom) {
		t.Errorf("read failure err = %v, want %v", err, boom)
	}
}

// TestUpdateProviderAbortsOnPriorReadFailure: the handler refuses the write
// rather than committing one it cannot reconcile afterwards.
func TestUpdateProviderAbortsOnPriorReadFailure(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	h.providerRepo = flakyProviderStore{ProviderStore: h.providerRepo, getErr: errors.New("connection reset")}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/providers/"+uuid.NewString(), strings.NewReader(`{"enabled": true}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "before update") {
		t.Errorf("body = %q, want it to name the pre-update read", rec.Body.String())
	}
}
