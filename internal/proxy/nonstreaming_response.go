package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// This file is the non-streaming half of the proxy: reading an upstream answer
// once, deciding whether it is a completion, and serving or failing it.

// nonStreamingFailureDetail decides what a response that is not a 2xx
// completion may say about itself: the message stored in the request log
// (dashboard-visible), the detail handed to the classifier and the debug log,
// the error kind and the client-facing reason.
//
// It takes every response that is not a 2xx completion (any non-2xx whatever its
// body decodes as, plus a 2xx that is not a chat completion) and the two are NOT
// treated the same way. Do not merge them:
//
//   - A 2xx body is a completion. It failed to decode (a relay answering 200
//     with "created":"1699…" or "total_tokens":"12" as a string is the usual
//     cause) but it still holds the model's generated text, and this gateway
//     logs no prompt or response content. Only non-content diagnostics are
//     reported: the decode error, the body length, the content type. The body
//     itself goes nowhere near the request log or the debug log.
//
//   - A non-2xx carries no completion. Its body is the provider's error
//     document, and that text is the whole reason such a row is worth reading,
//     so it is sanitized and kept.
func nonStreamingFailureDetail(ctx context.Context, resp *http.Response, body []byte, readErr, decodeErr error, modelID string, fence *contentFence) (logMsg, detail string, kind ErrorKind, reason string) {
	if servedSuccessStatus(resp.StatusCode) {
		if readErr != nil {
			// A read nobody was waiting for is not the provider failing. The body
			// is read under the attempt's context, so a caller hanging up arrives
			// here as a read error, and reporting that as a provider fault puts
			// someone else's cancellation on the provider's row and on its
			// circuit. requestAbandoned is the package's spelling of which
			// interruptions those are.
			if kind, aborted := cancelKind(ctx, readErr); aborted {
				if requestAbandoned(ctx, readErr) {
					detail = "the request was interrupted before the response was read"
					return detail, detail, kind, "the request was interrupted"
				}
				// The rest of them are this gateway's own per-attempt deadline
				// expiring on a provider that answered headers and then said
				// nothing: a stall, with the caller still waiting for it. The
				// streaming half classifies the identical event provider_timeout
				// and charges it (classifyProbeFailure), so this half does too,
				// and the last candidate in a group records what a candidate with
				// a sibling behind it would have.
				detail = fmt.Sprintf("upstream stopped sending before the per-attempt deadline: %s (body_bytes=%d)", errString(readErr), len(body))
				return detail, detail, KindProviderTimeout, "the provider stopped sending its response"
			}
			// A body that died on the wire is the provider breaking after it
			// committed the status, which is what the breaker exists to catch.
			detail = fmt.Sprintf("upstream body read error: %s (body_bytes=%d)", errString(readErr), len(body))
			return detail, detail, KindProviderError, "the provider stopped sending its response"
		}
		detail = fmt.Sprintf("response decode error: %s (body_bytes=%d, content_type=%q)",
			errString(decodeErr), len(body), resp.Header.Get("Content-Type"))
		// The gateway's own cap is not the provider failing, so it is the one
		// refusal here that leaves the circuit alone (translationIsProviderFault
		// draws the same line for the paths that fail over).
		if errors.Is(decodeErr, httpx.ErrBodyTooLarge) {
			return detail, detail, KindProviderBadRequest, "the provider returned a response larger than the gateway will read"
		}
		// provider_error, the same kind and the same charge rejectUntranslatableBody
		// records when a sibling is still available. A 2xx whose body is not a
		// completion is one fault, and which candidate in the group happened to
		// produce it must not decide whether its provider is charged for it.
		//
		// Not "upstream provider returned HTTP 200": reporting the status as the
		// failure sends operators hunting a provider outage that is not
		// happening.
		return detail, detail, KindProviderError, "the provider returned a response the gateway could not decode"
	}
	// Classify from the provider's own words, before the fence can take them:
	// the classification decides routing and the breaker and stores nothing,
	// so it reads the body whether or not the body may be kept.
	raw := util.SanitizeLogBody(string(body), logBodyCap)
	kind, reason = classifyUpstreamError(resp.StatusCode, raw, modelID)
	// The one piece of upstream text on this path. Whatever comes back is
	// storable, and the gateway's own prefix below is added after it.
	detail = fence.fenceUpstream(raw)
	// The prefix names which of the two ways in led here, so the row does not
	// report a decode failure for a body that decoded.
	logMsg = fmt.Sprintf("upstream HTTP %d: %s", resp.StatusCode, detail)
	if decodeErr != nil {
		logMsg = fmt.Sprintf("response decode error: %s", detail)
	}
	return logMsg, detail, kind, reason
}

