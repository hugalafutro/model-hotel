package proxy

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The exhaustion terminal of the failover loop: every candidate failed, or the
// deadline was hit. Split out of proxy_failover.go when that file reached the
// size ceiling.

// failAllExhausted handles phase E: every candidate failed (or the overall
// deadline was hit). It logs the exhaustion, records a 502 failure row, and
// writes the failover-vs-single-provider error response. numCandidates is the
// resolved candidate count (for the failRequest attempt index).
func (h *Handler) failAllExhausted(w http.ResponseWriter, st *requestState, numCandidates int) {
	last := st.lastReqErr
	status := last.terminalStatus()
	logMsg := last.terminalLogMessage(st.isFailover, numCandidates)
	clientMsg := last.terminalClientMessage(st.reqModel, st.isFailover)
	if st.isFailover {
		debuglog.Error("proxy: all providers exhausted", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "candidates", numCandidates, "failover_timeout", st.failoverTimeout)
	} else {
		debuglog.Error("proxy: provider request failed", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "request_timeout", st.failoverTimeout)
	}
	// Honest status for an all-busy exhaustion: every provider is alive and at
	// capacity, and OpenAI SDKs back off and retry on a 429 where a 502 is a
	// coin toss. The Retry-After is the last provider's own ask (or the class
	// default), so the client's backoff lines up with the slot actually
	// freeing. Behind failover_exhaustion_status_429 for any client that has
	// learnt to read the 502.
	if last.Kind == KindProviderSaturated && h.settingsRepo.GetBool(context.Background(), "failover_exhaustion_status_429", true) {
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(st.rateLimit.retryAfter)))
	}
	st.logData.providerID = uuid.Nil
	h.failRequest(st.logData, status, last.Kind, logMsg, numCandidates-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, clientMsg, status)
}

// retryAfterSeconds renders a wait as the whole seconds a Retry-After header
// carries: rounded up so a positive wait never becomes 0 ("retry now"), with
// the saturated class default standing in for an absent one.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		d = defaultSaturatedRetryAfter
	}
	return int((d + time.Second - 1) / time.Second)
}
