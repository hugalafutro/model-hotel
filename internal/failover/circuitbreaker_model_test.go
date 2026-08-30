package failover

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// Circuits are keyed (provider, resolved upstream model) and the provider-wide
// verdict is derived from them. These tests pin that split, so the ones that
// turn on a failure credit run at a threshold above 1 (a threshold of 1 opens on
// the first failure and hides an erased credit) and the ones that turn on the
// derivation run at the default span of 2 (a span of 1 hides the derivation
// entirely: the first open model would be the whole provider).

const (
	modelA = "model-a"
	modelB = "model-b"
	// modelC is never charged in these tests: it is the probe that reveals
	// whether the provider-wide predicate, not a model circuit, is doing the
	// skipping.
	modelC = "model-c"
)

// chargeToOpen drives one model circuit to Open with real failures.
func chargeToOpen(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string) {
	t.Helper()
	for i := 0; i < cb.effectiveThreshold(); i++ {
		cb.RecordFailure(id, "test-provider", model)
	}
	if got := cb.GetState(id, model); got != StateOpen {
		t.Fatalf("setup: model %q state %v, want open", model, got)
	}
}

func TestModelCircuits_OpenModelDoesNotSkipItsSibling(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)

	if !cb.IsOpen(id, "test-provider", modelA) {
		t.Error("model A circuit is open, want IsOpen(A) true")
	}
	if cb.IsOpen(id, "test-provider", modelB) {
		t.Error("model B never failed, want IsOpen(B) false")
	}
	if got := cb.GetState(id, modelB); got != StateClosed {
		t.Errorf("model B state %v, want closed", got)
	}
}

func TestModelCircuits_ProviderOpensOnlyWhenSpanModelsAreOpen(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	if cb.IsOpen(id, "test-provider", modelC) {
		t.Error("one open model with span 2, want the provider to stay usable for C")
	}

	chargeToOpen(t, cb, id, modelB)
	if !cb.IsOpen(id, "test-provider", modelC) {
		t.Error("two open models reach span 2, want the provider skipped for C")
	}
}

func TestModelCircuits_SpanSettingOverridesTheDefault(t *testing.T) {
	tests := []struct {
		name     string
		span     int
		wantOpen bool
	}{
		{name: "span 1 opens the provider on the first open model", span: 1, wantOpen: true},
		{name: "span 3 keeps the provider usable at two open models", span: 3, wantOpen: false},
		{name: "non-positive span falls back to the default of 2", span: -4, wantOpen: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker(&stubSettings{threshold: 2, cooldown: time.Hour, span: tc.span})
			cb.HalfOpenMaxProbes = 1
			id := uuid.New()

			chargeToOpen(t, cb, id, modelA)
			if tc.span != 1 {
				chargeToOpen(t, cb, id, modelB)
			}

			if got := cb.IsOpen(id, "test-provider", modelC); got != tc.wantOpen {
				t.Errorf("IsOpen(C) = %v, want %v", got, tc.wantOpen)
			}
		})
	}
}

func TestModelCircuits_ProviderClosesAgainWhenAnOpenCircuitIsOwedAProbe(t *testing.T) {
	cb := newTestCB(2, 40*time.Millisecond)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	chargeToOpen(t, cb, id, modelB)
	if !cb.IsOpen(id, "test-provider", modelC) {
		t.Fatal("setup: two open models reach span 2, want the provider skipped")
	}

	time.Sleep(60 * time.Millisecond)

	if cb.IsOpen(id, "test-provider", modelC) {
		t.Error("both cooldowns elapsed so no circuit is open, want the provider usable again")
	}
}