// nonStreamingBodyCap bounds the non-streaming completion body held in memory
// for decoding. Unlike the caps on error bodies (failoverErrorClassifyCap,
// responsesLearnBodyCap, miniMaxEnvelopeCap) this one guards a legitimate
// payload, so it sits far above any real answer rather than just above any real
// error message: 128k output tokens of text is well under 1MB, and the outliers
// are chat completions carrying base64 image parts, several of which still fit.
// It is 4x the multimodal pass-through's passthroughJSONBufferCap, which can
// degrade to an unbuffered stream when a body is too large; this path cannot,
// since it must decode to meter and normalise, so exceeding the cap fails the
// request and the cap carries that much more headroom.
const nonStreamingBodyCap = 32 << 20 // 32MB

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, ans nonStreamingAnswer, startTime time.Time, proxyOverhead, parseMs float64, timings resolveTimings, responseHeaderMs float64, vkHash string, attempt int) {
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleNonStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", attempt, "response_header_ms", responseHeaderMs)

	w.Header().Set("Content-Type", "application/json")

	// The body was read into memory once, up front, by the caller (see
	// readNonStreamingBody), because three places want the same bytes: the
	// success branch decodes them, the failure branch sanitizes them into the
	// request log, and the caller asks whether this answer is a completion at
	// all before any of it is written. resp.Body can only be consumed once, so
	// whichever of them read it directly would starve the others.
	body, chatResp, readErr, decodeErr := ans.body, ans.chat, ans.readErr, ans.decodeErr

	// Only a 2xx that decodes is a completion. Some upstreams (OpenCode Zen and
	// OpenCode Go both do this) answer a failed request with a non-2xx carrying
	// a complete chat.completion envelope and no error object at all, which
	// decodes cleanly, and forwarding that leaves the caller with a failure
	// status and nothing to read `.error.message` off. The status decides; the
	// body only says whether the success shape is available.
	// Stamped once for every arm, so no terminal path can record five of six
	// (the 204 arm used to record none of them).
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.applyTimings(timings)

	switch {
	case decodeErr == nil && servedSuccessStatus(resp.StatusCode):
		totalDuration := util.MillisSince(startTime)
		var reasoningTokens int
		if chatResp.Usage.CompletionTokensDetails != nil && chatResp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			reasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		// Clamped into locals, leaving chatResp alone: the body is re-encoded to
		// the caller further down, and rewriting a provider's usage block on the
		// way through is not this gateway's job, as the native Anthropic path's
		// untouched forward shows. A partial rewrite is worse than none, since
		// the block carries nine token members and clamping the three the meter
		// reads hands the caller an arithmetic no provider produced. The gateway
		// owns its OWN state, so the bound applies to the log row, the TPS math
		// and the charge below, all of which read these locals.
		promptTokens, completionTokens, reasoningTokens := h.clampReportedUsage(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, reasoningTokens, logData)
		tps := tokensPerSecond(completionTokens+reasoningTokens, totalDuration, responseHeaderMs)

		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.responseHeaderMs = responseHeaderMs
		logData.tokensPerSecond = tps
		// Added, not assigned: a candidate that answered 2xx without an answer
		// and was failed over already billed its prompt, and meterRejectedPrompt
		// put that charge here. The row reports what the request cost, which on a
		// walked group is more than the provider that finally served it charged.
		logData.tokensPrompt += promptTokens
		logData.tokensCompletion = completionTokens
		logData.tokensCompletionReasoning = reasoningTokens
		logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss = extractCacheTokens(chatResp.Usage)
		logData.failoverAttempt = attempt
		logData.state = "completed"
		// Whether the model actually answered, judged where the decoded body is
		// in hand. The failover loop clears the gone-strike streak on it (see
		// dispatchNonStreaming): a 200 is only a status, and a decodable-but-empty
		// completion is what an aggregator in front of a retired model returns,
		// which would reset the count so the model is never nominated.
		logData.deliveredContent = chatAnswerCarriesContent(chatResp)
		// The different question the breaker asks of the same body: see
		// answerCarriesSomething.
		logData.emptyCompletion = !answerCarriesSomething(chatResp)
		// Fire-and-forget: skip WaitForInsert so TTFB is not blocked. The async
		// INSERT is very likely complete by now; if not, the UPDATE affects 0
		// rows and is logged as a warning.
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

		promptTokens, completionTokens, reasoningTokens = estimateMissingUsage(promptTokens, completionTokens, reasoningTokens, logData, chatAnswerBytes(chatResp))
		h.recordTokenUsage(vkHash, logData, promptTokens, completionTokens, reasoningTokens)

		// Normalize the reasoning fields in the response message so
		// reasoning_content is always populated whatever the upstream format
		// (Ollama's reasoning, OpenRouter's reasoning_details, MiniMax's
		// <thinking> tags in content).
		for i := range chatResp.Choices {
			msg := &chatResp.Choices[i].Message
			// Rule 1: reasoning to reasoning_content.
			if msg.Reasoning != "" && msg.ReasoningContent == "" {
				msg.ReasoningContent = msg.Reasoning
			}
			// Rule 2: reasoning_details text to reasoning_content.
			if msg.ReasoningContent == "" && len(msg.ReasoningDetails) > 0 {
				var texts []string
				for _, rd := range msg.ReasoningDetails {
					if rd.Type == "reasoning.text" && rd.Text != "" {
						texts = append(texts, rd.Text)
					}
				}
				if len(texts) > 0 {
					msg.ReasoningContent = strings.Join(texts, "")
				}
			}
			// Rule 3: <thinking> tags in content to reasoning_content.
			if c, ok := msg.Content.(string); ok && c != "" {
				if thinking, remaining := ExtractThinking(c); thinking != "" {
					if msg.ReasoningContent == "" {
						msg.ReasoningContent = thinking
					} else {
						msg.ReasoningContent += thinking
					}
					msg.Content = remaining
				}
			}
		}

		// A success status the provider chose is kept. The body is the gateway's
		// own re-encoding either way, but rewriting 201 to 200 would overrule
		// the provider about whether its own answer was created or merely
		// returned.
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(resp.StatusCode)
		}
		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			debuglog.Error("proxy: failed to encode response", "model", logData.modelID, "provider", logData.providerName, "error", err)
		}
		// reported_*, because these are the provider's own figures while the row
		// carries what was recorded: the two differ whenever the bound bit.
		debuglog.Info("proxy: non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "duration_ms", totalDuration, "reported_prompt_tokens", chatResp.Usage.PromptTokens, "reported_completion_tokens", chatResp.Usage.CompletionTokens)
	case bodilessSuccessStatus(resp.StatusCode):
		// A success whose status forbids a body. There is nothing to decode and
		// nothing to meter, and an error envelope written here would be a body
		// this gateway invented for a request the provider considered
		// successful, under a status that may not carry one.
		//
		// Keyed on the STATUS, not on the body being empty. A 200 with an empty
		// body is a provider that answered nothing and must fail; only 204/205
		// promise no body. Any other 2xx carrying something that is not a
		// completion falls through to the failure branch exactly as a 200 does:
		// the caller asked for a chat completion, and a success status alone is
		// not one.
		totalDuration := util.MillisSince(startTime)
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.responseHeaderMs = responseHeaderMs
		logData.failoverAttempt = attempt
		logData.state = "completed"
		// Completed, and empty. recordAnswerOutcome credits the provider for a
		// completed answer unless told otherwise, so without this a relay
		// answering 204 to every request would be credited a breaker success
		// each time, its circuit could never open, and the group would route to
		// a black hole for ever.
		//
		// This CHARGES where the pass-through families make a 204 a breaker
		// no-op (serveBufferedJSONPassthrough), and the split is deliberate: a
		// chat completion that answers 204 has by definition produced no
		// completion, while an embeddings or image family has no rule that a
		// success must carry a body.
		//
		// deliveredContent is not set alongside it: it is already false, and
		// every site that sets it sits on a terminal path that cannot precede
		// this branch.
		logData.emptyCompletion = true
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		w.WriteHeader(resp.StatusCode)
		debuglog.Info("proxy: upstream answered with no content", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "duration_ms", totalDuration)
	default:
		totalDuration := util.MillisSince(startTime)
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.responseHeaderMs = responseHeaderMs
		logMsg, detail, kind, reason := nonStreamingFailureDetail(r.Context(), resp, body, readErr, decodeErr, logData.modelID, logData.fence())
		// body is already exact-masked; the log row also gets the key-shape
		// layer, like every other stored error message.
		logData.errorMessage = string(maskKeyShapedTokens([]byte(logMsg)))
		logData.errorKind = kind
		logData.failoverAttempt = attempt
		logData.state = "failed"
		// Fire-and-forget: skip WaitForInsert so the error response is not
		// blocked.
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		if debuglog.Level() <= slog.LevelDebug {
			// detail left the fence above, so the app log gets the same text
			// the row does.
			debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "error_kind", kind, "model", logData.modelID, "provider", logData.providerName, "error", detail, "duration_ms", totalDuration)
		}
		// The row keeps resp.StatusCode above, since what the upstream said is
		// the diagnostic. Only what the CLIENT is told changes.
		writeOpenAIError(w, upstreamClientMessage(logData.providerName, resp.StatusCode, reason), nonCompletionClientStatus(resp.StatusCode))
	}
}

