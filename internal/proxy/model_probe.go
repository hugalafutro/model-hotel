package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// goneProbeMaxTokens is the probe's output budget. Deliberately not 1: a
// reasoning model spends a 1-token budget on thinking, returns an empty
// completion, and some providers wrap that in a 400 — a manufactured false
// failure.
//
// Best-effort, and knowingly so. The probe goes through the real egress builder
// (see newProbeState), which strips params this provider+model has LEARNED the
// upstream rejects, so a model that once answered 400 for max_tokens is probed
// with no cap at all. Keeping the cap would mean a bespoke body that skips the
// rewrite, and a probe that is easier to satisfy than the requests it
// adjudicates is worse than no probe. The cost is one uncapped completion per
// retirement decision, at goneProbeCooldown apiece.
const goneProbeMaxTokens = 64

// goneProbeMaxBody bounds how much of a probe answer is read into memory.
//
// Applied to resp.Body itself rather than at each read site: remapMiniMaxBusinessError
// and both dialect translators consume the body whole from other packages, so a
// cap that only covered the judgement functions would leave every MiniMax,
// vertex-express and Responses probe unbounded. The read happens on a detached
// goroutine with no client backpressure behind it.
//
// A megabyte rather than the few hundred bytes a refusal needs, because the
// embeddings family shares the constant: a 3072-dimension embedding at full
// float precision runs past 60 KiB, so a tighter cap would truncate legitimate
// answers from exactly the models being adjudicated.
//
// The value is not load-bearing for correctness in either direction, which is
// what judgeProbeFailure's explicit length check is for: a truncated body
// postpones because it was measured to be truncated.
const goneProbeMaxBody = 1 << 20

// goneProbeTimeout bounds the pre-retirement probe. It runs on the detached
// disable goroutine, so nothing on the request path is waiting on it.
//
// probeModel does NOT apply this itself. Taking the context from the caller lets
// a test drive the real code with a millisecond deadline, and keeps the
// production budget at the call site that owns the decision.
const goneProbeTimeout = 30 * time.Second

// The two upstream surfaces a retirement probe is allowed to touch. Both are
// the OpenAI-compatible spelling; the egress builder re-routes them per
// provider dialect, which is the whole reason the probe goes through it.
const (
	probeChatEndpoint       = "/chat/completions"
	probeEmbeddingsEndpoint = "/embeddings"
)

// probeVerdict is what a real request to the model proves about whether the
// provider still serves it.
//
// Three-way, and the zero value is the one that matters: every path that cannot
// establish anything — an unprobeable family, a request that would not build, a
// connection that never landed, a deadline, a body that will not parse — lands
// on probeInconclusive, and the caller postpones the retirement on it.
type probeVerdict int

const (
	// probeInconclusive: the probe proved nothing. Postpone.
	probeInconclusive probeVerdict = iota
	// probeRefused: the provider refused the model by name. Retire.
	probeRefused
	// probeServed: the model answered with content. Do not retire.
	probeServed
)

// String names the verdict for logs. Call sites pass the result of this rather
// than the value: debuglog's arguments are evaluated eagerly, so an explicit
// conversion keeps the rendered name independent of whether Debug is enabled.
//
// The default renders the number rather than folding into "inconclusive",
// because noteModelGone's default arm exists to surface a verdict nobody has
// handled and logs this string.
func (v probeVerdict) String() string {
	switch v {
	case probeInconclusive:
		return "inconclusive"
	case probeRefused:
		return "refused"
	case probeServed:
		return "served"
	default:
		return "unknown(" + strconv.Itoa(int(v)) + ")"
	}
}