func TestModelCircuits_SuccessOnOneModelKeepsTheOtherStreak(t *testing.T) {
	cb := newTestCB(3, time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider", modelA)
	cb.RecordFailure(id, "test-provider", modelA)
	cb.RecordSuccess(id, "test-provider", modelB)
	cb.RecordFailure(id, "test-provider", modelA)

	if got := cb.GetState(id, modelA); got != StateOpen {
		t.Errorf("model A took 3 failures at threshold 3, state %v, want open", got)
	}
	if got := cb.GetState(id, modelB); got != StateClosed {
		t.Errorf("model B only ever succeeded, state %v, want closed", got)
	}
}

func TestModelCircuits_FailureOnOneModelKeepsTheOtherStreak(t *testing.T) {
	cb := newTestCB(3, time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider", modelA)
	cb.RecordFailure(id, "test-provider", modelB)
	cb.RecordFailure(id, "test-provider", modelB)

	if got := cb.GetState(id, modelA); got != StateClosed {
		t.Errorf("model A took 1 of 3 failures, state %v, want closed", got)
	}
	if got := cb.GetState(id, modelB); got != StateClosed {
		t.Errorf("model B took 2 of 3 failures, state %v, want closed", got)
	}
	cb.RecordFailure(id, "test-provider", modelB)
	if got := cb.GetState(id, modelB); got != StateOpen {
		t.Errorf("model B took 3 of 3 failures, state %v, want open", got)
	}
	if got := cb.GetState(id, modelA); got != StateClosed {
		t.Errorf("model A still has 1 failure of its own, state %v, want closed", got)
	}
}

func TestModelCircuits_HalfOpenProbeIsPerModel(t *testing.T) {
	cb := newTestCB(2, 40*time.Millisecond)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	chargeToOpen(t, cb, id, modelB)
	time.Sleep(60 * time.Millisecond)

	// The elapsed cooldown lets a probe through for A only; B stays a separate
	// circuit with its own probe to spend.
	if cb.IsOpen(id, "test-provider", modelA) {
		t.Fatal("model A cooldown elapsed, want the probe allowed")
	}
	if got := cb.GetState(id, modelA); got != StateHalfOpen {
		t.Fatalf("model A state %v, want half-open", got)
	}

	cb.RecordFailure(id, "test-provider", modelA)
	if got := cb.GetState(id, modelA); got != StateOpen {
		t.Errorf("model A probe failed, state %v, want open", got)
	}

	if cb.IsOpen(id, "test-provider", modelB) {
		t.Error("model B cooldown elapsed too, want its own probe allowed")
	}
	cb.RecordSuccess(id, "test-provider", modelB)
	if got := cb.GetState(id, modelB); got != StateClosed {
		t.Errorf("model B probe succeeded, state %v, want closed", got)
	}
	if got := cb.GetState(id, modelA); got != StateOpen {
		t.Errorf("model B recovering must not close model A, state %v, want open", got)
	}
}

func TestModelCircuits_QuotaPinOpensTheProviderBelowSpan(t *testing.T) {
	tests := []struct {
		name     string
		advisor  QuotaAdvisor
		wantOpen bool
	}{
		{name: "no quota advice leaves the span rule in charge", advisor: nil, wantOpen: false},
		{
			name:     "a quota pin opens the provider on its own",
			advisor:  stubAdvisor{at: time.Now().Add(5 * time.Hour), ok: true},
			wantOpen: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cb := newTestCB(2, time.Hour)
			if tc.advisor != nil {
				cb.SetQuotaAdvisor(tc.advisor)
			}
			id := uuid.New()

			chargeToOpen(t, cb, id, modelA)

			if got := cb.IsOpen(id, "test-provider", modelC); got != tc.wantOpen {
				t.Errorf("one open model at span 2: IsOpen(C) = %v, want %v", got, tc.wantOpen)
			}
		})
	}
}

func TestModelCircuits_ReleasingTheQuotaPinReDerivesTheProvider(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(5 * time.Hour), ok: true})
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	if !cb.IsOpen(id, "test-provider", modelC) {
		t.Fatal("setup: quota pin should open the provider")
	}

	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); released != 1 {
		t.Fatalf("released %d pins, want 1", released)
	}

	if cb.IsOpen(id, "test-provider", modelC) {
		t.Error("pin lifted and only one model open at span 2, want the provider usable")
	}
	if !cb.IsOpen(id, "test-provider", modelA) {
		t.Error("lifting a pin must not close the model circuit that carried it")
	}
}