// nonCompletionClientStatus is the status the CLIENT is answered with when an
// upstream response is not a completion: the provider's own status, except for a
// 2xx.
//
// A 2xx becomes 502, because the gateway's own error envelope must never travel
// under a success status. An OpenAI SDK does not raise on a 2xx: it unmarshals
// the envelope, finds no choices, and hands the caller an empty answer instead
// of an error, so a relay answering 200 with something that is not a completion
// leaves the request log reading "failed" beside a client that believes it
// succeeded, with nothing retrying and nothing alerting.
//
// 502 rather than another code because the multimodal pass-through answers
// exactly that for the same shape (serveBufferedJSONPassthrough on a broken
// read, serveStreamedPassthrough on an empty body). The upstream status still
// reaches the request-log row either way, and reaches the CLIENT only when the
// provider is named: upstreamClientMessage appends it to the message with the
// provider, and returns the bare reason without one.
//
// The non-2xx return is defensive. Every caller reaches this handler through
// attemptCandidate, which sends anything that is not a 2xx to
// forwardUpstreamError first (proxy_failover.go), so only the 2xx branch runs in
// production; the other keeps the function total for a handler driven directly.
//
// This is not reached for 204/205, which have their own branch, nor for a 2xx
// that decodes as a completion. Nor for a 2xx that is not a completion while a
// sibling remains: dispatchNonStreaming asks completionFault before this handler
// is called and fails over instead, so what is left here is the last candidate's
// answer, where this gateway owes the client an error it can read.
func nonCompletionClientStatus(upstreamStatus int) int {
	if servedSuccessStatus(upstreamStatus) {
		return http.StatusBadGateway
	}
	return upstreamStatus
}

