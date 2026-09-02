package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// This file is the non-streaming half of the proxy: reading an upstream answer
// once, deciding whether it is a completion, and serving or failing it. Moved
// here verbatim from proxy.go, which the 2xx-metering change pushed past the
// 800-line ceiling. The same commit then changed what moved, so read the diff
// rather than trusting the move to be inert.

// nonStreamingFailureDetail decides what a response that is not a 2xx
// completion may say about itself: the message stored in the request log
// (dashboard-visible), the detail handed to the classifier and the debug log,
// the error kind and the client-facing reason.
//
// It takes every response that is not a 2xx completion — any non-2xx whatever
// its body decodes as, plus a 2xx that is not a chat completion — and the two
// are NOT treated the same way. Do not merge them back together:
//
//   - A 2xx body is a completion. It failed to decode (a relay answering 200
//     with "created":"1699…" or "total_tokens":"12" as a string is the usual
//     cause) but it still holds the model's generated text, and this gateway
//     logs no prompt or response content, ever. Only non-content diagnostics
//     are reported: the decode error, the body length, the content type. The
//     body itself goes nowhere near the request log or the debug log.
//
//   - A non-2xx carries no completion. Its body is the provider's error
//     document, and that text is the whole reason such a row is worth reading,
//     so it is sanitized and kept.
func nonStreamingFailureDetail(ctx context.Context, resp *http.Response, body []byte, readErr, decodeErr error, modelID string) (logMsg, detail string, kind ErrorKind, reason string) {
	if servedSuccessStatus(resp.StatusCode) {
		if readErr != nil {
			// A read the provider did not break is not the provider failing. The
			// body is read under the attempt's context, so a caller hanging up
			// AND this gateway's own request_timeout both arrive here as read
			// errors — and reporting either as a provider fault put someone
			// else's cancellation on the provider's row and, once the breaker
			// started reading these, its circuit. cancelKind is the package's own
			// classifier; hand-rolling a narrower one here is what dropped the
			// deadline case.
			if kind, aborted := cancelKind(ctx, readErr); aborted {
				detail = "the request was interrupted before the response was read"
				return detail, detail, kind, "the request was interrupted"
			}
			// A body that died on the wire is not a body this gateway could not
			// parse, and the two were collapsed into one kind. The parse failure
			// is provider_bad_request because the answer is probably still in
			// there; a transport failure is the provider breaking after it
			// committed the status, which is what the breaker exists to catch —
			// and classifying it as the parse failure left it uncharged.
			detail = fmt.Sprintf("upstream body read error: %s (body_bytes=%d)", errString(readErr), len(body))
			return detail, detail, KindProviderError, "the provider stopped sending its response"
		}
		detail = fmt.Sprintf("response decode error: %s (body_bytes=%d, content_type=%q)",
			errString(decodeErr), len(body), resp.Header.Get("Content-Type"))
		// Not "upstream provider returned HTTP 200": reporting the status as the
		// failure sent operators hunting a provider outage that never happened.
		return detail, detail, KindProviderBadRequest, "the provider returned a response the gateway could not decode"
	}
	detail = util.SanitizeLogBody(string(body), 10000)
	// The prefix names which of the two ways in led here, so the row does not
	// report a decode failure for a body that decoded perfectly well.
	logMsg = fmt.Sprintf("upstream HTTP %d: %s", resp.StatusCode, detail)
	if decodeErr != nil {
		logMsg = fmt.Sprintf("response decode error: %s", detail)
	}
	// Classify from the body so the row is not left with an empty error_kind.
	kind, reason = classifyUpstreamError(resp.StatusCode, detail, modelID)
	return logMsg, detail, kind, reason
}

