package proxy

import (
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
func runEntitlementAttempt(t *testing.T, h *Handler, cand modelCandidate, srvURL string, totalCandidates int) {
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
			runEntitlementAttempt(t, h, cand, "", tc.totalCandidates)
			runEntitlementAttempt(t, h, cand, "", tc.totalCandidates)

			if h.circuitBreaker.GetState(cand.provider.ID) == failover.StateOpen {
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
			runEntitlementAttempt(t, h, cand, "", tc.totalCandidates)
			runEntitlementAttempt(t, h, cand, "", tc.totalCandidates)

			if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
				t.Error("two rate-limit 429s did not open the circuit: the breaker stopped reacting to overload")
			}
		})
	}
}

// A 5xx is unambiguous provider unhealth and is unaffected by any of this.
func TestAttemptCandidate_AServerFaultStillChargesTheBreaker(t *testing.T) {
	h, cand := entitlementHandler(t, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)
	runEntitlementAttempt(t, h, cand, "", 1)
	runEntitlementAttempt(t, h, cand, "", 1)

	if h.circuitBreaker.GetState(cand.provider.ID) != failover.StateOpen {
		t.Error("two 500s did not open the circuit")
	}
}