// bodilessSuccessStatus reports whether a 2xx status is one HTTP forbids a body
// on, so an empty response under it is the provider's complete answer rather
// than a truncated or missing one.
func bodilessSuccessStatus(code int) bool {
	return code == http.StatusNoContent || code == http.StatusResetContent
}

// errEmptyCompletion is the fault a 2xx that decoded as a completion carrying
// nothing fails over with. Gateway-authored, so it can be reported as it is:
// there is no upstream text behind it to mask or fence.
var errEmptyCompletion = errors.New("upstream answered with a completion carrying no content")

// nonStreamingAnswer is one upstream answer read and decoded once: the bytes,
// the completion they decoded as, and the two failures kept apart. It is read
// before anything is written to the client so the caller can decide whether
// this candidate answered at all (completionFault) while failing over to a
// sibling is still possible.
type nonStreamingAnswer struct {
	body      []byte
	chat      ChatCompletionResponse
	readErr   error
	decodeErr error
}

// completionCarriesAnswer reports whether a decoded completion is the provider
// answering, which is a lower bar than whether it delivered content.
//
// Everything answerCarriesSomething counts, plus a choice that states how the
// generation ended. Its streaming twin sets the bar lower still: any data frame
// that is neither empty, [DONE] nor an error envelope counts as a token, so a
// stream carrying one role-only delta is served. This is the closest a decoded
// body gets to that without letting `{"choices":[]}`, the shape an aggregator in
// front of a dead model returns, read as an answer.
func completionCarriesAnswer(out ChatCompletionResponse) bool {
	if answerCarriesSomething(out) {
		return true
	}
	for _, choice := range out.Choices {
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			return true
		}
	}
	return false
}

