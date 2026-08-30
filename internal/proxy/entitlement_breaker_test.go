package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// zaiEntitlementBody is the real refusal Z.ai's coding-plan endpoint returns for
// a model outside the subscription. Note it names BOTH the account-wide
// condition and the per-model one in a single sentence, which is why the phrases
// alone cannot separate them.
const zaiEntitlementBody = `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`

// runEntitlementAttempt drives one attempt against an upstream answering status
// with body, and reports whether the provider's circuit ended up open.
func runEntitlementAttempt(t *testing.T, h *Handler, cand modelCandidate, totalCandidates int) {
	t.Helper()
	st := &requestState{
		startTime: time.Now(), reqModel: "plan-model",
		bodyBytes:             []byte(`{"model":"plan-model","messages":[{"role":"user","content":"hi"}]}`),
		failoverTimeout:       30 * time.Second,
		circuitBreakerEnabled: true,
		vkHash:                "test-hash",
		logData: &requestLogData{
			id: uuid.New().String(), modelID: "plan-model",
			providerID: cand.provider.ID, providerName: "Z.ai Coding Plan",
			endpointType: endpointTypeChat, state: "pending",
			virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
		},
	}
	h.insertRequestLogAsync(st.logData)
	h.attemptCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
		st, cand, 0, totalCandidates)
}

func entitlementHandler(t *testing.T, status int, body string) (*Handler, modelCandidate) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThreshold(t, h, "2")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "plan-model"}
	return h, goneCandidateAt(m, "Z.ai Coding Plan", "http://api.z.ai")
}

// A plan that does not cover ONE model says nothing about the provider's health,
// and one such model must not take the whole provider out of rotation.
//
// Z.ai answers 429 for a model outside the coding plan while other models on the
// same provider and the same key answer 200 — verified against production:
// glm-5.1 returns 200 while glm-4.7-flashx and glm-4.5-x return 429.
//
// The 429 is charged, and the circuit KEY is what confines it: the refused model
// goes dark, and ONE open model is below the default span of 2, so the provider
// stays usable for everything else. That is why the sibling probe below is the
// assertion and the model's own state is only its precondition. Two refused
// models is a different case with a different answer, pinned by
// TestAttemptCandidate_TwoPlanRefusalsReachTheProviderVerdict.
//
// Both routes through attemptCandidate are covered: the last candidate (which
// classifies inside forwardUpstreamError) and a failover-eligible one with a
// candidate to fall back to (which classifies on the drain path).
func TestAttemptCandidate_AModelOutsideThePlanDoesNotDarkenTheProvider(t *testing.T) {
	for _, tc := range []struct {
		name            string
		totalCandidates int
	}{
		{"last candidate", 1},
		{"with a candidate to fall back to", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cand := entitlementHandler(t, http.StatusTooManyRequests, zaiEntitlementBody)
			runEntitlementAttempt(t, h, cand, tc.totalCandidates)
			runEntitlementAttempt(t, h, cand, tc.totalCandidates)

			if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
				t.Errorf("the refused model's own circuit is %v, want open: a 429 charges like any other failure", got)
			}
			if h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "glm-5.1") {
				t.Error("a model outside the plan darkened a sibling: the models the plan DOES cover are now skipped")
			}
		})
	}
}

// withBreakerSpan sets circuit_breaker_span_models for one test. The span is
// read at IsOpen time rather than at charge time, so raising it re-derives the
// verdict over circuits that are already open.
func withBreakerSpan(t *testing.T, h *Handler, span string) {
	t.Helper()
	if err := h.settingsRepo.Set(context.Background(), "circuit_breaker_span_models", span); err != nil {
		t.Fatalf("set circuit_breaker_span_models: %v", err)
	}
	h.settingsRepo.InvalidateCache("circuit_breaker_span_models")
	t.Cleanup(func() {
		_ = h.settingsRepo.Set(context.Background(), "circuit_breaker_span_models", "2")
		h.settingsRepo.InvalidateCache("circuit_breaker_span_models")
	})
}