func TestModelCircuits_EvictionNeverDropsAnOpenCircuit(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)

	// One failure each, well below the threshold: every one of these circuits is
	// closed and therefore evictable, while model A stays open and must not be.
	const churn = maxModelCircuitsPerProvider + 50
	for i := 0; i < churn; i++ {
		cb.RecordFailure(id, "test-provider", fmt.Sprintf("churn-%03d", i))
	}

	if got := cb.GetState(id, modelA); got != StateOpen {
		t.Errorf("model A state %v after %d other models were charged, want open", got, churn)
	}
	if got := countCircuits(t, cb, id); got != maxModelCircuitsPerProvider {
		t.Errorf("tracking %d circuits, want the cap of %d", got, maxModelCircuitsPerProvider)
	}
	// The most recently charged circuit is the one eviction must keep.
	newest := fmt.Sprintf("churn-%03d", churn-1)
	if !hasCircuit(t, cb, id, newest) {
		t.Errorf("circuit for the most recently charged model %q was evicted", newest)
	}
}

func TestModelCircuits_EvictionDropsTheLeastRecentlyChargedCircuit(t *testing.T) {
	cb := newTestCB(5, time.Hour)
	id := uuid.New()

	// Fill the provider to the cap with closed circuits, one failure each.
	for i := 0; i < maxModelCircuitsPerProvider; i++ {
		cb.RecordFailure(id, "test-provider", fmt.Sprintf("m%03d", i))
	}
	// Re-charge the oldest one, which makes the second-oldest the least recently
	// charged. Ordering by charge time and ordering by creation now disagree,
	// which is the whole point of the assertion below.
	cb.RecordFailure(id, "test-provider", "m000")

	cb.RecordFailure(id, "test-provider", "newcomer")

	if !hasCircuit(t, cb, id, "m000") {
		t.Error("m000 was charged most recently of the filled set, want it kept")
	}
	if hasCircuit(t, cb, id, "m001") {
		t.Error("m001 was the least recently charged circuit, want it evicted")
	}
	if !hasCircuit(t, cb, id, "newcomer") {
		t.Error("the circuit eviction made room for is missing")
	}
	if got := countCircuits(t, cb, id); got != maxModelCircuitsPerProvider {
		t.Errorf("tracking %d circuits, want the cap of %d", got, maxModelCircuitsPerProvider)
	}
}

// The empty model id is a real key (a caller that has no resolved id in scope
// uses it), so it has to be as evictable as any other or one provider's map
// grows past the cap around it.
func TestModelCircuits_EvictionDropsTheEmptyModelIDLikeAnyOther(t *testing.T) {
	cb := newTestCB(5, time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider", "") // charged first, so the oldest
	for i := 1; i < maxModelCircuitsPerProvider; i++ {
		cb.RecordFailure(id, "test-provider", fmt.Sprintf("m%03d", i))
	}

	cb.RecordFailure(id, "test-provider", "newcomer")

	if hasCircuit(t, cb, id, "") {
		t.Error("the empty model id was the least recently charged circuit, want it evicted")
	}
	if got := countCircuits(t, cb, id); got != maxModelCircuitsPerProvider {
		t.Errorf("tracking %d circuits, want the cap of %d", got, maxModelCircuitsPerProvider)
	}
}

func TestModelCircuits_EvictionKeepsGrowingWhenEveryCircuitIsOpen(t *testing.T) {
	cb := newTestCB(1, time.Hour)
	id := uuid.New()

	const models = maxModelCircuitsPerProvider + 3
	for i := 0; i < models; i++ {
		cb.RecordFailure(id, "test-provider", fmt.Sprintf("open-%03d", i))
	}

	if got := countCircuits(t, cb, id); got != models {
		t.Errorf("tracking %d circuits, want %d: an open circuit is never evicted", got, models)
	}
}

func TestModelCircuits_StatusReportsTheMostDegradedCircuit(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider", modelB) // one failure, stays closed
	chargeToOpen(t, cb, id, modelA)

	s := onlyStatus(t, cb)
	if s.State != StateOpen.String() {
		t.Errorf("status state %q, want %q", s.State, StateOpen.String())
	}
	if s.ConsecutiveFails != 2 {
		t.Errorf("status consecutive_fails %d, want 2 (model A's streak)", s.ConsecutiveFails)
	}
	if s.OpenedAt == "" || s.NextRetryAt == "" {
		t.Errorf("status omits the open-circuit timing: %+v", s)
	}
}

// A zeroed SpanModels is a caller that never set the field, and it must not
// mean "the provider is down with nothing open": the floor of 1 is the lowest
// meaningful span, not a licence to skip a provider on no evidence at all.
func TestModelCircuits_SpanFloorSurvivesAZeroedField(t *testing.T) {
	cb := newTestCB(2, 40*time.Millisecond)
	cb.SpanModels = 0
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	time.Sleep(60 * time.Millisecond)

	if cb.IsOpen(id, "test-provider", modelC) {
		t.Error("no circuit is open any more, want the provider usable whatever the span field says")
	}
}

func TestModelCircuits_StatusReportsTheWorstStreakAmongClosedCircuits(t *testing.T) {
	cb := newTestCB(5, time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider", modelA)
	for i := 0; i < 3; i++ {
		cb.RecordFailure(id, "test-provider", modelB)
	}

	s := onlyStatus(t, cb)
	if s.State != StateClosed.String() {
		t.Errorf("status state %q, want %q: neither circuit reached the threshold", s.State, StateClosed.String())
	}
	if s.ConsecutiveFails != 3 {
		t.Errorf("status consecutive_fails %d, want 3 (the longest streak the provider carries)", s.ConsecutiveFails)
	}
}

// Two open circuits with different retry instants: the provider row must report
// the one that keeps it dark the longest, or an operator reads a next_retry_at
// that passes while the provider is still being skipped.
func TestModelCircuits_StatusReportsTheDarkestOpenCircuit(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(5 * time.Hour), ok: true})
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA) // opens against live quota advice: pinned
	cb.SetQuotaAdvisor(stubAdvisor{ok: false})
	chargeToOpen(t, cb, id, modelB) // opens with the ordinary cooldown

	s := onlyStatus(t, cb)
	if !s.QuotaPinned {
		t.Errorf("status quota_pinned=false, want the pinned circuit to represent the provider: %+v", s)
	}
	if s.CooldownMs <= cb.Cooldown.Milliseconds() {
		t.Errorf("status cooldown_ms %d, want the pinned circuit's cooldown (over %d)", s.CooldownMs, cb.Cooldown.Milliseconds())
	}
}