// probeEndpointForFamily reports which upstream endpoint can cheaply prove
// whether a model of this endpoint family is still served, and whether one
// exists at all.
//
// Only chat and embeddings qualify, and the exclusions are the point. A chat
// probe against an image, TTS or STT model fails for reasons that have nothing
// to do with retirement, and that failure would read as confirmation of the
// retirement it was supposed to adjudicate. Those probes also cost real money
// and real seconds per call.
//
// Rerank is excluded on weaker grounds: a /rerank call with one document is as
// cheap as an embeddings call, but it would need its own request body, answer
// shape and content judgement, and shipping an unexercised third judgement path
// is a worse trade than leaving rerank models enabled until an operator disables
// one. Cheap to reverse: add the family here plus a body and a content check.
//
// Where a family cannot be probed cheaply, the correct outcome is to not
// auto-retire that family at all. Falling back to the prose classifier would
// leave the retirement running unsupervised precisely where it is least
// observed.
//
// The empty string and any unknown family are unprobeable for the same reason:
// nothing can be substantiated about them, so nothing is claimed.
func probeEndpointForFamily(endpointType string) (endpoint string, ok bool) {
	switch endpointType {
	case endpointTypeChat, endpointTypeMessages:
		return probeChatEndpoint, true
	case endpointTypeEmbeddings:
		return probeEmbeddingsEndpoint, true
	default:
		return "", false
	}
}

// cappedBody is a response body bounded by goneProbeMaxBody whose Close still
// closes the transport's own body underneath.
//
// The Closer half is the point: an io.NopCloser would leave nothing holding the
// real body, and every reader downstream — including the two in other packages —
// would read through a cap while the connection leaked.
type cappedBody struct {
	io.Reader
	io.Closer
}

// probeChatRequest is the minimal chat body the probe sends. It reuses the
// package's Message so the shape cannot drift from what the rest of the proxy
// considers a chat message.
type probeChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

// probeEmbeddingsRequest is the minimal embeddings body the probe sends.
type probeEmbeddingsRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// newProbeState builds the synthetic requestState the probe hands to the real
// egress builder.
//
// The state is synthetic but the path is not. Provider-type detection, the
// target-URL rules, the Responses and vertex-express Gemini re-routes and the
// param rewrite all have to run, because a bespoke request builder could succeed
// where real traffic fails.
//
// Three fields are load-bearing by their absence:
//
//   - anthropicIn/anthropicRawBody are NOT set, so an endpointTypeMessages
//     candidate on an anthropic provider is probed with an OpenAI chat body at
//     {base}/v1/chat/completions rather than natively at /v1/messages.
//     anthropicRawBody is the CLIENT's Messages body and a probe has no client,
//     so the native path would have to be handed an invented one. Anthropic's
//     compat layer serves the same catalog, so a live model answers on both
//     surfaces and a retired one is refused by name on both; if the compat layer
//     itself is the problem the probe draws an error that is not a retirement and
//     returns probeInconclusive.
//   - endpointPath is left empty for the chat families even though
//     probeEndpointForFamily names "/chat/completions". Empty IS chat to the
//     builder, while a non-empty endpointPath means "this is a multimodal
//     endpoint" and both dialect re-routes refuse to fire when it is set.
//   - reqModel is left empty. It is the model the CLIENT asked for, and the
//     builder's rewrite gate is `st.reqModel != candidate.model.ModelID || ...`
//     (proxy_failover.go), so naming the upstream model here closes every
//     disjunct and paramrewrite.BuildUpstreamBody never runs. The probe would
//     then skip the learned renames real traffic gets — an OpenAI reasoning model
//     that has learned max_tokens -> max_completion_tokens would be probed with
//     the very parameter it rejects and answer 400 forever.
//
// The marshal errors are discarded because both payloads are fixed structs of
// strings and ints: encoding/json has no failure to report, and threading an
// error nothing can produce would add a branch no test could reach.
func newProbeState(candidate modelCandidate, endpointType, endpoint string) *requestState {
	modelID := candidate.model.ModelID
	st := &requestState{
		logData: &requestLogData{
			modelID: modelID,
			// The builder logs the provider on its rewrite-check line; without
			// this every probe would report an empty one.
			providerName: candidate.provider.Name,
			endpointType: endpointType,
		},
	}

	if endpoint == probeEmbeddingsEndpoint {
		body, _ := json.Marshal(probeEmbeddingsRequest{Model: modelID, Input: "hi"})
		st.endpointPath = endpoint
		// The multimodal endpoints own their body rewrite through
		// makeUpstreamBody (see multimodal.go); the probe does the same, so the
		// id in the body is the one the builder resolved.
		st.makeUpstreamBody = func(resolvedModelID string) ([]byte, string, error) {
			resolved, _ := json.Marshal(probeEmbeddingsRequest{Model: resolvedModelID, Input: "hi"})
			return resolved, "application/json", nil
		}
		// Set to the same payload even though makeUpstreamBody supersedes it:
		// several things downstream of the builder read the request body, and
		// none of them should be looking at an empty one.
		st.bodyBytes = body
		return st
	}

	body, _ := json.Marshal(probeChatRequest{
		Model:     modelID,
		Messages:  []Message{{Role: "user", Content: "hi"}},
		MaxTokens: goneProbeMaxTokens,
	})
	st.bodyBytes = body
	return st
}

