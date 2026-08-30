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
// and must not take the whole provider out of rotation.
//
// breakerRecordAction keyed purely on the status, so every 429 charged the
// provider-wide breaker. Z.ai answers 429 for a model outside the coding plan
// while other models on the same provider and the same key answer 200 — verified
// against production: glm-5.1 returned 200 while glm-4.7-flashx and glm-4.5-x
// returned 429. Two probes to an uncovered model therefore blacked out every
// Z.ai model for the cooldown, the working ones included.
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

			if h.circuitBreaker.GetState(cand.provider.ID, "") == failover.StateOpen {
				t.Error("a model outside the plan opened the provider's circuit: every other model on it is now skipped")
			}
		})
	}
}

// The other half of the rule. An ordinary 429 is the provider saying it is
// overloaded, which IS about its health, so it must keep charging — otherwise
// this fix would simply stop the breaker reacting to rate limiting.
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

			if h.circuitBreaker.GetState(cand.provider.ID, "") != failover.StateOpen {
				t.Error("two rate-limit 429s did not open the circuit: the breaker stopped reacting to overload")
			}
		})
	}
}

// A 5xx is unambiguous provider unhealth and is unaffected by any of this.
func TestAttemptCandidate_AServerFaultStillChargesTheBreaker(t *testing.T) {
	h, cand := entitlementHandler(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	runEntitlementAttempt(t, h, cand, 1)
	runEntitlementAttempt(t, h, cand, 1)

	if h.circuitBreaker.GetState(cand.provider.ID, "") != failover.StateOpen {
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
// trip its breaker. Deferring the 429 verdict must not undo that.
//
// The first cut gated recordClassifiedOutcome only on the status, so a 429 that
// was never failover-eligible got the documented credit and then a charge on top
// — at a threshold of one, the very first rate limit blacked the provider out,
// which is the opposite of what the setting asks for.
func TestAttemptCandidate_ARateLimitIsNotChargedWhenFailoverIsOff(t *testing.T) {
	const rateLimited = `{"error":{"message":"Rate limit reached for this model, please retry shortly"}}`
	h, cand := entitlementHandler(t, http.StatusTooManyRequests, rateLimited)
	withBreakerThreshold(t, h, "1")
	withRateLimitFailover(t, h, "false")

	runEntitlementAttempt(t, h, cand, 1)

	if h.circuitBreaker.GetState(cand.provider.ID, "") == failover.StateOpen {
		t.Error("a 429 opened the circuit with failover_on_rate_limit off: the setting asks to stay on the provider")
	}
}

// The hedged-streaming path is now the ONLY thing charging the breaker for a 429
// there, since the header-time charge this PR removed used to cover it. Deleting
// that one line left the whole suite green before this test existed.
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
	if h.circuitBreaker.GetState(cand.provider.ID, "") != failover.StateOpen {
		t.Error("the hedge race did not charge a rate limit: nothing else does on that path now")
	}
}

// And its sibling: a plan refusal on the hedged path must not darken the
// provider either.
func TestProbeStreamingCandidate_DoesNotChargeAPlanRefusal(t *testing.T) {
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

	if h.circuitBreaker.GetState(cand.provider.ID, "") == failover.StateOpen {
		t.Error("a model outside the plan opened the provider's circuit on the hedged path")
	}
}

// The multimodal drain path is the other site the header-time charge used to
// cover. Two candidates, so the eligible-with-a-fallback branch runs.
func TestAttemptPassthroughCandidate_ChargesARateLimitButNotAPlanRefusal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantOpen bool
	}{
		{"ordinary rate limit", `{"error":{"message":"Rate limit reached, retry shortly"}}`, true},
		{"model outside the plan", zaiEntitlementBody, false},
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

			if open := h.circuitBreaker.GetState(cand.provider.ID, "") == failover.StateOpen; open != tc.wantOpen {
				t.Errorf("circuit open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

// The model-gone clause of refusalIsAboutTheModel: a 429 whose body says the
// model is gone is about the model, not the provider.
func TestRefusalIsAboutTheModel_ModelGoneIsNotProviderHealth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind ErrorKind
		body string
		want bool
	}{
		{"model gone", KindProviderModelGone, "the model has been retired", true},
		{"plan excludes this model", KindProviderNotEntitled, zaiEntitlementBody, true},
		{"account out of credit", KindProviderNotEntitled, "insufficient balance", false},
		{"ordinary overload", KindProviderError, "rate limit reached", false},
	} {
		if got := refusalIsAboutTheModel(tc.kind, tc.body); got != tc.want {
			t.Errorf("%s: refusalIsAboutTheModel = %v, want %v", tc.name, got, tc.want)
		}
	}
}