// nonStreamingBodyCap bounds the non-streaming completion body held in memory
// for decoding. Unlike the caps on error bodies (failoverErrorClassifyCap,
// responsesLearnBodyCap, miniMaxEnvelopeCap) this one guards a legitimate
// payload, so it is set far above any real answer rather than just above any
// real error message: 128k output tokens of text is well under 1MB, and the
// outliers are chat completions carrying base64 image parts, several of which
// still fit. It is 4x the multimodal pass-through's passthroughJSONBufferCap,
// which can degrade to an unbuffered stream when a body is too large — this
// path cannot, since it must decode to meter and normalise, so exceeding the
// cap fails the request and it is set with that much more headroom.
const nonStreamingBodyCap = 32 << 20 // 32MB

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, failoverLookupMs, modelLookupMs, providerLookupMs, keyDecryptMs, dialMs, settingsReadMs, responseHeaderMs float64, vkHash string, attempt int) {
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleNonStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", attempt, "response_header_ms", responseHeaderMs)

	w.Header().Set("Content-Type", "application/json")

	// The body is read into memory once, up front, because both branches below
	// want the same bytes: the success branch decodes them, the failure branch
	// sanitizes them into the request log. resp.Body can only be consumed once,
	// so whichever branch read it directly would starve the other.
	//
	// json.Decoder, not json.Unmarshal: a decoder stops at the end of the first
	// JSON value, so a completion with trailing bytes after it still decodes,
	// where an Unmarshal rejects the whole body.
	//
	// The read is bounded (nonStreamingBodyCap) so one upstream cannot make the
	// gateway buffer an arbitrary amount: cap+1 is read, and a body that reaches
	// cap+1 is refused as oversized rather than decoded, because a truncated
	// completion re-encoded as a valid one would hand the client silently
	// mutilated content.
	body, chatResp, readErr, decodeErr := readNonStreamingBody(resp, logData.masker)

	// Only a 2xx that decodes is a completion. Some upstreams (OpenCode Zen and
	// OpenCode Go both do this) answer a failed request with a non-2xx carrying a
	// complete chat.completion envelope and no error object at all, which decodes
	// cleanly; forwarding that leaves the caller with a failure status and nothing
	// to read `.error.message` off. Status decides, the body only says whether the
	// success shape is even available.
	switch {
	case decodeErr == nil && servedSuccessStatus(resp.StatusCode):
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		var tps float64
		var reasoningTokens int
		if chatResp.Usage.CompletionTokensDetails != nil && chatResp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			reasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		// Clamped into locals, leaving chatResp alone: the body is re-encoded
		// to the caller further down, and rewriting a provider's usage block on
		// the way through is not this gateway's job. The native Anthropic path
		// forwards the provider's bytes untouched and is the right precedent;
		// a partial rewrite is worse than none, because the block carries nine
		// token members and clamping the three the meter reads hands the caller
		// an arithmetic no provider produced. What the gateway owns is its OWN
		// state, so the bound applies to the log row, the TPS math and the
		// charge below, all of which read these locals.
		promptTokens, completionTokens, reasoningTokens := h.clampReportedUsage(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, reasoningTokens, logData)
		totalOutputTokens := completionTokens + reasoningTokens
		generationDuration := totalDuration - responseHeaderMs
		// Avoid absurd TPS when generation time is negligible
		// (e.g. non-streaming where response_header_ms ≈ duration_ms).
		minGeneration := max(1.0, totalDuration*0.05)
		if totalOutputTokens > 0 && generationDuration >= minGeneration {
			tps = float64(totalOutputTokens) / float64(generationDuration) * 1000
		} else if totalOutputTokens > 0 && totalDuration > 0 {
			tps = float64(totalOutputTokens) / float64(totalDuration) * 1000
		}

		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.failoverLookupMs = failoverLookupMs
		logData.dialMs = dialMs
		logData.settingsReadMs = settingsReadMs
		logData.responseHeaderMs = responseHeaderMs
		logData.tokensPerSecond = tps
		logData.tokensPrompt = promptTokens
		logData.tokensCompletion = completionTokens
		logData.tokensCompletionReasoning = reasoningTokens
		logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss = extractCacheTokens(chatResp.Usage)
		logData.failoverAttempt = attempt
		logData.state = "completed"
		// Whether the model actually answered, judged where the decoded body is in
		// hand. The failover loop clears the gone-strike streak on it (see
		// attemptCandidate): a 200 is a status, and a decodable-but-empty
		// completion is what an aggregator in front of a retired model returns,
		// resetting the count so the model is never nominated.
		logData.deliveredContent = chatAnswerCarriesContent(chatResp)
		// And the different question the breaker asks of the same body: see
		// answerCarriesSomething.
		logData.emptyCompletion = !answerCarriesSomething(chatResp)
		// Fire-and-forget: skip WaitForInsert to avoid blocking TTFB.
		// The async INSERT is very likely complete by now; if not, the
		// UPDATE simply affects 0 rows (harmless, logged as warning).
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

		promptTokens, completionTokens, reasoningTokens = estimateMissingUsage(promptTokens, completionTokens, reasoningTokens, logData, chatAnswerBytes(chatResp))
		h.recordTokenUsage(vkHash, logData, promptTokens, completionTokens, reasoningTokens)

		// Normalize reasoning fields in the response message so that
		// reasoning_content is always populated regardless of upstream
		// provider format (Ollama's reasoning, OpenRouter's reasoning_details,
		// MiniMax's <thinking> tags in content).
		for i := range chatResp.Choices {
			msg := &chatResp.Choices[i].Message
			// Rule 1: reasoning → reasoning_content
			if msg.Reasoning != "" && msg.ReasoningContent == "" {
				msg.ReasoningContent = msg.Reasoning
			}
			// Rule 2: reasoning_details text → reasoning_content
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
			// Rule 3: <thinking> tags in content → reasoning_content
			if c, ok := msg.Content.(string); ok && c != "" {
				if thinking, remaining, found := extractThinkingFromContent(c); found {
					if msg.ReasoningContent == "" {
						msg.ReasoningContent = thinking
					} else {
						msg.ReasoningContent += thinking
					}
					msg.Content = remaining
				}
			}
		}

		// A success status the provider chose is kept. The body is the
		// gateway's own re-encoding either way, but rewriting 201 to 200 would
		// be this gateway overruling a provider about whether its own answer
		// was created or merely returned.
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(resp.StatusCode)
		}
		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			debuglog.Error("proxy: failed to encode response", "model", logData.modelID, "provider", logData.providerName, "error", err)
		}
		// reported_*, because these are the provider's own figures and the row
		// carries what was recorded: the two differ whenever the bound bit, and
		// an operator comparing them needs to know which is which.
		debuglog.Info("proxy: non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "duration_ms", totalDuration, "reported_prompt_tokens", chatResp.Usage.PromptTokens, "reported_completion_tokens", chatResp.Usage.CompletionTokens)
	case bodilessSuccessStatus(resp.StatusCode):
		// A success whose status forbids a body. There is nothing to decode and
		// nothing to meter, and an error envelope written here would be a body
		// this gateway invented for a request the provider considered
		// successful, under a status that may not carry one.
		//
		// Keyed on the STATUS, not on the body being empty. A 200 with an empty
		// body is a provider that answered nothing and must keep failing as it
		// always has (TestHandleNonStreamingResponse_EmptyBody); only 204/205
		// promise no body. Any other 2xx carrying something that is not a
		// completion falls through to the failure branch exactly as a 200 does:
		// the caller asked for a chat completion, and a success status alone is
		// not one.
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.responseHeaderMs = responseHeaderMs
		logData.failoverAttempt = attempt
		logData.state = "completed"
		// Completed, and empty. recordAnswerOutcome credits the provider for a
		// completed answer unless told otherwise, so without this a relay
		// answering 204 to every request would be credited a breaker success
		// each time and its circuit could never open — the group would route to
		// a black hole for ever.
		//
		// deliveredContent is deliberately not set alongside it: it is already
		// false, and every site that sets it sits on a terminal path that cannot
		// precede this branch, so writing it here would be a line no mutation
		// could kill.
		logData.emptyCompletion = true
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		w.WriteHeader(resp.StatusCode)
		debuglog.Info("proxy: upstream answered with no content", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "duration_ms", totalDuration)
	default:
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.failoverLookupMs = failoverLookupMs
		logData.dialMs = dialMs
		logData.settingsReadMs = settingsReadMs
		logData.responseHeaderMs = responseHeaderMs
		logMsg, detail, kind, reason := nonStreamingFailureDetail(r.Context(), resp, body, readErr, decodeErr, logData.modelID)
		// body is already exact-masked; the log row also gets the key-shape
		// layer, like every other stored error message.
		logData.errorMessage = string(maskKeyShapedTokens([]byte(logMsg)))
		logData.errorKind = kind
		logData.failoverAttempt = attempt
		logData.state = "failed"
		// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "error_kind", kind, "model", logData.modelID, "provider", logData.providerName, "error", logData.content.maskOne(detail), "duration_ms", totalDuration)
		// The ROW keeps resp.StatusCode above: what the upstream said is the
		// diagnostic. Only what the CLIENT is told changes.
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
// of an error. A relay answering 200 with something that is not a completion
// therefore leaves the request log reading "failed" beside a client that
// believes it succeeded, and nothing retries and nothing alerts.
//
// 502 rather than any other code because the multimodal pass-through already
// answers exactly that for the same shape (serveBufferedJSONPassthrough on a
// broken read, serveStreamedPassthrough on an empty body). The upstream status
// still reaches the request-log row either way, and reaches the CLIENT only when
// the provider is named: upstreamClientMessage appends it to the message with
// the provider, and returns the bare reason without one.
//
// The non-2xx return is defensive rather than live. Every caller reaches this
// handler through attemptCandidate, which sends anything that is not a 2xx to
// forwardUpstreamError first (proxy_failover.go), so in production only the 2xx
// branch runs; the other keeps the function total for a handler driven directly.
//
// This is not reached for 204/205, which are served by their own branch, nor for
// a 2xx that decodes as a completion.
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

// readNonStreamingBody reads the upstream body once and decodes it, returning
// the bytes both branches below need, the decoded completion, and the two
// failures kept apart: what went wrong on the wire, and what went wrong parsing.
//
// The read is bounded (nonStreamingBodyCap) so one upstream cannot make the
// gateway buffer an arbitrary amount: cap+1 is read, and a body that reaches
// cap+1 is refused rather than decoded, because a truncated completion
// re-encoded as a valid one would hand the client silently mutilated content.
// That refusal is THIS gateway's policy and not the provider failing, so it is
// reported as a decode failure — folding it into readErr had an oversized answer
// reported as "the provider stopped sending its response" and its provider's
// circuit breaker charged for sending too much.
//
// json.Decoder, not json.Unmarshal: a decoder stops at the end of the first JSON
// value, so a completion with trailing bytes after it still decodes.
//
// The decode is attempted even when the read errored, because JSON is
// self-delimiting: a provider that sent the whole document and then dropped the
// connection without its terminal chunk yields ErrUnexpectedEOF from a body that
// parses perfectly. Discarding that threw away a complete answer and charged the
// provider for it. If the bytes really were cut short the decode fails too, and
// the read error is what gets reported. Same salvage rule as #810.
func readNonStreamingBody(resp *http.Response, masker credentialMasker) (body []byte, chatResp ChatCompletionResponse, readErr, decodeErr error) {
	body, readErr = io.ReadAll(io.LimitReader(resp.Body, nonStreamingBodyCap+1))
	if readErr == nil && len(body) > nonStreamingBodyCap {
		decodeErr = fmt.Errorf("upstream response exceeds the %d byte non-streaming body cap", nonStreamingBodyCap)
	}
	// Exact-key scrub on the whole body: the client answer and the failure log
	// message both derive from it, and a success body is content where the
	// key-shape regex must not run.
	body = masker.maskExact(body)
	if decodeErr != nil {
		return body, chatResp, readErr, decodeErr
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&chatResp); err != nil {
		if readErr != nil {
			return body, chatResp, readErr, readErr
		}
		return body, chatResp, readErr, err
	}
	// readErr is returned as it was rather than cleared. It is only ever
	// consulted alongside a decode failure — a clean decode means the 2xx branch
	// serves the answer and never looks at it — so clearing it claimed a
	// meaning it did not have.
	return body, chatResp, readErr, nil
}