// probeModel asks the provider directly whether it still serves this model.
//
// classifyUpstreamError reads the words a provider chose on the day it wrote
// them; this reads what the provider actually does when asked for the model. The
// probe is the adjudicator, the classifier only nominates.
//
// It is an upstream call the operator did not make, so it is deliberately NOT
// run through doUpstream: no circuit-breaker outcome, no request log, no
// metering, no rate-limit accounting. Charging a provider's breaker for a
// verification would let the verification take a healthy provider out of
// routing, and metering it would bill an operator for traffic they never sent.
//
// The caller owns the deadline (see goneProbeTimeout) and the decision; this
// returns evidence only.
func (h *Handler) probeModel(ctx context.Context, candidate modelCandidate, endpointType string) probeVerdict {
	endpoint, ok := probeEndpointForFamily(endpointType)
	if !ok {
		// Defensive: the caller gates on the family before spending a request.
		// Reaching here means it did not, and the honest answer is still that
		// nothing was established.
		return probeInconclusive
	}
	// Kept although probeForRetirement checks first, because this function takes
	// a candidate and dereferences it. The tests drive probeModel directly, which
	// is also how any future caller would.
	if candidate.model == nil || candidate.provider == nil {
		return probeInconclusive
	}

	// Skipping the breaker's ACCOUNTING is argued above; skipping its CHECK is a
	// separate decision and not one this makes. If the gateway has already
	// sidelined this provider, a probe to it is a guaranteed-wasted call whose
	// answer would be inconclusive anyway.
	//
	// GetState rather than IsOpen, and the difference is what makes the check
	// safe. IsOpen is the routing gate: it takes a write lock and performs the
	// Open->HalfOpen transition, spending the provider's one half-open trial slot
	// on a request the operator did not make. GetState takes a read lock and
	// derives the same logical state without touching the circuit, so an open
	// circuit past its cooldown already reads as half-open here. A nil breaker
	// means nobody has an opinion, which is not a reason to postpone.
	//
	// Unlike every other breaker consultation in the proxy this gate does not
	// also check the circuit_breaker_enabled setting, because reading it means
	// calling h.settingsRepo.GetBool and h.settingsRepo is nil on the path the
	// probe unit tests exercise. The failure mode is bounded and safe: an
	// operator who disables the breaker after a circuit has opened sees that
	// provider's probes deferred until the breaker's own cooldown clears, and a
	// deferred probe can only leave a model enabled longer. If a later change
	// threads a testable settings source through here, honor the setting then.
	if h.circuitBreaker != nil && h.circuitBreaker.GetState(candidate.provider.ID) == failover.StateOpen {
		debuglog.Debug("proxy: retirement probe skipped, the provider's circuit is open", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "verdict", probeInconclusive.String())
		return probeInconclusive
	}

	st := newProbeState(candidate, endpointType, endpoint)

	req, providerType, _, err := h.buildCandidateRequest(ctx, st, candidate)
	if err != nil {
		debuglog.Debug("proxy: retirement probe could not build its request", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "error", err)
		return probeInconclusive
	}

	// The shared upstream transport, with the same redirect guard real traffic
	// gets: a provider that redirects must not be able to walk this request's
	// credential to another host just because it arrived off the request path.
	//
	// The nil check has to exist — upstreamTransport is a concrete
	// *http.Transport, so assigning a nil one produces a non-nil interface
	// holding a nil pointer and net/http panics inside RoundTrip — and leaving
	// the field unset instead is not the harmless alternative: an unset Transport
	// IS http.DefaultTransport, which carries no DialContext, so the SafeDialer's
	// guard against a provider URL resolving into the gateway's own network would
	// be silently absent for exactly the request nobody is watching.
	if h.upstreamTransport == nil {
		debuglog.Debug("proxy: retirement probe has no upstream transport to make a guarded request with", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "verdict", probeInconclusive.String())
		return probeInconclusive
	}
	client := &http.Client{Transport: h.upstreamTransport}
	if h.safeDialer != nil {
		client.CheckRedirect = h.safeDialer.CheckRedirect
	}

	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	resp, err := client.Do(req)
	if err != nil {
		// A connection that never landed, a DNS failure or an expired deadline
		// says nothing about the model. This branch is what keeps a network
		// problem on the gateway's side from retiring a provider's whole catalog.
		debuglog.Debug("proxy: retirement probe did not reach the provider", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "verdict", probeInconclusive.String(), "error", err)
		return probeInconclusive
	}
	// The cap goes on the body here, once, and everything past this line reads
	// through it. rawBody is held separately because resp.Body is replaced here
	// and replaced again by remapMiniMaxBusinessError, and the drain below has to
	// read the transport's own body rather than whichever wrapper is in the field
	// by then. The close can stay on the field because cappedBody passes Close
	// through.
	//
	// One byte past the cap, so a truncated body can be recognised as truncated:
	// io.ReadAll reports no error when a LimitReader runs out, so length is the
	// only signal there is (see judgeProbeFailure).
	rawBody := resp.Body
	resp.Body = cappedBody{Reader: io.LimitReader(rawBody, goneProbeMaxBody+1), Closer: rawBody}
	defer func() {
		// Drain before closing so the transport can reuse the connection, and
		// drain no more than the judgement was willing to read. A body past
		// goneProbeMaxBody costs the connection its reuse and nothing else.
		//
		// This bounds the copy, not the total: the judgement has already taken
		// its own goneProbeMaxBody, so a large answer is pulled off the socket
		// twice. What the constant promises is memory, and that still holds —
		// this streams to io.Discard through one 32 KiB buffer.
		//
		// Knowingly a no-op on some paths: MiniMax and both dialect translators
		// read the body to EOF and CLOSE it themselves, so there this copy reads
		// an already-closed body and discards the error. A body those paths could
		// finish was already drained by them, and one they could not finish is
		// past the cap, where the connection is forfeit either way.
		_, _ = io.Copy(io.Discard, io.LimitReader(rawBody, goneProbeMaxBody))
		// Whatever is in the field: the cap above on every path, or the NopCloser
		// remapMiniMaxBusinessError leaves behind — and that path has already
		// closed the cap, and through it the transport's own body. The real body
		// is closed exactly once either way.
		_ = resp.Body.Close()
	}()

	// MiniMax reports refusals inside a 200 envelope, so the status has to be
	// normalised before it is judged — exactly as attemptCandidate does.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)

	if resp.StatusCode != http.StatusOK {
		return judgeProbeFailure(resp, candidate, endpointType)
	}
	return judgeProbeSuccess(resp, st, candidate, endpointType)
}

