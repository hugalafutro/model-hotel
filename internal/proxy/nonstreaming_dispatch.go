package proxy

import "net/http"

// dispatchNonStreaming serves one non-streaming 2xx: read the answer, decide
// whether it is a completion at all while a sibling can still be asked, then
// hand it to the handler that writes it. The non-streaming twin of
// dispatchStreaming, which makes the same decision from its TTFT probe.
//
// r carries the attempt's own context (the client's, under the failover
// timeout), so every judgement below reads the same clock: an interrupted read
// is one the client or this gateway's own timeout ended, never the provider.
func (h *Handler) dispatchNonStreaming(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, responseHeaderMs float64, hasMoreCandidates bool) candidateOutcome {
	logData := st.logData
	// A non-streaming answer clears any gone-strike streak the model had, judged
	// after the handler on what the handler decoded rather than on the 200 that
	// preceded it. Both halves of that placement are load-bearing:
	//
	//   - Below the dialect translations, which attemptCandidate runs before
	//     calling this, because any of the three can read the body, fail, and
	//     send the attempt to failover. A provider that answered 200 with
	//     something that is not a Responses object, a Gemini answer or an
	//     Anthropic message has not served the model.
	//   - Below the handler, because a status is not an answer. `200
	//     {"choices":[]}` decodes, and on the last candidate it is forwarded as
	//     a normal completion, and is what an aggregator in front of a retired
	//     model returns between its gone-shaped 404s, resetting the count so
	//     three never land consecutively and the model is never nominated.
	//
	// producedOutput is where that line is drawn. The breaker verdict is
	// deferred to the handler's terminal write so the attempt trail's record
	// carries it; judgeAnswerNow is the fallback for a handler exit that wrote
	// nothing. It is armed on each of the two dispatches rather than once above
	// them, because the read between them can still fail over, and a verdict
	// left armed on a candidate the loop has moved past would fire on the
	// terminal row of a different one.
	if st.anthropicNativeAttempt {
		h.deferAnswerJudgement(st, candidate, logData, resp.StatusCode)
		outcome := h.handleNativeNonStreaming(w, r, st, candidate, resp, attempt, responseHeaderMs, hasMoreCandidates)
		judgeAnswerNow(logData)
		if producedOutput(logData) {
			h.noteModelServed(candidate.model, logData.endpointType)
		}
		return outcome
	}

	// The body is read here rather than inside the handler, and read before
	// anything is written, so a 2xx that turns out to carry no completion can
	// still go to a sibling. Past this point the client is committed to this
	// candidate: the handler writes the answer, or the gateway's own 502, and
	// neither can be taken back. The streaming twin makes the same decision at
	// the same moment, from its TTFT probe.
	//
	// The read runs under the attempt's context, so that is what an interrupted
	// read is judged by. With the bare client request instead, this gateway's own
	// request_timeout looks like the provider dying.
	ans := readNonStreamingBody(resp, logData.masker)
	if err := ans.completionFault(r.Context(), resp.StatusCode); err != nil && hasMoreCandidates && answerFaultIsRoutable(err) {
		// The provider generated this answer and billed the prompt for it, so
		// the charge is recorded before the candidate is left behind.
		h.meterRejectedPrompt(st, logData, ans.chat.Usage.PromptTokens)
		outcome := h.rejectUntranslatableBody(st, candidate, logData, "chat completion", resp.StatusCode, err, attempt, r)
		// Fully read already (or refused past the cap, where the rest is not
		// worth draining), so the connection is released here rather than by the
		// handler that normally owns it. After the reject, which settles the
		// attempt's in-flight slot as the failure it is before the close can
		// settle it as a clean success.
		_ = resp.Body.Close()
		return outcome
	}

	h.deferAnswerJudgement(st, candidate, logData, resp.StatusCode)
	h.handleNonStreamingResponse(w, r, logData, resp, ans, st.startTime, st.proxyOverhead, st.parseMs, st.timings, responseHeaderMs, st.vkHash, attempt)
	judgeAnswerNow(logData)
	if producedOutput(logData) {
		h.noteModelServed(candidate.model, logData.endpointType)
	}
	return outcomeServed
}