func TestModelCircuits_EventAndLogCarryTheModel(t *testing.T) {
	capt := captureLogs(t)
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(2, time.Hour)
	id := uuid.New()
	chargeToOpen(t, cb, id, modelA)

	ev := waitForOpenEvent(t, sub, id)
	if got, _ := ev.Metadata["model"].(string); got != modelA {
		t.Errorf("open event model metadata %q, want %q", got, modelA)
	}

	_, attrs, ok := capt.last("circuit-breaker: model state=closed→open")
	if !ok {
		t.Fatal("no open-transition log line captured")
	}
	if got, _ := attrs["model"].(string); got != modelA {
		t.Errorf("open-transition log model %q, want %q", got, modelA)
	}
}

// The provider row carries the derived verdict and the evidence behind it. Both
// halves matter: at the default span of 2 one open model leaves the provider
// usable, so a row that only reported the dominant circuit's state would show
// "open" for a provider that is still serving every other model.
func TestModelCircuits_StatusReportsTheDerivedProviderVerdict(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)

	s := onlyStatus(t, cb)
	if s.ProviderOpen {
		t.Error("one open model at span 2: provider_open true, want the provider still usable")
	}
	if !slices.Equal(s.OpenModels, []string{modelA}) {
		t.Errorf("open_models = %v, want [%s]", s.OpenModels, modelA)
	}

	chargeToOpen(t, cb, id, modelB)

	s = onlyStatus(t, cb)
	if !s.ProviderOpen {
		t.Error("two open models at span 2: provider_open false, want the provider indicted")
	}
	if !slices.Equal(s.OpenModels, []string{modelA, modelB}) {
		t.Errorf("open_models = %v, want both open models", s.OpenModels)
	}
}

// The list is what a provider detail prints, and it is refetched every few
// seconds, so its order must not depend on Go's randomized map iteration. The
// models are opened in reverse order, so insertion order cannot pass for sorted.
func TestModelCircuits_StatusListsOpenModelsInAStableOrder(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	want := []string{"model-a", "model-b", "model-c", "model-d"}
	for i := len(want) - 1; i >= 0; i-- {
		chargeToOpen(t, cb, id, want[i])
	}

	if got := onlyStatus(t, cb).OpenModels; !slices.Equal(got, want) {
		t.Errorf("open_models = %v, want %v in sorted order", got, want)
	}
}