// judgeProbeFailure turns a non-200 probe response into a verdict.
//
// Only the classifier's retirement verdict retires. Everything else postpones,
// and that single line is what keeps a provider incident from becoming a mass
// retirement: 429s, 402s, quota and entitlement failures, bad-request verdicts
// and every 5xx are things that happen to models that are alive. Deliberately
// no status-code shortcut around the classifier — a bare "404 means gone" would
// retire every model behind a misconfigured base URL.
func judgeProbeFailure(resp *http.Response, candidate modelCandidate, endpointType string) probeVerdict {
	// One byte past the cap on purpose. This is the one place in this file where
	// a body we did not receive in full could RETIRE a model: a surviving prefix
	// that names the model beside a gone-phrase classifies exactly like a real
	// refusal, and the classifier only sees the first 10 000 characters.
	//
	// Reading exactly goneProbeMaxBody could not tell either: io.ReadAll returns
	// a nil error when a LimitReader is exhausted, so the err branch below catches
	// genuine transport failures and never catches truncation.
	body, err := io.ReadAll(io.LimitReader(resp.Body, goneProbeMaxBody+1))
	switch {
	case err != nil:
		// A body we could not finish reading is not the provider saying
		// anything, so it postpones like every other unproven case.
		debuglog.Debug("proxy: retirement probe could not read the provider's answer", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "status", resp.StatusCode, "verdict", probeInconclusive.String(), "error", err)
		return probeInconclusive
	case len(body) > goneProbeMaxBody:
		// Over the cap: what is in hand is a prefix of an answer whose real
		// content is unknown. Postpone rather than classify it.
		debuglog.Debug("proxy: retirement probe answer exceeded the read cap", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "status", resp.StatusCode, "verdict", probeInconclusive.String(), "max_bytes", goneProbeMaxBody)
		return probeInconclusive
	}
	kind, _ := classifyUpstreamError(resp.StatusCode, util.SanitizeLogBody(string(body), 10000), candidate.model.ModelID)

	verdict := probeInconclusive
	if kind == KindProviderModelGone {
		verdict = probeRefused
	}
	// Never the body, never the key: this is a provider response and the
	// no-content rule applies to it exactly as it does on the request path.
	debuglog.Debug("proxy: retirement probe drew an error status", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "status", resp.StatusCode, "error_kind", string(kind), "verdict", verdict.String())
	return verdict
}