// TWO plan-refused models is the real #819 incident shape, and this is the
// accepted trade of the per-model-breaker design, pinned here so nobody has to
// rediscover it from a production report.
//
// Z.ai's coding plan refuses glm-4.7-flashx AND glm-4.5-x with 429 while
// glm-5.1 answers 200 on the same key. Each refusal charges the model it names,
// so two circuits open — and the design says a provider is down once `span`
// distinct models corroborate. At the default span of 2 they do, so the derived
// provider verdict skips healthy glm-5.1 as well.
//
// That is deliberate, not a gap. The design forbids exempting a refusal by its
// wording (a phrase list only ever recognises refusals somebody has already
// seen), and "two of this provider's models are refusing" is genuinely the
// shape a provider-wide fault has. The operator's lever is the span setting,
// and arm (b) is that lever: at span 3 the same two open circuits leave glm-5.1
// routable, so an operator who knows their plan excludes several models can buy
// the tolerance back without turning the breaker off.
//
// Threshold 2, so each model needs two refusals to open and one stray charge
// cannot stand in for corroboration.
func TestAttemptCandidate_TwoPlanRefusalsReachTheProviderVerdict(t *testing.T) {
	h, cand := entitlementHandler(t, http.StatusTooManyRequests, zaiEntitlementBody)

	// Two models on the SAME provider: the candidate's provider pointer is shared,
	// which is what makes them siblings under one circuit map.
	flashx, x45 := cand, cand
	flashx.model = &model.Model{ID: uuid.New(), ModelID: "glm-4.7-flashx"}
	x45.model = &model.Model{ID: uuid.New(), ModelID: "glm-4.5-x"}

	for _, refused := range []modelCandidate{flashx, x45} {
		runEntitlementAttempt(t, h, refused, 1)
		runEntitlementAttempt(t, h, refused, 1)
		if got := h.circuitBreaker.GetState(cand.provider.ID, refused.model.ModelID); got != failover.StateOpen {
			t.Fatalf("setup: %s circuit is %v, want open", refused.model.ModelID, got)
		}
	}

	// (a) At the default span of 2, two corroborating models ARE the provider
	// verdict, and it reaches the model that never failed.
	if !h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "glm-5.1") {
		t.Error("two open models did not reach the provider verdict at span 2: the derivation is not firing")
	}

	// (b) The escape hatch, over the very same circuits: raising the span above
	// the number of refused models leaves the healthy sibling routable.
	withBreakerSpan(t, h, "3")
	if h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "glm-5.1") {
		t.Error("glm-5.1 is still skipped at span 3: the operator's span lever does not lift the provider verdict")
	}
	// And the refused models stay dark on their own circuits, which the span
	// setting has no business touching.
	for _, refused := range []modelCandidate{flashx, x45} {
		if !h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, refused.model.ModelID) {
			t.Errorf("%s became routable at span 3: the span governs the provider verdict, not a model's own circuit", refused.model.ModelID)
		}
	}
}

// The other half of the rule. An ordinary 429 is the provider saying it is
// overloaded, which IS about its health, so it must keep charging — otherwise
// this would simply stop the breaker reacting to rate limiting.
func TestAttemptCandidate_AnOrdinaryRateLimitStillChargesTheBreaker(t *testing.T) {
	const rateLimited = `{"error":{"message":"Rate limit reached for this model, please retry shortly"}}`
	for _, tc := range []struct {
		name            string
		totalCandidates int
	}{
		{"last candidate", 1},
		{"with a candidate to fall back to", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cand := entitlementHandler(t, http.StatusTooManyRequests, rateLimited)
			runEntitlementAttempt(t, h, cand, tc.totalCandidates)
			runEntitlementAttempt(t, h, cand, tc.totalCandidates)

			if h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID) != failover.StateOpen {
				t.Error("two rate-limit 429s did not open the circuit: the breaker stopped reacting to overload")
			}
		})
	}
}

// The charge site and the skip site must agree on the key, and nothing warns
// when they do not: RecordFailure creates whatever circuit it is handed, so a
// charge landing on one key while resolveCandidates asks IsOpen about another
// leaves the failing model routed to forever with a full ledger nobody reads.
//
// A 5xx, so the classification plays no part: this is about the key alone.
func TestAttemptCandidate_TheChargedModelIsTheOneResolveSkips(t *testing.T) {
	h, cand := entitlementHandler(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	runEntitlementAttempt(t, h, cand, 1)
	runEntitlementAttempt(t, h, cand, 1)

	if !h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, cand.model.ModelID) {
		t.Error("the model the failures were routed to is not skipped: the charge and the skip disagree on the key")
	}
	if h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "sibling-model") {
		t.Error("a sibling model on the same provider is skipped: one model's failures darkened the whole provider")
	}
}

// A 5xx is unambiguous provider unhealth and is unaffected by any of this.
func TestAttemptCandidate_AServerFaultStillChargesTheBreaker(t *testing.T) {
	h, cand := entitlementHandler(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	runEntitlementAttempt(t, h, cand, 1)
	runEntitlementAttempt(t, h, cand, 1)

	if h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID) != failover.StateOpen {
		t.Error("two 500s did not open the circuit")
	}
}

// withRateLimitFailover sets failover_on_rate_limit for one test.
func withRateLimitFailover(t *testing.T, h *Handler, enabled string) {
	t.Helper()
	if err := h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", enabled); err != nil {
		t.Fatalf("set failover_on_rate_limit: %v", err)
	}
	h.settingsRepo.InvalidateCache("failover_on_rate_limit")
	t.Cleanup(func() {
		_ = h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "true")
		h.settingsRepo.InvalidateCache("failover_on_rate_limit")
	})
}

