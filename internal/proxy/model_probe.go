package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// goneProbeMaxTokens is the probe's output budget. Deliberately not 1: a
// reasoning model spends a 1-token budget on thinking, returns an empty
// completion, and some providers wrap that in a 400 — a manufactured false
// failure that cost half a day during the July catalog audit.
//
// Best-effort, and knowingly so. The probe goes through the real egress builder
// (see newProbeState), whose step 6 strips params this provider+model has
// LEARNED the upstream rejects. A model that once answered 400 for max_tokens
// outright therefore gets probed with no cap at all and answers at whatever
// length it likes. That is the right trade rather than a gap to close: the
// alternative is a bespoke body that keeps the cap by skipping the rewrite, and
// a probe that is easier to satisfy than the requests it adjudicates is worse
// than no probe. The cost is bounded anyway — one uncapped completion per
// retirement decision, at goneProbeCooldown apiece.
const goneProbeMaxTokens = 64

// goneProbeMaxBody bounds how much of a probe answer is read into memory.
//
// The read happens on the detached disable goroutine with no client backpressure
// behind it, so nothing downstream would notice a provider (or something
// impersonating one) streaming megabytes into it. probeModel applies this to
// resp.Body itself rather than at each read site, because the reads are not all
// in this file: remapMiniMaxBusinessError and both dialect translators consume
// the body whole, and a cap that only covered the two judgement functions would
// have left every MiniMax, vertex-express and Responses probe unbounded.
//
// A megabyte rather than the couple of hundred bytes a chat refusal or a
// 64-token completion actually needs, because the embeddings family shares the
// constant: a 3072-dimension embedding serialised at full float precision runs
// past 60 KiB, so a tighter cap would truncate legitimate answers from exactly
// the models this probe is supposed to adjudicate. Still orders of magnitude
// below anything that matters to the process, at one probe per model per
// goneProbeCooldown.
//
// The value is not load-bearing for correctness in either direction, which is
// the point of judgeProbeFailure's explicit length check: a truncated body
// postpones because it was measured to be truncated, not because 1 MiB happens
// to sit above the classifier's own 10 000-character window.
const goneProbeMaxBody = 1 << 20

// goneProbeTimeout bounds the pre-retirement probe. It runs on the detached
// disable goroutine, so nothing on the request path is waiting on it.
//
// probeModel does NOT apply this itself. Taking the context from the caller is
// what lets a test drive the real code with a deadline measured in
// milliseconds, and it keeps the production budget at the single call site that
// owns the decision rather than buried in the helper.
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
// on probeInconclusive, and the caller postpones the retirement on it. Nothing
// is claimed from evidence that was never gathered.
type probeVerdict int

const (
	// probeInconclusive: the probe proved nothing. Postpone.
	probeInconclusive probeVerdict = iota
	// probeRefused: the provider refused the model by name. Retire.
	probeRefused
	// probeServed: the model answered with content. Do not retire.
	probeServed
)

