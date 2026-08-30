package proxy

import (
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The breaker keys a circuit by the resolved upstream model id, and the empty
// string candidateModelID falls back to is a real key: it earns a circuit, and
// that circuit counts toward the span of distinct models that indicts the
// provider. So a candidate reaching the breaker without a model does not merely
// lose a diagnostic, it charges a circuit nothing routes by, and the operator
// needs a trail saying which provider it happened on.
func TestCandidateModelIDLogsAModellessCandidate(t *testing.T) {
	capture := captureProxyLogs(t)
	prov := &provider.Provider{ID: uuid.New(), Name: "acme"}

	if got := candidateModelID(modelCandidate{provider: prov}); got != "" {
		t.Fatalf("candidateModelID = %q, want the empty fallback", got)
	}

	records := capture.find("candidate carries no model")
	if len(records) != 1 {
		t.Fatalf("got %d log records for a modelless candidate, want exactly 1", len(records))
	}
	rec := records[0]
	if rec.level != slog.LevelError {
		t.Errorf("logged at %v, want error: charging the wrong circuit is a defect, not routine noise", rec.level)
	}
	if rec.attrs["provider"] != "acme" {
		t.Errorf("provider attr = %q, want %q", rec.attrs["provider"], "acme")
	}
	if rec.attrs["provider_id"] != prov.ID.String() {
		t.Errorf("provider_id attr = %q, want %q", rec.attrs["provider_id"], prov.ID)
	}
}

// The ordinary path must stay silent: every routed candidate carries a model,
// and a line per attempt would bury the one that matters.
func TestCandidateModelIDIsSilentForAResolvedModel(t *testing.T) {
	capture := captureProxyLogs(t)
	candidate := modelCandidate{
		provider: &provider.Provider{ID: uuid.New(), Name: "acme"},
		model:    testModelNamed("gpt-4o-mini"),
	}

	if got := candidateModelID(candidate); got != "gpt-4o-mini" {
		t.Fatalf("candidateModelID = %q, want the candidate's upstream model id", got)
	}
	if records := capture.find("candidate carries no model"); len(records) != 0 {
		t.Errorf("got %d log records for a resolved model, want none", len(records))
	}
}
