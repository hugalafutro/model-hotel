package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newRequestWithContext is injectable for testing request creation errors.
var newRequestWithContext = http.NewRequestWithContext

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
func nonStreamingFailureDetail(resp *http.Response, body []byte, decodeErr error, modelID string) (logMsg, detail string, kind ErrorKind, reason string) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
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
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, nonStreamingBodyCap+1))
	if readErr == nil && len(body) > nonStreamingBodyCap {
		readErr = fmt.Errorf("upstream response exceeds the %d byte non-streaming body cap", nonStreamingBodyCap)
	}
	// Exact-key scrub on the whole body: the client answer and the failure
	// log message both derive from it, and a success body is content where
	// the key-shape regex must not run.
	body = logData.masker.maskExact(body)
	var chatResp ChatCompletionResponse
	decodeErr := readErr
	if decodeErr == nil {
		decodeErr = json.NewDecoder(bytes.NewReader(body)).Decode(&chatResp)
	}

	// Only a 2xx that decodes is a completion. Some upstreams (OpenCode Zen and
	// OpenCode Go both do this) answer a failed request with a non-2xx carrying a
	// complete chat.completion envelope and no error object at all, which decodes
	// cleanly; forwarding that leaves the caller with a failure status and nothing
	// to read `.error.message` off. Status decides, the body only says whether the
	// success shape is even available.
	if decodeErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		var tps float64
		var reasoningTokens int
		if chatResp.Usage.CompletionTokensDetails != nil && chatResp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			reasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		totalOutputTokens := chatResp.Usage.CompletionTokens + reasoningTokens
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
		logData.tokensPrompt = chatResp.Usage.PromptTokens
		logData.tokensCompletion = chatResp.Usage.CompletionTokens
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
		// Fire-and-forget: skip WaitForInsert to avoid blocking TTFB.
		// The async INSERT is very likely complete by now; if not, the
		// UPDATE simply affects 0 rows (harmless, logged as warning).
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

		promptTokens, completionTokens, reasoningTokens := estimateMissingUsage(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, reasoningTokens, logData, chatAnswerBytes(chatResp))
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

		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			debuglog.Error("proxy: failed to encode response", "model", logData.modelID, "provider", logData.providerName, "error", err)
		}
		debuglog.Info("proxy: non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "duration_ms", totalDuration, "prompt_tokens", chatResp.Usage.PromptTokens, "completion_tokens", chatResp.Usage.CompletionTokens)
	} else {
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
		logMsg, detail, kind, reason := nonStreamingFailureDetail(resp, body, decodeErr, logData.modelID)
		// body is already exact-masked; the log row also gets the key-shape
		// layer, like every other stored error message.
		logData.errorMessage = string(maskKeyShapedTokens([]byte(logMsg)))
		logData.errorKind = kind
		logData.failoverAttempt = attempt
		logData.state = "failed"
		// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "error_kind", kind, "model", logData.modelID, "provider", logData.providerName, "error", detail, "duration_ms", totalDuration)
		writeOpenAIError(w, upstreamClientMessage(logData.providerName, resp.StatusCode, reason), resp.StatusCode)
	}
}

// failRequest populates logData with failure details and updates the request log.
// Always populates all timing fields from timings - if zero-valued, they record as 0ms.
// kind is the machine-readable classification (a required argument so no failure
// path can silently omit it); it is stored in request_logs.error_kind.
func (h *Handler) failRequest(logData *requestLogData, statusCode int, kind ErrorKind, errMsg string, attempt int, startTime time.Time, parseMs float64, timings resolveTimings, cacheHits resolveCacheHits, proxyOverhead float64) {
	logData.statusCode = statusCode
	logData.errorKind = kind
	logData.errorMessage = errMsg
	logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = timings.modelLookupMs
	logData.providerLookupMs = timings.providerLookupMs
	logData.keyDecryptMs = timings.keyDecryptMs
	logData.dialMs = timings.dialMs
	logData.failoverLookupMs = timings.failoverLookupMs
	logData.settingsReadMs = timings.settingsReadMs
	logData.cacheHits = cacheHits
	logData.failoverAttempt = attempt
	logData.state = "failed"
	// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
}