// completionFault reports what stopped a success status from being a completion,
// or nil when the answer can be served. It is the non-streaming twin of the TTFT
// probe's verdict: an upstream that answers 2xx and then hands over something
// this gateway cannot turn into a completion has not served the request, and a
// healthy sibling in the group can.
//
// Three exclusions, each of which would otherwise send a good request to another
// provider:
//
//   - A non-2xx is not this question. forwardUpstreamError already owns every
//     one of them, with the failover rules in shouldFailover.
//   - 204/205 promise no body, so the empty one they carry is the whole answer
//     and its decode failure is expected.
//   - An attempt nobody is waiting for. requestAbandoned draws that line, and
//     draws it narrowly: a caller that hung up and a superseded hedge, never
//     this gateway's own per-attempt deadline, which is a provider that stalled
//     after its headers and is exactly what the sibling exists for.
//
// A body that decoded but carries nothing is a fault too, for the same reason
// its streaming twin fails over on a stream that ends without a single chunk: a
// zero-token answer is not a valid one in almost any real use, and the sibling
// that can serve one is right there. Silence is partly a function of the prompt,
// so a caller can send every tenant's request down the whole candidate list by
// coercing a model into saying nothing; that risk is accepted here as the stream
// probe accepts it, and the breaker charge bounds the provider that does it
// habitually.
//
// The bar for that is completionCarriesAnswer and NOT emptyCompletion's
// answerCarriesSomething, which is stricter than anything the stream probe
// applies. The two questions differ: the breaker charges an answer that
// delivered nothing, while this one asks whether the provider answered at all,
// and a stated finish_reason is the provider saying how its own generation
// ended. Routing around that would re-bill a prompt on a sibling for a
// completion the first provider finished.
//
// For a body that did not parse, the decode failure is what decides, and a read
// error alone never does. A provider that sent the whole document and then
// dropped the connection without its terminal chunk leaves a body that parses
// perfectly beside an ErrUnexpectedEOF, and that answer is served
// (readNonStreamingBody says why). Failing over on it would discard a complete
// answer and re-bill the prompt. When both are set the read error is the one
// reported, since the wire is where it started.
//
// A fault is not automatically worth a sibling: answerFaultIsRoutable says which
// ones are.
func (ans nonStreamingAnswer) completionFault(ctx context.Context, status int) error {
	if !servedSuccessStatus(status) || bodilessSuccessStatus(status) {
		return nil
	}
	err := ans.decodeErr
	if ans.readErr != nil {
		err = ans.readErr
	}
	// Above both verdicts, not just the decode one: an attempt nobody is waiting
	// for is nobody's answer to serve, and an abandoned read can leave a body
	// that parses into an empty completion just as easily as one that does not
	// parse at all. requestAbandoned reads the context even when nothing errored,
	// so the empty-answer arm is covered by the same call.
	if requestAbandoned(ctx, err) {
		return nil
	}
	if ans.decodeErr != nil {
		return err
	}
	if completionCarriesAnswer(ans.chat) {
		return nil
	}
	return errEmptyCompletion
}