// String names the verdict for logs. The call sites pass the result of this
// rather than the value itself: debuglog's arguments are evaluated eagerly, so
// an explicit conversion keeps the rendered name independent of whether the
// Debug scope happens to be enabled.
//
// probeInconclusive has its own case and the default renders the number, which
// is the opposite of the obvious arrangement and is the point. noteModelGone's
// default arm exists specifically to surface a verdict nobody has handled, and
// it logs this string; folding unknown values into "inconclusive" meant the one
// diagnostic designed to name the unknown value was guaranteed to misname it,
// and a fourth verdict would have been indistinguishable in the logs from the
// zero value it is not.
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
// Only chat and embeddings qualify, and the exclusions are the point rather
// than an omission. A chat probe against an image, TTS or STT model fails for
// reasons that have nothing to do with the model being retired, and that
// failure would read as confirmation of the retirement it was supposed to
// adjudicate — the probe would manufacture exactly the wrong answer. Image,
// speech and transcription probes also cost real money and real seconds per
// call, which a background verification must not spend.
//
// Rerank is excluded on a different and weaker ground, recorded here so nobody
// reads it as belonging to the sentence above. A /rerank call with one document
// is about as cheap as the embeddings call this function already allows, so the
// cost argument does not apply to it. It is excluded because a rerank probe
// would need its own request body, its own answer shape and its own "did the
// model actually produce something" judgement, none of which exist yet and none
// of which anything has asked for — and shipping an unexercised third judgement
// path is a worse trade than the consequence, which is that rerank models are
// never auto-retired and simply stay enabled until an operator disables one.
// This is a deliberately conservative choice and the cheap one to reverse:
// adding the family here plus a body and a content check is all it takes.
//
// So where a family cannot be probed cheaply, the correct outcome is to not
// auto-retire that family at all. The alternative — falling back to trusting
// the prose classifier — would leave the retirement running unsupervised
// precisely in the corner where it is least observed and least often right.
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
// The Closer half is the point. Wrapping in an io.NopCloser would leave nothing
// holding the real body, and every reader downstream — including the two in
// other packages — would then be reading through a cap while the connection it
// belongs to leaked.
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
// The state is synthetic but the path is not. Everything the builder derives
// from it — provider-type detection, the target-URL rules, the OpenAI-Responses
// and vertex-express Gemini dialect re-routes, the param rewrite — has to run,
// because a bespoke request builder would be able to succeed where real traffic
// fails and a probe that is easier to satisfy than the requests it adjudicates
// is worse than no probe at all.
//
// One divergence from that, named rather than left to be discovered. anthropicIn
// and anthropicRawBody are NOT set, so an endpointTypeMessages candidate on an
// anthropic-type provider is probed with an OpenAI chat body at
// {base}/v1/chat/completions, whereas the traffic that nominated it went out
// natively at /v1/messages (see anthropic_native.go). Setting them is not
// available here: anthropicRawBody is the CLIENT's /v1/messages body, and a
// probe has no client, so the native path would have to be handed a body this
// function invented — which is the bespoke request the paragraph above rules
// out, on the one dialect where it is easiest to get subtly wrong.
//
// It is acceptable because of which way the difference can fail. Anthropic's
// compat layer serves the same model catalog, so a live model answers on both
// surfaces and a retired one is refused by name on both. If the compat layer
// itself is the problem — not enabled, differently gated, differently shaped —
// the probe draws an error that is not a retirement, classifies as such, and
// returns probeInconclusive. The retirement is postponed, never manufactured,
// which is the direction every unproven case in this file already takes.
//
// endpointPath is deliberately left empty for the chat families even though
// probeEndpointForFamily names "/chat/completions". Empty IS chat to the
// builder (it defaults to exactly that path), while a non-empty endpointPath is
// how the pipeline says "this is one of the multimodal endpoints" — and both
// dialect re-routes refuse to fire when it is set. Spelling the default out
// would therefore switch off the re-routes this function exists to go through,
// and a vertex-express candidate would be probed with an OpenAI body against a
// path Google does not serve.
//
// reqModel is deliberately left EMPTY, and that is load-bearing for the same
// reason. It is the model the CLIENT asked for, and a probe has no client, so
// there is no alias to record — but the builder's rewrite gate is
// `st.reqModel != candidate.model.ModelID || ...` (proxy_failover.go), so
// naming the upstream model here closes every disjunct and
// paramrewrite.BuildUpstreamBody never runs. The probe would then skip the
// learned renames and strips that real traffic to the same candidate gets:
// an OpenAI reasoning model that has learned max_tokens -> max_completion_tokens
// would be probed with the very parameter it rejects, answer 400, classify as
// not-a-retirement and return inconclusive forever. That is the exact inversion
// this function exists to prevent — the probe failing where real traffic
// succeeds — and it would land on the reasoning family the 64-token budget was
// chosen for. Empty opens the gate honestly, and BuildUpstreamBody then writes
// the resolved id into the body itself.
//
// The marshal errors are discarded rather than propagated, and that is a
// statement about the inputs rather than a shortcut: both payloads are fixed
// structs of strings and ints, so encoding/json has no failure to report here.
// Threading an error nothing can produce would add a branch no test could ever
// reach.
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
		// id in the body is the one the builder resolved rather than the one
		// this function happened to be handed.
		st.makeUpstreamBody = func(resolvedModelID string) ([]byte, string, error) {
			resolved, _ := json.Marshal(probeEmbeddingsRequest{Model: resolvedModelID, Input: "hi"})
			return resolved, "application/json", nil
		}
		// bodyBytes is set to the same payload even though makeUpstreamBody
		// supersedes it: several things downstream of the builder read the
		// request body, and none of them should be looking at an empty one.
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
// It exists because the retirement verdict was being drawn from provider prose,
// and prose drifts. classifyUpstreamError reads the words a provider chose on
// the day it wrote them; this reads what the provider actually does when asked
// for the model. The probe is the adjudicator, the classifier is only what
// nominates a model for adjudication.
//
// It is an upstream call the operator did not make, so it is deliberately NOT
// run through doUpstream: no circuit-breaker outcome, no request log, no
// metering, no rate-limit accounting. Charging a provider's breaker for a
// verification request would let the verification itself take a healthy
// provider out of routing, and metering it would bill an operator for traffic
// they never sent.
//
// The caller owns the deadline (see goneProbeTimeout) and the decision; this
// returns evidence only.
func (h *Handler) probeModel(ctx context.Context, candidate modelCandidate, endpointType string) probeVerdict {
	endpoint, ok := probeEndpointForFamily(endpointType)
	if !ok {
		// Defensive: the caller is expected to gate on the family before
		// spending a request. Reaching here means it did not, and the honest
		// answer is still that nothing was established.
		return probeInconclusive
	}
	// Kept although probeForRetirement now checks first, because this function
	// takes a candidate and dereferences it and must be able to say so on its
	// own terms rather than by the grace of its only production caller. It is
	// reachable and exercised: the tests drive probeModel directly, which is
	// also how any future caller would.
	if candidate.model == nil || candidate.provider == nil {
		return probeInconclusive
	}

	// Skipping the breaker's ACCOUNTING is argued at length above. Skipping its
	// CHECK would be a separate decision and is not one this makes: if the
	// gateway has already sidelined this provider, a probe to it is a
	// guaranteed-wasted call to a host nothing else is being sent to, and its
	// answer would be inconclusive anyway. So the breaker is consulted, and
	// consulted through the one entry point that records nothing.
	//
	// GetState rather than IsOpen, and the difference is the whole reason this
	// check is safe to make. IsOpen is the routing gate: it takes a write lock
	// and performs the Open→HalfOpen transition, which spends the provider's
	// one half-open trial slot on a request the operator did not make and would
	// let a verification decide a provider's fate. GetState takes a read lock
	// and derives the same logical state without touching the circuit, so an
	// open circuit past its cooldown already reads as half-open here — which is
	// exactly the semantics wanted: postpone only while the breaker is actually
	// holding traffic back, proceed the moment it is ready to be probed again.
	//
	// A nil breaker means nobody has an opinion, which is not a reason to
	// postpone.
	//
	// This gate is on the breaker alone, unlike every other breaker consultation
	// in the proxy (resolve.go's cbEnabled, the record sites' st.circuitBreakerEnabled),
	// which also checks the circuit_breaker_enabled setting before consulting the
	// breaker at all. That is a known asymmetry, not an oversight: reading the
	// setting here means calling h.settingsRepo.GetBool, and h.settingsRepo is nil
	// on the path every probe unit test in this file exercises (newProbeHandler
	// builds a bare *Handler with no settings repository, deliberately, so these
	// tests do not need a database). Wiring a live settings repository through
	// that fixture to read one flag is a cost this check does not justify, because
	// the failure mode of skipping it is bounded and always in the safe direction:
	// an operator who disables the breaker after a provider's circuit has already
	// opened sees that provider's probes deferred until the breaker's own cooldown
	// clears rather than immediately, and a deferred probe can only leave a model
	// enabled longer, never retire it early or wrongly. If a later change threads a
	// testable settings source through this path for some other reason, honoring
	// the setting here becomes free and should be done then.
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
	// No transport, no probe. The nil check has to exist — upstreamTransport is a
	// concrete *http.Transport, so assigning a nil one into the RoundTripper
	// field produces a non-nil interface holding a nil pointer, and net/http does
	// not fall back to DefaultTransport, it panics inside RoundTrip, on the
	// detached disable goroutine — but leaving the field unset instead is not the
	// harmless half of that choice. An unset Transport IS http.DefaultTransport,
	// which carries no DialContext, so the SafeDialer's guard against a provider
	// URL resolving into the gateway's own network would be silently absent for
	// exactly the request nobody is watching. Every other outbound site in this
	// package assigns the shared transport unconditionally; a probe that cannot
	// have it postpones, which costs nothing in a process that always sets it.
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
		// says nothing about the model. This is the branch that keeps a network
		// problem on the gateway's side from retiring a provider's whole
		// catalog.
		debuglog.Debug("proxy: retirement probe did not reach the provider", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "verdict", probeInconclusive.String(), "error", err)
		return probeInconclusive
	}
	// The cap goes on the body here, once, and everything past this line reads
	// through it. Applying it at the read sites instead would have missed three
	// of them: remapMiniMaxBusinessError and both dialect translators call
	// io.ReadAll(resp.Body) inside packages that know nothing about this budget,
	// so a MiniMax, vertex-express or Responses candidate answering a 200 with a
	// multi-gigabyte body would have been read into memory in full.
	//
	// rawBody is held separately because resp.Body is replaced here and replaced
	// again by remapMiniMaxBusinessError, and the drain below has to read the
	// transport's own body rather than whichever wrapper is in the field by then.
	// The close can stay on the field precisely because cappedBody passes Close
	// through — see cappedBody for why the obvious io.NopCloser cannot.
	//
	// One byte past the cap, so a truncated body can be recognised as truncated:
	// io.ReadAll reports no error when a LimitReader runs out, so length is the
	// only signal there is (see judgeProbeFailure).
	rawBody := resp.Body
	resp.Body = cappedBody{Reader: io.LimitReader(rawBody, goneProbeMaxBody+1), Closer: rawBody}
	defer func() {
		// Drain before closing so the transport can reuse the connection, and
		// drain no more than the judgement was willing to read. A body past
		// goneProbeMaxBody costs the connection its reuse and nothing else,
		// which is the cheaper of the two ways to be wrong here.
		//
		// "No more than the judgement read" bounds this copy, not the total: the
		// judgement has already taken its own goneProbeMaxBody out of rawBody, so
		// a large answer is pulled off the socket twice over. What the constant
		// promises is memory, and that still holds — what is retained is the
		// judgement's read, while this streams to io.Discard through one 32 KiB
		// buffer. Bytes off a socket on a detached goroutine, four at a time per
		// provider, are not the resource being bounded.
		//
		// It is a plain-path drain and knowingly a no-op elsewhere: MiniMax and
		// both dialect translators read the body to EOF and CLOSE it themselves,
		// so on those candidates this copy reads an already-closed body and
		// discards the error. Nothing is lost by that. A body those paths could
		// finish was already drained by them, and one they could not finish is
		// past the cap, where the connection is forfeit either way. The branch it
		// would take to skip the copy would cost more to read than the copy costs
		// to run.
		_, _ = io.Copy(io.Discard, io.LimitReader(rawBody, goneProbeMaxBody))
		// Whatever is in the field: the cap above on every path, or the
		// NopCloser remapMiniMaxBusinessError leaves behind — and that path has
		// already closed the cap, and through it the transport's own body. The
		// real body is closed exactly once either way.
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
// no status-code shortcut around the classifier here — a bare "404 means gone"
// would retire every model behind a misconfigured base URL.
func judgeProbeFailure(resp *http.Response, candidate modelCandidate, endpointType string) probeVerdict {
	// One byte past the cap on purpose. This is the one place in this file where
	// a body we did not receive in full could RETIRE a model: a surviving prefix
	// that happens to name the model beside a gone-phrase classifies exactly like
	// a real refusal, and the classifier only ever sees the first 10 000
	// characters, so it cannot tell that the rest went missing.
	//
	// Reading exactly goneProbeMaxBody could not tell either. io.ReadAll returns
	// a nil error when a LimitReader is exhausted — that is not a read failure,
	// it is the reader ending — so the err branch below catches genuine transport
	// failures and never catches truncation. The extra byte is what turns "we hit
	// the cap" into something observable, and probeModel sizes its own wrapper to
	// let it through.
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
// A 200 that carries nothing is NOT a success. A stream can open, emit nothing
// and end cleanly, and crediting that is the same bug an earlier review round
// caught on the streaming path (see streamProducedOutput in model_gone.go).
// It is not a refusal either — the provider did not say the model is gone — so
// an empty answer postpones like every other unproven case.
func judgeProbeSuccess(resp *http.Response, st *requestState, candidate modelCandidate, endpointType string) probeVerdict {
	// The dialect re-routes answer in their own wire format. Translate first,
	// for the same reason attemptCandidate does: everything downstream judges
	// the chat-completions shape. An answer that cannot be translated is not a
	// refusal, so it postpones like every other unreadable result.
	if err := translateProbeDialect(resp, st, candidate.model.ModelID); err != nil {
		debuglog.Debug("proxy: retirement probe could not read the provider's dialect", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "error", err)
		return probeInconclusive
	}

	// Both the read error and the cap are ignored here, which is the opposite of
	// the care judgeProbeFailure takes over the same call. The asymmetry is in
	// what an incomplete body can produce, not in how much attention the two
	// paths deserve. There, a surviving prefix naming the model beside a
	// gone-phrase would classify as a refusal and RETIRE, so it has to be
	// detected; here, the worst a partial body can do is fail to parse
	// (probeDeliveredContent returns false, and the probe postpones) or carry
	// content, which can only ever PREVENT a disable. Neither outcome can retire
	// a model, so the branches would postpone exactly where the code already
	// postpones.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, goneProbeMaxBody))
	verdict := probeInconclusive
	if probeDeliveredContent(endpointType, body) {
		verdict = probeServed
	}
	debuglog.Debug("proxy: retirement probe answered", "endpoint", endpointType, "provider", candidate.provider.Name, "model", candidate.model.ModelID, "status", resp.StatusCode, "verdict", verdict.String())
	return verdict
}

// translateProbeDialect converts a dialect answer back to the
// chat-completions shape the judgement below understands, mirroring what
// attemptCandidate does on the served path. Which flag is set was decided by
// buildCandidateRequest, so this stays correct as the re-route rules move.
//
// Only the non-streaming translators are reachable: the probe never asks for a
// stream, so there is no adapter to install.
//
// The Responses case cannot fire as the probe body stands — that re-route also
// requires tools in the request, and the probe sends none — but it is kept
// rather than dropped, because the flag is set by buildCandidateRequest and not
// by anything here. If the re-route rules widen, a probe that skipped the
// translation would read a Responses object as an empty chat completion and
// postpone every retirement for those models forever, silently.
func translateProbeDialect(resp *http.Response, st *requestState, modelID string) error {
	// The upstream model id, NOT st.reqModel: the served path passes the name
	// the client asked for, and a probe has no client (st.reqModel is empty by
	// design, see newProbeState). The synthesized response is named after the
	// model actually asked for, which is the only name this request ever had.
	switch {
	case st.responsesAttempt:
		return translateResponsesResponseBody(resp, modelID)
	case st.geminiAttempt:
		return translateGeminiResponseBody(resp, modelID)
	default:
		return nil
	}
}

// probeDeliveredContent reports whether a 200 probe response actually carried
// something the model produced.
//
// The chat side accepts visible content, reasoning content and tool calls
// because all three are the model working, and falls back to the reported
// completion tokens because a reasoning model can legitimately spend the whole
// budget thinking and return an empty visible answer. Without that fallback the
// probe would call a working reasoning model empty and postpone forever.
//
// A body that will not parse returns false, which postpones rather than
// retires: an unreadable answer is not the provider saying the model is gone.
func probeDeliveredContent(endpointType string, body []byte) bool {
	if endpointType == endpointTypeEmbeddings {
		// The vector is left undecoded on purpose. Typing it as []float64 made
		// the whole document fail to parse when a provider answered with
		// base64-encoded embeddings — a JSON string where the struct wanted an
		// array — so a live embeddings model came back inconclusive, its streak
		// was never parked, and every refusal past the cooldown bought another
		// upstream request indefinitely. It is the same shape of mistake the
		// chat side made by accepting only a string content.
		//
		// Raw and non-empty is still a real bar: an absent vector, a null, an
		// empty array and an empty string are all "the provider answered with
		// nothing", which is not evidence the model is served. What it stops
		// doing is judging the ENCODING, which is the provider's choice and says
		// nothing about whether the model exists.
		var out struct {
			Data []struct {
				Embedding json.RawMessage `json:"embedding"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &out) != nil || len(out.Data) == 0 {
			return false
		}
		switch string(bytes.TrimSpace(out.Data[0].Embedding)) {
		case "", "null", "[]", `""`:
			return false
		}
		return true
	}

	var out ChatCompletionResponse
	if json.Unmarshal(body, &out) != nil {
		return false
	}
	for _, choice := range out.Choices {
		if s, ok := choice.Message.Content.(string); ok && s != "" {
			return true
		}
		// Content is `any` because a provider may answer with content PARTS
		// rather than a string. Judging only the string shape would call such a
		// model empty: the probe would return inconclusive on a model that is
		// demonstrably alive, the streak would never be parked, and every fresh
		// refusal past the cooldown would buy another upstream request for as
		// long as the disagreement lasted. Non-empty is the whole test — the
		// parts are the model's output whatever their shape, and reaching into
		// them to find a non-empty text field would be judging providers' part
		// vocabularies from here.
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