// ChatCompletions handles OpenAI-compatible chat completion requests with failover support.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	st, ok := h.ingestRequest(w, r, endpointTypeChat)
	if !ok {
		return
	}
	candidates, ok := h.resolveCandidates(w, r, st)
	if !ok {
		return
	}
	h.loadFailoverConfig(r, st)

	debuglog.Debug("proxy: model resolved (pre-loop)", "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "overhead_ms", st.proxyOverhead)

	// Request hedging (opt-in, streaming failover groups only): race a backup
	// provider's first-token probe instead of trying members strictly in
	// sequence. Everything else keeps the sequential failover loop unchanged.
	if st.hedgingEnabled && st.isStreaming && len(candidates) > 1 {
		h.runHedgedStreaming(w, r, st, candidates, h.probeStreamingCandidate)
		return
	}

	h.runFailoverLoop(w, r, st, candidates, h.attemptCandidate)
}

// attemptFn runs one failover attempt against a single candidate and reports
// whether the loop should try the next candidate (outcomeFailover) or stop.
// ChatCompletions uses attemptCandidate; the multimodal endpoints use
// attemptPassthroughCandidate.
type attemptFn func(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome

// runFailoverLoop drives the shared failover loop (phase D): the overall
// deadline check, exponential backoff between attempts, one attempt call per
// candidate, and the all-exhausted failure path.
func (h *Handler) runFailoverLoop(w http.ResponseWriter, r *http.Request, st *requestState, candidates []modelCandidate, attemptOne attemptFn) {
	for attempt, candidate := range candidates {
		// Overall deadline check: stop failover if the total time budget
		// across all candidates has been exceeded. This prevents N candidates
		// from holding a goroutine for N×failoverTimeout when the client
		// is silent but connected (no TCP reset).
		if time.Now().After(st.overallDeadline) && attempt > 0 {
			debuglog.Warn("proxy: overall request deadline exceeded, stopping failover", "model", st.logData.modelID, "attempt", attempt+1, "total_candidates", len(candidates), "deadline", st.overallDeadline)
			st.setReqErr(reqError{Kind: KindFailoverTimeout, Attempt: attempt - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
			break
		}

		// Exponential backoff between failover attempts: 0ms, ~100ms, ~200ms, ~400ms...
		// Capped at 2s, with ±50ms jitter to avoid thundering herd.
		// First attempt (attempt=0) has no delay.
		if attempt > 0 {
			backoff := failoverBackoff(100*time.Millisecond, 2*time.Second, attempt)
			debuglog.Info("proxy: failover backoff", "backoff", backoff, "attempt", attempt+1)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				// If the prior attempt was a zero-token provider stall, the
				// silent connection was most likely dropped by an intermediary
				// (reverse proxy / LB / CDN) idle-read timeout rather than the
				// client. Preserve the provider stall as the terminal cause
				// instead of overwriting it with a client disconnect.
				if st.lastReqErr.Kind == KindProviderTimeout {
					status := st.lastReqErr.terminalStatus()
					// Only `attempt` providers were actually contacted before the
					// connection dropped during this backoff; the remaining
					// candidates were never tried. Pass the attempted count (not
					// len(candidates)) so the log does not claim "all N providers
					// failed" when only the first stalled.
					logMsg := st.lastReqErr.terminalLogMessage(st.isFailover, attempt)
					clientMsg := st.lastReqErr.terminalClientMessage(st.reqModel, st.isFailover)
					debuglog.Info("proxy: connection closed during failover backoff after provider stall", "model", st.logData.modelID, "provider", st.logData.providerName, "attempt", attempt+1, "kind", string(st.lastReqErr.Kind), "status", status)
					h.failRequest(st.logData, status, st.lastReqErr.Kind, logMsg, attempt-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
					writeOpenAIError(w, clientMsg, status)
					return
				}
				debuglog.Info("proxy: client disconnected during failover backoff", "model", st.logData.modelID, "provider", st.logData.providerName, "attempt", attempt+1)
				// Carry the prior attempt's provider error (if any) so the log
				// shows what was failing when the client gave up. 499 (client
				// closed request) on both the log and the wire — see plan §7.
				st.setReqErr(reqError{Kind: KindClientDisconnect, Attempt: attempt - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
				h.failRequest(st.logData, statusClientClosedRequest, KindClientDisconnect, st.lastErr, attempt-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
				writeOpenAIError(w, "client disconnected", statusClientClosedRequest)
				return
			}
		}

		// One failover attempt. The attempt fn owns its request contexts
		// (cancelled via defer after body consumption) and reports whether to
		// try the next candidate, that the response was served, or that a
		// terminal error was written.
		if attemptOne(w, r, st, candidate, attempt, len(candidates)) != outcomeFailover {
			return
		}
	}

	h.failAllExhausted(w, st, len(candidates))
}

// probeFirstToken reads from body until it finds the first real SSE data chunk
// or the timeout fires. It returns a buffer containing all bytes read (for
// replay via io.MultiReader), the true time-to-first-token in milliseconds,
// and any error.
//
// A "real data chunk" is any "data:" line where the content after "data:" is
// not "[DONE]". Keepalive comments (":"), empty lines, "event:", "id:", and
// "retry:" directives are skipped but still captured in probeBuf for replay.
func (h *Handler) probeFirstToken(
	ctx context.Context,
	body io.ReadCloser,
	ttftTimeout time.Duration,
	startTime time.Time,
) (probeBuf *bytes.Buffer, trueTtftMs float64, err error) {
	probeCtx, probeCancel := context.WithTimeout(ctx, ttftTimeout)
	defer probeCancel()

	// Signal the goroutine when the probe finishes, so it doesn't close
	// the body after a successful read. Closed explicitly on success paths
	// and via sync.Once/defer on all paths.
	probeDone := make(chan struct{})
	var closeProbeOnce sync.Once
	closeProbe := func() { closeProbeOnce.Do(func() { close(probeDone) }) }
	defer closeProbe()

	// Atomic flag set the instant a data line is detected, before any
	// string processing. The goroutine checks this as a last guard before
	// closing the body, closing a narrow race where the timer fires at the
	// same instant the scanner returns a data line.
	var probeSucceeded atomic.Bool

	// Goroutine closes body when the probe context is cancelled (TTFT timeout
	// or parent context cancellation), unblocking the scanner. The double-
	// check of probeDone handles the narrow race where the probe succeeds
	// at the same instant the context fires; probeSucceeded is the final
	// guard to prevent closing a body that's about to be replayed.
	go func() {
		select {
		case <-probeDone:
			// Probe finished — don't touch the body.
			return
		case <-probeCtx.Done():
			// Double-check: probe may have just finished between the
			// outer select and here.
			select {
			case <-probeDone:
				return
			default:
			}
			if !probeSucceeded.Load() {
				_ = body.Close()
			}
		}
	}()

	var buf bytes.Buffer
	tee := io.TeeReader(body, &buf)
	scanner := bufio.NewScanner(tee)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines, keepalive comments, and non-data directives.
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			// Signal the goroutine immediately — a data line was found,
			// the provider is healthy. This must happen before any
			// string processing so the goroutine sees it even if the
			// timer fires at the same instant.
			probeSucceeded.Store(true)
			content := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if content == "[DONE]" {
				// Stream ended before any real token.
				debuglog.Info("proxy: TTFT probe saw [DONE] before first token", "ttft_ms", float64(time.Since(startTime).Microseconds())/1000.0)
				closeProbe()
				return &buf, 0, nil
			}
			// First real data chunk found.
			ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
			debuglog.Info("proxy: TTFT probe found first token", "ttft_ms", ttft)
			closeProbe()
			return &buf, ttft, nil
		}
		// Unknown line format — skip but captured in buf.
	}

	// Scanner exited — body closed (timeout) or read error.
	// bufio.Scanner never returns io.EOF from Err(); on clean EOF,
	// Scan() returns false with Err() == nil, handled by the fallback
	// after this block.
	if scanErr := scanner.Err(); scanErr != nil {
		// Race recovery: the goroutine may close the body between the
		// scanner reading a complete data line and probeSucceeded being
		// checked. TeeReader writes to buf before scanner.Scan() returns,
		// so the data is captured. Only return success if the probe context
		// is still valid — if it expired, the goroutine closed the body and
		// returning success would give the caller a closed body, causing
		// handleStreamingResponse to truncate the stream after buffer replay.
		if probeCtx.Err() == nil {
			probeSucceeded.Store(true) // mirror line 1680: store before any processing
			bufStr := buf.String()
			for rawLine := range strings.SplitSeq(bufStr, "\n") {
				if l := strings.TrimSpace(rawLine); strings.HasPrefix(l, "data:") {
					// Reject partial lines: a complete SSE line must be
					// followed by \n in the buffer. Without this guard a
					// mid-line network fragment like "data: hel" (no \n)
					// would pass HasPrefix but represent malformed data.
					if !strings.Contains(bufStr, rawLine+"\n") {
						continue
					}
					content := strings.TrimSpace(strings.TrimPrefix(l, "data:"))
					if content != "[DONE]" {
						ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
						debuglog.Info("proxy: TTFT probe recovered data after scanner error", "ttft_ms", ttft, "scan_error", scanErr)
						return &buf, ttft, nil
					}
				}
			}
		}
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, 0, fmt.Errorf("TTFT timeout: no first token within %s", ttftTimeout)
		}
		return nil, 0, fmt.Errorf("TTFT probe read error: %w", scanErr)
	}

	// Scanner finished without error and without finding data — body EOF.
	return nil, 0, fmt.Errorf("TTFT probe: body closed before first data chunk")
}

// See util.BuildProviderTargetURL for URL construction and util.SetProviderAuthHeaders for auth.

// mapKeys returns the keys of a map[string]bool for logging.
// failoverBackoff calculates exponential backoff with jitter between failover attempts.
// base is the starting delay, capacity is the maximum delay, attempt is the 1-indexed attempt number.
// Jitter of [0, base) is added to spread retries from concurrent requests hitting the same cascade.
func failoverBackoff(base, capacity time.Duration, attempt int) time.Duration {
	exp := min(time.Duration(float64(base)*math.Pow(2, float64(attempt-1))), capacity)
	jitter := time.Duration(rand.Int64N(int64(base)))
	return exp + jitter
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// writeOpenAIError writes an OpenAI-compatible JSON error response.
// All proxy error responses must be JSON, not plain text, because clients like
// SillyTavern parse responses as JSON and crash on plain text error messages.
func writeOpenAIError(w http.ResponseWriter, message string, statusCode int) {
	util.WriteOpenAIError(w, message, statusCode)
}

// humanReadableCancelOrigin maps internal cancel origin identifiers to
// human-readable descriptions for error messages and request logs.
// Raw Go errors like "context canceled" and "context deadline exceeded" are
// opaque — callers need to know whether the client disconnected, the failover
// timeout expired, or a param-strip retry timed out.
func humanReadableCancelOrigin(origin string) string {
	switch origin {
	case "client_disconnect":
		return "client disconnected"
	case "failover_timeout":
		return "upstream request timed out"
	case "retry_timeout":
		return "param-strip retry timed out"
	case "hedge_superseded":
		return "superseded by a faster hedged attempt"
	default:
		return origin
	}
}