// judgeProbeSuccess turns a 200 probe response into a verdict.
//
// A 200 that carries nothing is NOT a success: a stream can open, emit nothing
// and end cleanly. It is not a refusal either — the provider did not say the
// model is gone — so an empty answer postpones like every other unproven case.
func judgeProbeSuccess(resp *http.Response, st *requestState, candidate modelCandidate, endpointType string) probeVerdict {
	// The dialect re-routes answer in their own wire format. Translate first, for
	// the same reason attemptCandidate does: everything downstream judges the
	// chat-completions shape. An answer that cannot be translated is not a
	// refusal, so it postpones.
	if err := translateProbeDialect(resp, st, candidate.model.ModelID); err != nil {
		debuglog.Debug("proxy: retirement probe could not read the provider's dialect", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "error", err)
		return probeInconclusive
	}

	// Both the read error and the cap are ignored here, unlike in
	// judgeProbeFailure, because of what an incomplete body can produce. There, a
	// surviving prefix naming the model beside a gone-phrase would RETIRE, so it
	// has to be detected; here the worst a partial body can do is fail to parse
	// (probeDeliveredContent returns false and the probe postpones) or carry
	// content, which can only PREVENT a disable.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, goneProbeMaxBody))
	verdict := probeInconclusive
	if probeDeliveredContent(endpointType, body) {
		verdict = probeServed
	}
	debuglog.Debug("proxy: retirement probe answered", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "status", resp.StatusCode, "verdict", verdict.String())
	return verdict
}