// A circuit whose cooldown has elapsed is owed a probe and blocks nothing, so it
// counts for neither the verdict nor the list. Reporting it would tell an
// operator a model is sidelined at the very moment the breaker has handed it
// back to the request path.
func TestModelCircuits_StatusDropsAnOpenCircuitOwedAProbe(t *testing.T) {
	cb := newTestCB(2, 40*time.Millisecond)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	chargeToOpen(t, cb, id, modelB)

	s := onlyStatus(t, cb)
	if !s.ProviderOpen || len(s.OpenModels) != 2 {
		t.Fatalf("setup: provider_open=%v open_models=%v, want an indicted provider with both models dark", s.ProviderOpen, s.OpenModels)
	}

	time.Sleep(60 * time.Millisecond)

	s = onlyStatus(t, cb)
	if s.ProviderOpen {
		t.Error("both cooldowns elapsed: provider_open true, want the provider owed its probes")
	}
	if len(s.OpenModels) != 0 {
		t.Errorf("open_models = %v, want empty: neither circuit is blocking any more", s.OpenModels)
	}
}

// A quota pin indicts the provider on its own, below the span, and the row must
// say so: the operator's explanation for a provider skipped on one open model is
// quota_pinned beside provider_open, not a second open model that never appears.
func TestModelCircuits_StatusProviderOpenFollowsTheQuotaPin(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(5 * time.Hour), ok: true})
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)

	s := onlyStatus(t, cb)
	if !s.ProviderOpen {
		t.Error("quota-pinned circuit: provider_open false, want the pin to indict the provider below span")
	}
	if !slices.Equal(s.OpenModels, []string{modelA}) {
		t.Errorf("open_models = %v, want [%s]: the pin does not invent a second open model", s.OpenModels, modelA)
	}
}

// The transition event has to carry the derived verdict, or a consumer watching
// the stream has to re-derive it from the span setting it cannot see. The same
// event type fires for the first model (provider still usable) and the second
// (provider indicted), so the flag is the only thing that tells them apart.
func TestModelCircuits_EventCarriesTheDerivedProviderVerdict(t *testing.T) {
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(2, time.Hour)
	id := uuid.New()

	chargeToOpen(t, cb, id, modelA)
	ev := waitForOpenEvent(t, sub, id)
	if open, _ := ev.Metadata["provider_open"].(bool); open {
		t.Error("first model open at span 2: event provider_open true, want false")
	}

	chargeToOpen(t, cb, id, modelB)
	ev = waitForOpenEvent(t, sub, id)
	if open, _ := ev.Metadata["provider_open"].(bool); !open {
		t.Error("second model open at span 2: event provider_open false, want true")
	}
}

// ResetAll's two counts are about CIRCUITS, which is what the map now holds two
// levels of. Counting the top-level provider entries reported "cleared 1" for a
// provider whose five models had all been charged, and the API hands that number
// to the operator verbatim as the size of what the bulk lever just threw away.
//
// Three circuits over two providers, only two of them open, so provider-counting
// (2, 2), circuit-counting (3, 2) and "every circuit was blocking" (3, 3) are all
// distinguishable.
func TestModelCircuits_ResetAllCountsCircuitsNotProviders(t *testing.T) {
	cb := newTestCB(2, time.Hour)
	busy, other := uuid.New(), uuid.New()

	chargeToOpen(t, cb, busy, modelA)
	chargeToOpen(t, cb, busy, modelB)
	cb.RecordFailure(other, "test-provider", modelA) // tracked, below threshold, closed

	cleared, recovered := cb.ResetAll()
	if cleared != 3 {
		t.Errorf("cleared = %d, want 3 (two model circuits on one provider plus one on the other)", cleared)
	}
	if recovered != 2 {
		t.Errorf("recovered = %d, want 2 (both open model circuits, not the closed one)", recovered)
	}
	if cb.IsOpen(busy, "test-provider", modelA) || cb.IsOpen(busy, "test-provider", modelB) {
		t.Error("ResetAll left a circuit open")
	}
}
