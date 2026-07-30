package proxy

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// goneStrikeThreshold is how many consecutive KindProviderModelGone responses a
// model must draw, with no successful request in between, before the gateway
// disables it.
//
// Why traffic and not discovery: a provider listing is not a promise. Google
// kept gemini-2.0-flash in /models for two months after shutting it down,
// OpenCode Zen lists claude-sonnet-4 and refuses it, OpenCode Go lists
// hy3-preview and refuses it. RecordMissingModels can only act when a model
// leaves the listing, so none of those were ever going to be caught by
// discovery. The only source that knows a model is dead is a real request to
// it, which is exactly what classifyUpstreamError now labels.
//
// Three rather than the discovery sweep's two: a scan is a deliberate, spaced
// observation, whereas requests can arrive in a burst during a provider
// incident. Requiring three consecutive refusals with no success in between
// keeps a brief upstream wobble that happens to match a gone-pattern from
// retiring a live model.
const goneStrikeThreshold = 3

// noteModelGone records one strike against a model the provider refused as
// retired, and disables it once the streak reaches goneStrikeThreshold.
//
// Strikes are in-memory and deliberately not persisted. They are a heuristic
// over recent traffic, not an audit trail: losing them on restart just means a
// genuinely dead model re-earns them on the next few requests, while keeping
// the hot path free of a database write per failed request. Each HA member
// therefore reaches its own conclusion from its own traffic, which is the safer
// direction — nothing fans a disable out across the fleet on one member's
// evidence.
func (h *Handler) noteModelGone(m *model.Model, providerName string) {
	if m == nil || m.ID == uuid.Nil {
		return
	}

	// The counter must be incremented atomically, not read-modify-written. A
	// dead model is exactly the one that gets hammered concurrently — clients
	// retry it, and a failover group can try it on several requests at once — so
	// a lost update here is not theoretical: two refusals racing would both read
	// the same value and store the same increment, the streak would stall below
	// the threshold, and the model would stay routable indefinitely.
	//
	// sync.Map holds a per-model *atomic.Int64 rather than a plain int, so
	// LoadOrStore only races on creating the counter (harmless, the winner's is
	// used by everyone) and the increment itself is atomic.
	raw, _ := h.goneStrikes.LoadOrStore(m.ID, &atomic.Int64{})
	counter, ok := raw.(*atomic.Int64)
	if !ok {
		return
	}
	strikes := counter.Add(1)

	if strikes < goneStrikeThreshold {
		debuglog.Info("proxy: provider reports model gone", "model", m.ModelID, "provider", providerName, "strikes", strikes, "threshold", goneStrikeThreshold)
		return
	}
	// Exactly one caller sees the threshold value, so a burst of concurrent
	// refusals still issues a single disable; everyone past it drops out here.
	//
	// The counter is deliberately NOT cleared here. Clearing it would let the
	// next refusal start a fresh count from zero, so a burst against a dead
	// model — 50 concurrent retries, say — would disable it once every three
	// strikes and publish an alert each time. Leaving the count above the
	// threshold makes every later refusal a no-op. noteModelServed clears it if
	// the model ever answers again, and the failure path below clears it so a
	// disable that could not be written is retried rather than lost.
	if strikes > goneStrikeThreshold {
		return
	}

	// Threshold reached. Disable out of band: this runs on the request path and
	// must not add latency to the error response the caller is already getting.
	modelID, modelName, provider := m.ID, m.ModelID, providerName

	go func() {
		// Detached on purpose: the request context is already being torn down,
		// and the caller's error response must not wait on this write.
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := h.modelRepo.SetEnabled(dctx, modelID, false); err != nil {
			debuglog.Error("proxy: failed to auto-disable retired model", "model", modelName, "provider", provider, "error", err)
			// Clear the streak so the next refusals can rebuild it and try
			// again. Without this a transient database error would leave the
			// count parked above the threshold and the model enabled forever.
			h.goneStrikes.Delete(modelID)
			return
		}

		debuglog.Warn("proxy: auto-disabled retired model", "model", modelName, "provider", provider, "strikes", goneStrikeThreshold)

		// Disabling a member does not resize the custom failover groups it
		// belongs to, so a group can be left enabled while it no longer has two
		// routable members. Discovery already revalidates after it disables a
		// model (see discovery_diff.go); this path has to do the same or an
		// undersized group stays enabled until some unrelated scan happens to
		// fix it. Best-effort: a failure here must not undo the disable.
		if h.failoverRepo != nil {
			if _, err := h.failoverRepo.RevalidateCustomGroups(dctx); err != nil {
				debuglog.Error("proxy: custom-group revalidation after auto-disable failed", "model", modelName, "error", err)
			}
		}
		events.Publish(events.Event{
			Type:     "model.auto_disabled_gone",
			Severity: "warning",
			Source:   "proxy",
			Message:  fmt.Sprintf("Disabled %s: %s reports it is no longer served", modelName, provider),
			Metadata: map[string]any{
				"model_id":      modelName,
				"model_uuid":    modelID.String(),
				"provider_name": provider,
				"strikes":       goneStrikeThreshold,
				"reason":        string(KindProviderModelGone),
			},
		})
	}()
}

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
func verdictForStream(kind ErrorKind, producedOutput bool) streamVerdict {
	switch {
	case kind == KindProviderModelGone:
		return verdictGone
	case kind == "" && producedOutput:
		return verdictServed
	default:
		return verdictInconclusive
	}
}

// streamProducedOutput reports whether a finished stream actually delivered
// content. Either signal alone is enough and neither is reliable on its own:
// completion tokens are absent when a provider omits the usage chunk, and TTFT
// is zero when the probe is disabled, so a stream that emitted content will
// normally set at least one.
func streamProducedOutput(logData *requestLogData) bool {
	return logData != nil && (logData.tokensCompletion > 0 || logData.ttftMs > 0)
}

// noteStreamOutcome applies the model verdict once a stream has finished. Shared
// by the sequential dispatch and the hedged winner so the two cannot drift —
// the hedged path previously returned without recording any verdict at all, so
// a model retired mid-stream stayed routable whenever hedging was enabled.
func (h *Handler) noteStreamOutcome(logData *requestLogData, candidate modelCandidate) {
	switch verdictForStream(logData.errorKind, streamProducedOutput(logData)) {
	case verdictGone:
		h.noteModelGone(candidate.model, candidate.provider.Name)
	case verdictServed:
		h.noteModelServed(candidate.model)
	case verdictInconclusive:
		// Deliberately nothing: see verdictForStream.
	}
}

// noteModelServed clears any accumulated gone-strikes after the model answers.
// The strike streak must be consecutive, so one success is enough to reset it.
// The map lookup is deliberately the only work done for a healthy model: nothing
// is written, and nothing touches the database.
func (h *Handler) noteModelServed(m *model.Model) {
	if m == nil || m.ID == uuid.Nil {
		return
	}
	if _, ok := h.goneStrikes.Load(m.ID); ok {
		h.goneStrikes.Delete(m.ID)
		debuglog.Debug("proxy: model answered again, cleared gone-strikes", "model", m.ModelID)
	}
}
