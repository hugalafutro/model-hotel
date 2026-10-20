package proxy

// The terminal verdict of a request that no candidate served: which attempt's
// failure the exhaustion path renders, and the 429 verdict that goes with it.

// setReqErr records a failed attempt's structured cause as the terminal
// verdict so far and keeps the rendered lastErr string in sync. Every
// failover-loop failure goes through here, so the exhaustion path always has a
// structured error to render and classify.
//
// Underlying is the one field that can hold upstream text (an SSE error frame,
// a provider's sentence, a transport error quoting what came off the wire), and
// the exhaustion path renders it into the row's error_message, so it is masked
// and then fenced here, once, before anything reads it. Masked first, the order
// fencedFrameMessage uses: a provider is free to quote the operator's key back,
// and the row reaches the dashboard. Kind,
// Detail and Hint are written by this package and are never fenced, which is
// what keeps a prompt quoting gateway wording from blanking the gateway's own
// diagnosis of the failure.
func (st *requestState) setReqErr(e reqError) {
	// A refusal of the caller's body (a translation that cannot carry it) does
	// not displace ANOTHER candidate's provider-side failure as the terminal
	// verdict: that candidate had a route for the content and a retry may serve
	// it, which a 400 would deny. The same candidate's earlier failure (a busy
	// skip before the slot freed and the body was refused) and this gateway's
	// own internal error say nothing about a route, so the refusal replaces
	// them. A candidate is its provider AND its model: one provider can serve a
	// model with a route for the body beside one without (OpenCode's GPT and
	// Claude models). Sequential and hedged attempts both land here.
	rl := st.rateLimit
	if e.Kind == KindValidation {
		if prev := st.lastReqErr; prev.Kind != "" && prev.Kind != KindValidation && prev.Kind != KindInternal && (prev.Provider != e.Provider || prev.Model != e.Model) {
			return
		}
		// The refusal would be the verdict: a routed candidate's earlier
		// failure, displaced by busy skips since, is the verdict instead.
		if st.routedFailure.Kind != "" {
			st.lastReqErr, st.lastErr, st.terminalRateLimit = st.routedFailure, st.routedFailure.render(), st.routedFailureRateLimit
			return
		}
	}
	e.Underlying = st.logData.fence().fenceUpstream(string(st.logData.masks().mask([]byte(e.Underlying))))
	st.lastReqErr = e
	st.terminalRateLimit = rl
	if isRoutedFailure(e) {
		st.routedFailure, st.routedFailureRateLimit = e, rl
	}
	st.lastErr = e.render()
}

// busySkipDetail is the Detail of the saturated reqError an attempt records
// when its provider is at the learned in-flight window and no request is
// made, which tells that skip apart from a 429 the provider answered.
const busySkipDetail = "at in-flight limit"

// isRoutedFailure reports a failure produced by a provider that was sent the
// request: it had a route for the body, so a later refusal of that body by
// another candidate says nothing against it. A 429 the provider answered and
// an attempt or retry that timed out on it count; a skip without a request
// (busy, refused), this gateway's own errors and the client leaving do not.
func isRoutedFailure(e reqError) bool {
	switch e.Kind {
	case KindProviderError, KindProviderTimeout, KindProviderModelGone, KindProviderNotEntitled, KindProviderBadRequest, KindProviderQuotaExhausted, KindRetryTimeout, KindFailoverTimeout:
		return true
	case KindProviderSaturated:
		return e.Detail != busySkipDetail
	default:
		return false
	}
}