// answerFaultIsRoutable reports whether a 2xx that carried no answer is worth
// asking a sibling about. Every fault is, except this gateway's own body cap.
//
// An answer's size is a function of the request, so a prompt that draws a body
// past the cap from one provider draws one past it from the next: failing over
// walks the whole group, re-billing the prompt at every stop, to render the same
// 502 the first candidate already owed the client. It is also not the provider's
// failure, which is why translationIsProviderFault leaves the circuit alone for
// the same sentinel, so a group would burn itself down with nothing recorded
// anywhere.
//
// Shared by the chat dispatch and the native Anthropic handler, the two paths
// whose reads are capped.
func answerFaultIsRoutable(err error) bool {
	return !errors.Is(err, httpx.ErrBodyTooLarge)
}

// readNonStreamingBody reads the upstream body once and decodes it into the
// answer every later step reads: the bytes, the decoded completion, and the two
// failures kept apart (what went wrong on the wire, what went wrong parsing).
//
// The read is bounded (nonStreamingBodyCap) so one upstream cannot make the
// gateway buffer an arbitrary amount: cap+1 is read, and a body that reaches
// cap+1 is refused rather than decoded, because a truncated completion
// re-encoded as a valid one would hand the client silently mutilated content.
// That refusal is THIS gateway's policy and not the provider failing, so it is
// reported as a decode failure: folding it into readErr would report an
// oversized answer as "the provider stopped sending its response" and charge its
// provider's circuit breaker for sending too much.
//
// json.Decoder, not json.Unmarshal: a decoder stops at the end of the first JSON
// value, so a completion with trailing bytes after it still decodes.
//
// The decode is attempted even when the read errored, because JSON is
// self-delimiting: a provider that sent the whole document and then dropped the
// connection without its terminal chunk yields ErrUnexpectedEOF from a body that
// parses perfectly, and discarding that throws away a complete answer and
// charges the provider for it. If the bytes really were cut short the decode
// fails too, and the read error is what gets reported.
func readNonStreamingBody(resp *http.Response, masker credentialMasker) nonStreamingAnswer {
	var ans nonStreamingAnswer
	ans.body, ans.readErr = io.ReadAll(io.LimitReader(resp.Body, nonStreamingBodyCap+1))
	if ans.readErr == nil && len(ans.body) > nonStreamingBodyCap {
		// Wrapping httpx.ErrBodyTooLarge, the sentinel every other capped read
		// in this codebase refuses with, so the one rule that a body past a cap
		// is this gateway's limit and never the provider's fault
		// (translationIsProviderFault) can be written once and hold for all of
		// them.
		ans.decodeErr = fmt.Errorf("upstream response exceeds the %d byte non-streaming body cap: %w", nonStreamingBodyCap, httpx.ErrBodyTooLarge)
	}
	// Exact-key scrub on the whole body: the client answer and the failure log
	// message both derive from it, and a success body is content where the
	// key-shape regex must not run.
	ans.body = masker.maskExact(ans.body)
	if ans.decodeErr != nil {
		return ans
	}
	if err := json.NewDecoder(bytes.NewReader(ans.body)).Decode(&ans.chat); err != nil {
		// readErr is left as it was rather than cleared. It is only ever
		// consulted alongside a decode failure, since a clean decode means the
		// 2xx branch serves the answer without looking at it, so clearing it
		// would claim a meaning it does not have.
		ans.decodeErr = err
		if ans.readErr != nil {
			ans.decodeErr = ans.readErr
		}
	}
	return ans
}