// With failover_on_rate_limit OFF, a 429 is deliberately recorded as a SUCCESS:
// the operator has asked to ride out rate limits on this provider rather than
// trip its breaker. The status→action mapping charging every 429 must not undo
// that: a 429 is only failover-eligible when the setting is on, and only the
// eligible branch consults the mapping.
//
// Threshold 1, so a single stray charge is enough to open the circuit and be
// seen.
func TestAttemptCandidate_ARateLimitIsNotChargedWhenFailoverIsOff(t *testing.T) {
	const rateLimited = `{"error":{"message":"Rate limit reached for this model, please retry shortly"}}`
	h, cand := entitlementHandler(t, http.StatusTooManyRequests, rateLimited)
	withBreakerThreshold(t, h, "1")
	withRateLimitFailover(t, h, "false")

	runEntitlementAttempt(t, h, cand, 1)

	if h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID) == failover.StateOpen {
		t.Error("a 429 opened the circuit with failover_on_rate_limit off: the setting asks to stay on the provider")
	}
}

// The hedged-streaming path charges its own 429s. Nothing else on that path
// does: the probe never reaches the sequential loop's charge sites, so deleting
// the one line under test here leaves the whole rest of the suite green.
func TestProbeStreamingCandidate_ChargesARateLimit(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThreshold(t, h, "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"Rate limit reached, retry shortly"}}`)
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	st, cand := probeStateForServer(srv.URL)
	st.circuitBreakerEnabled = true
	if res := h.probeStreamingCandidate(context.Background(), st, cand, 0, time.Second, time.Second); res.won {
		t.Fatal("a 429 must not win the race")
	}
	if h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID) != failover.StateOpen {
		t.Error("the hedge race did not charge a rate limit: nothing else does on that path now")
	}
}

// And its sibling: a plan refusal on the hedged path charges the refused model
// and leaves the rest of the provider alone. The refusal is one Z.ai answers for
// a model outside the coding plan while glm-5.1 keeps answering 200 on the same
// key, so a sibling still being routable is the whole point of the key.
func TestProbeStreamingCandidate_ChargesAPlanRefusalToTheModelAlone(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	withBreakerThreshold(t, h, "1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, zaiEntitlementBody)
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	st, cand := probeStateForServer(srv.URL)
	st.circuitBreakerEnabled = true
	_ = h.probeStreamingCandidate(context.Background(), st, cand, 0, time.Second, time.Second)

	if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
		t.Errorf("the refused model's circuit is %v, want open: the hedged path stopped charging plan refusals", got)
	}
	if h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "glm-5.1") {
		t.Error("a model outside the plan darkened a sibling on the hedged path")
	}
}

// The multimodal drain path charges its own 429s. Both flavours charge the model
// they were routed to, and neither reaches a sibling: at the default span of 2
// one open circuit is not a provider verdict.
//
// Two candidates, so the eligible-with-a-fallback branch runs.
func TestAttemptPassthroughCandidate_ChargesARateLimitToTheModelAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"ordinary rate limit", `{"error":{"message":"Rate limit reached, retry shortly"}}`},
		{"model outside the plan", zaiEntitlementBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })
			withBreakerThreshold(t, h, "1")

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			h.upstreamTransport = dialToTestServer(t, srv)

			m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small"}
			cand := goneCandidateAt(m, "Z.ai Coding Plan", "http://api.z.ai")
			st := &requestState{
				startTime: time.Now(), reqModel: "text-embedding-3-small",
				bodyBytes:             []byte(`{"model":"text-embedding-3-small","input":"hi"}`),
				failoverTimeout:       30 * time.Second,
				circuitBreakerEnabled: true,
				vkHash:                "test-hash",
				logData: &requestLogData{
					id: uuid.New().String(), modelID: "text-embedding-3-small",
					providerID: cand.provider.ID, providerName: "Z.ai Coding Plan",
					endpointType: endpointTypeEmbeddings, state: "pending",
					virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001",
				},
			}
			h.insertRequestLogAsync(st.logData)
			// totalCandidates 2: eligible AND a fallback, which is the drain path.
			h.attemptPassthroughCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, cand, 0, 2)

			if got := h.circuitBreaker.GetState(cand.provider.ID, cand.model.ModelID); got != failover.StateOpen {
				t.Errorf("the refused model's circuit is %v, want open", got)
			}
			if h.circuitBreaker.IsOpen(cand.provider.ID, cand.provider.Name, "text-embedding-3-large") {
				t.Error("the refusal darkened a sibling embeddings model on the same provider")
			}
		})
	}
}