// translateProbeDialect converts a dialect answer back to the chat-completions
// shape the judgement understands, mirroring what attemptCandidate does on the
// served path. Which flag is set was decided by buildCandidateRequest, so this
// stays correct as the re-route rules move.
//
// Only the non-streaming translators are reachable: the probe never asks for a
// stream. The Responses case cannot fire as the probe body stands — that
// re-route also requires tools in the request — and neither can the Anthropic
// egress case, whose re-route requires a document part the probe body has no
// reason to carry. Both are kept because the flag is set by
// buildCandidateRequest and not by anything here. If the re-route rules widen, a
// probe that skipped the translation would read a dialect object as an empty
// chat completion and postpone those retirements forever, silently.
func translateProbeDialect(resp *http.Response, st *requestState, modelID string) error {
	// The upstream model id, NOT st.reqModel: the served path passes the name the
	// client asked for, and a probe has no client (st.reqModel is empty by design,
	// see newProbeState).
	switch {
	case st.responsesAttempt:
		return translateResponsesResponseBody(resp, modelID)
	case st.geminiAttempt:
		return translateEgressResponseBody(resp, modelID, gemini.BuildChatCompletion)
	case st.anthropicEgressAttempt:
		return translateEgressResponseBody(resp, modelID, anthropicegress.BuildChatCompletion)
	default:
		return nil
	}
}

// probeDeliveredContent reports whether a 200 probe response actually carried
// something the model produced.
//
// A body that will not parse returns false, which postpones rather than retires:
// an unreadable answer is not the provider saying the model is gone.
func probeDeliveredContent(endpointType string, body []byte) bool {
	if endpointType == endpointTypeEmbeddings {
		// The vector is left undecoded on purpose. Typing it as []float64 made
		// the whole document fail to parse when a provider answered with
		// base64-encoded embeddings — a JSON string where the struct wanted an
		// array — so a live embeddings model came back inconclusive forever.
		//
		// Raw and non-empty is still a real bar: an absent vector, a null, an
		// empty array and an empty string are all "the provider answered with
		// nothing". What it stops doing is judging the ENCODING, which is the
		// provider's choice and says nothing about whether the model exists.
		var out struct {
			Data []struct {
				Embedding json.RawMessage `json:"embedding"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &out) != nil || len(out.Data) == 0 {
			return false
		}
		return !jsonValueIsEmpty(out.Data[0].Embedding)
	}

	var out ChatCompletionResponse
	if json.Unmarshal(body, &out) != nil {
		return false
	}
	return chatAnswerCarriesContent(out)
}

// jsonValueIsEmpty reports whether a raw JSON value carries nothing: absent,
// null, an array with no elements, or a string with no characters.
//
// Structural rather than a comparison against the spellings encoding/json
// happens to emit. `[]` and `[ ]` are the same empty array, and the
// exact-string version of this read `[ ]` as content.
func jsonValueIsEmpty(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return true
	}
	if len(v) >= 2 && (v[0] == '[' && v[len(v)-1] == ']' || v[0] == '"' && v[len(v)-1] == '"') {
		return len(bytes.TrimSpace(v[1:len(v)-1])) == 0
	}
	return false
}

// chatAnswerCarriesContent reports whether a decoded chat completion carries
// something the model produced.
//
// Shared with the non-streaming request path, which asks the same question of
// the same shape for the same reason: a 200 that carries nothing is not the
// model answering, and crediting one would let a provider intermittently
// returning an empty completion reset a retired model's strike count forever.
// One judgement rather than two, so the probe and the traffic cannot drift.
//
// The completion-token fallback is what admits a reasoning model that
// legitimately spends the whole budget thinking and returns an empty visible
// answer.
func chatAnswerCarriesContent(out ChatCompletionResponse) bool {
	for _, choice := range out.Choices {
		if s, ok := choice.Message.Content.(string); ok && s != "" {
			return true
		}
		// Content is `any` because a provider may answer with content PARTS
		// rather than a string. Non-empty is the whole test — the parts are the
		// model's output whatever their shape, and reaching into them to find a
		// non-empty text field would be judging providers' part vocabularies from
		// here.
		if parts, ok := choice.Message.Content.([]any); ok && len(parts) > 0 {
			return true
		}
		if choice.Message.ReasoningContent != "" || choice.Message.Reasoning != "" {
			return true
		}
		if len(choice.Message.ToolCalls) > 0 {
			return true
		}
	}
	return out.Usage.CompletionTokens > 0
}
