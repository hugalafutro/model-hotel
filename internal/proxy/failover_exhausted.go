package proxy

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
)

// The exhaustion terminal of the failover loop: every candidate failed, or the
// deadline was hit.

// failAllExhausted ends a request whose candidates are all spent, or whose
// overall deadline expired. It logs the exhaustion, records the failure row and
// writes the failover-vs-single-provider error response. numCandidates is the
// resolved candidate count, used for the failRequest attempt index.
func (h *Handler) failAllExhausted(w http.ResponseWriter, st *requestState, numCandidates int) {
	last := st.lastReqErr
	status := last.terminalStatus()
	// Fenced: the message renders the last provider's own error text, which
	// may quote the prompt (content_fence.go).
	logMsg := st.logData.content.maskOne(last.terminalLogMessage(st.isFailover, numCandidates))
	clientMsg := last.terminalClientMessage(st.reqModel, st.isFailover)
	if st.isFailover {
		debuglog.Error("proxy: all providers exhausted", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "candidates", numCandidates, "failover_timeout", st.failoverTimeout)
		// all_busy means the last candidate was alive and at capacity; any other
		// cause, and the failover deadline, is all_failed.
		reason := "all_failed"
		if last.Kind == KindProviderSaturated {
			reason = "all_busy"
		}
		// Lower-cased like the group lookup: the client's spelling of the
		// group must not mint a series per casing.
		metrics.RecordFailoverExhausted(strings.ToLower(strings.TrimPrefix(st.reqModel, "hotel/")), reason)
	} else {
		debuglog.Error("proxy: provider request failed", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "request_timeout", st.failoverTimeout)
	}
	// An all-busy exhaustion answers 429 rather than 502: every provider is
	// alive and at capacity, and OpenAI SDKs back off and retry on a 429.
	// Retry-After carries the last provider's own ask, or the class default, so
	// the client's backoff lines up with a slot actually freeing.
	// Switching failover_exhaustion_status_429 off restores the 502.
	if last.Kind == KindProviderSaturated && h.settingsRepo.GetBool(context.Background(), "failover_exhaustion_status_429", true) {
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(st.rateLimit.retryAfter)))
	}
	st.logData.providerID = uuid.Nil
	h.failRequest(st.logData, status, last.Kind, logMsg, numCandidates-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, clientMsg, status)
}

// retryAfterSeconds renders a wait as the whole seconds a Retry-After header
// carries, rounded up so a positive wait never becomes 0 ("retry now"). An
// absent wait falls back to the saturated class default.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		d = defaultSaturatedRetryAfter
	}
	return int((d + time.Second - 1) / time.Second)
}
