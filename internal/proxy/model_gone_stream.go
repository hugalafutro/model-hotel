package proxy

import (
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// streamVerdict is what a finished stream says about whether the model still
// exists. Three-way on purpose, and the middle case is the one that matters.
type streamVerdict int

const (
	// verdictInconclusive: the stream failed for a reason unrelated to the
	// model's existence. Leave the strike streak untouched.
	verdictInconclusive streamVerdict = iota
	// verdictGone: the provider reported the model retired mid-stream.
	verdictGone
	// verdictServed: the stream completed cleanly, so the model answered.
	verdictServed
)

// verdictForStream maps a finished stream to what it proves about the model.
//
// The trap this encodes: treating "not gone" as "served" lets a retired model
// stay routable forever, because its own unrelated failures (transient provider
// errors, client disconnects, stalls) keep resetting the count that would have
// retired it. Equally, treating any failure as evidence of death would disable
// a healthy model during an outage.
//
// producedOutput is the second half of that care. An absent error kind is not by
// itself proof the model answered: a stream can open, emit nothing and end
// without ever recording an error, and crediting that as a success would clear a
// retirement streak on the strength of nothing at all. Clearing therefore
// requires positive evidence that content actually flowed.
// upstreamKind is what the provider said, which is the only thing that can
// establish a retirement. kind is how the request ended, and it is what rules a
// success out.
//
// The two are separate because they answer different questions and the second
// overwrites the first. A provider can report the model retired mid-stream and
// the client can then hang up — the likeliest thing for a client to do on
// receiving an error — at which point the recorded kind becomes
// client_disconnect. Judging the model by that would let a client suppress the
// evidence by reacting to it, and a retired model would stay routable for as
// long as clients kept disconnecting on its errors.
func verdictForStream(kind, upstreamKind ErrorKind, producedOutput bool) streamVerdict {
	switch {
	case upstreamKind == KindProviderModelGone || kind == KindProviderModelGone:
		return verdictGone
	case kind == "" && producedOutput:
		return verdictServed
	default:
		return verdictInconclusive
	}
}

// producedOutput reports whether a response actually delivered content.
//
// Used by the stream verdict and by the non-streaming chat path, which ask the
// same question of the same fields: a 200 is a status, and what clears a
// retirement streak has to be an answer.
//
// deliveredContent is the authoritative signal, set where the content itself is
// observed. The other two are corroboration and neither can be relied on alone:
// completion tokens are absent when a provider omits the usage chunk, and TTFT
// is zero when the probe is switched off. With only those two, a provider that
// streams a perfectly good answer and reports no usage, on a gateway with the
// probe off, reads as having produced nothing — the success then fails to clear
// the streak, and later refusals retire a model whose failures were never
// consecutive.
//
// The direction of the error matters here, which is why the bar is positive
// evidence rather than absence of an error: a stream can open, emit nothing and
// end without recording anything, and crediting that would clear a retirement
// streak on the strength of an empty response.
func producedOutput(logData *requestLogData) bool {
	if logData == nil {
		return false
	}
	return logData.deliveredContent || logData.tokensCompletion > 0 || logData.ttftMs > 0
}

// noteStreamOutcome applies the model verdict once a stream has finished. Shared
// by the sequential dispatch and the hedged winner so the two cannot drift —
// the hedged path previously returned without recording any verdict at all, so
// a model retired mid-stream stayed routable whenever hedging was enabled.
func (h *Handler) noteStreamOutcome(logData *requestLogData, candidate modelCandidate) {
	// The guard belongs here, not in producedOutput. That function checks for nil
	// too, but this expression dereferences logData to build its arguments and Go
	// evaluates all of them before the call, so the helper's guard could never
	// run. Same shape as probeForRetirement's.
	if logData == nil {
		return
	}
	switch verdictForStream(logData.errorKind, logData.upstreamKind, producedOutput(logData)) {
	case verdictGone:
		// The endpoint family comes off the log entry rather than being assumed:
		// it is what decides whether this refusal can be adjudicated at all, and
		// a stream reaching here can have started at /v1/chat/completions or at
		// /v1/messages. newPendingRequestLog stamps it at ingest, so it is set on
		// every path that can reach this.
		h.noteModelGone(candidate, logData.endpointType)
	case verdictServed:
		h.noteModelServed(candidate.model, logData.endpointType)
	case verdictInconclusive:
		// Deliberately nothing: see verdictForStream.
	}
}

// noteModelServed clears any accumulated gone-strikes after the model answers.
// The strike streak must be consecutive, so one success is enough to reset it.
//
// It runs on the request path, so what it costs there is part of the design: a
// model that has never been refused misses the map entirely, and one that has
// pays a miss plus an uncontended mutex on a request that is already waiting on
// an upstream call. Nothing is written and nothing touches the database.
//
// It also cancels a disable that has been decided but not yet written, which is
// why it goes through supersede rather than setting the flag and parking
// separately: a queued disable reads both and has to see them change together.
//
// The entry is parked, not dropped, and the difference is the probe cooldown.
// Deleting it took nextProbeAt with it, so a success between refusals reset the
// rate bound — and a model that refuses some request shapes while serving others
// produces exactly that interleaving. A success clears what the model is accused
// of; it does not buy the gateway another free upstream call. See park.
//
// The success clears ONE surface: the one it arrived on. Clearing globally
// answered two separate questions as one — a provider that had retired a model's
// chat surface while still serving its embeddings would have every embeddings
// success wipe the chat streak, so the dead surface could never accumulate three
// consecutive strikes and would never be adjudicated at all. Tightening it is
// safe because what retires a model is the PROBE, and the probe asks on the same
// surface.
//
// A family that cannot be probed clears nothing, following the strike side: an
// image or TTS response is not evidence about the chat surface any more than an
// image refusal is. It is also why this takes an endpointType rather than
// deriving one — every caller has the family that ingest stamped.
//
// The log line is conditional on supersede having changed something. Nothing
// removes entries, so a model that drew one refusal at 09:00 keeps its parked
// streak for the life of the process, and an unconditional line would claim to
// have "cleared gone-strikes" on every successful request from then on.
func (h *Handler) noteModelServed(m *model.Model, endpointType string) {
	if m == nil || m.ID == uuid.Nil {
		return
	}
	endpoint, ok := probeEndpointForFamily(endpointType)
	if !ok {
		return
	}
	raw, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint})
	if !ok {
		return
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		return
	}
	if streak.supersede() {
		debuglog.Debug("proxy: model answered again, cleared gone-strikes", "model", m.ModelID, "endpoint", endpoint)
	}
}
