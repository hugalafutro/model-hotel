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

// goneWriteTimeout bounds each out-of-band write the auto-disable makes.
//
// Each write gets its OWN deadline rather than sharing one across the sequence.
// They run one after another, so a shared budget lets a slow first write starve
// everything after it: a disable that took the full ten seconds but succeeded
// would leave group revalidation to fail instantly with a deadline that was
// already spent — exactly when the database is under the load that made the
// disable slow, and exactly when an undersized group most needs resizing.
//
// The work is already detached from the request path, so nothing is waiting on
// the total. Bounding each step is what keeps every step able to do its job.
const goneWriteTimeout = 10 * time.Second

// goneStreak is one model's consecutive-refusal count plus a tombstone for the
// window between deciding to disable and the write landing.
//
// n must be atomic because a retired model is precisely the one taking
// concurrent refusals; a read-modify-write would lose increments and the streak
// would never reach the threshold.
//
// cancelled exists because the disable is detached. Between the threshold being
// reached and the database write completing — as long as the database takes —
// the model can answer a request and prove it is alive. noteModelServed sets
// this so the queued write can stand down instead of retiring a model whose
// evidence has already been superseded.
//
// It is read twice, and both reads are needed. Before the write it prevents a
// disable that is now known to be wrong; after the write it catches a success
// that landed while the write was in flight, which the first read is by
// definition too early to see. The second case cannot be prevented, only
// undone, so noteModelGone reverts rather than skips there.
type goneStreak struct {
	n         atomic.Int64
	cancelled atomic.Bool
}

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
	// sync.Map holds a per-model *goneStreak rather than a plain int, so
	// LoadOrStore only races on creating it (harmless, the winner's is used by
	// everyone) and the increment itself is atomic.
	raw, _ := h.goneStrikes.LoadOrStore(m.ID, &goneStreak{})
	streak, ok := raw.(*goneStreak)
	if !ok {
		return
	}
	strikes := streak.n.Add(1)

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
		// The decision was made before this goroutine was scheduled, and the
		// write below can take as long as the database does. If the model
		// answered a request in the meantime it has proved it is alive, and
		// disabling it now would take a working model out of routing on the
		// strength of evidence that has already been superseded.
		//
		// noteModelServed marks the streak cancelled before dropping it, so this
		// check sees any success that landed while the disable was queued.
		if streak.cancelled.Load() {
			debuglog.Info("proxy: skipping auto-disable, model answered while the disable was queued", "model", modelName, "provider", provider)
			return
		}

		dctx, cancel := context.WithTimeout(context.Background(), goneWriteTimeout)
		_, err := h.modelRepo.SetEnabled(dctx, modelID, false)
		cancel()
		if err != nil {
			debuglog.Error("proxy: failed to auto-disable retired model", "model", modelName, "provider", provider, "error", err)
			// Clear the streak so the next refusals can rebuild it and try
			// again. Without this a transient database error would leave the
			// count parked above the threshold and the model enabled forever.
			h.goneStrikes.Delete(modelID)
			return
		}

		// Re-check AFTER the write, not only before it. The check above cannot
		// cover the write itself: a success landing while the UPDATE was in
		// flight sets the flag too late to stop it, and the model is then
		// retired in the moment it proved it still serves traffic.
		//
		// Serialising instead — holding a lock across the write so a success
		// has to wait for it — is not available here. noteModelServed runs on
		// the request path BEFORE a non-streaming response is written to the
		// client (see proxy_failover.go), so blocking it would put client
		// latency behind a database write on an unrelated request's error path.
		// Undoing a disable that has been superseded is the cheaper trade: it
		// costs one extra write in a window that is rarely hit, and only ever on
		// a model that has just been shown to work.
		if streak.cancelled.Load() {
			rctx, rcancel := context.WithTimeout(context.Background(), goneWriteTimeout)
			_, rerr := h.modelRepo.SetEnabled(rctx, modelID, true)
			rcancel()
			if rerr != nil {
				// Nothing safe is left to try. Log loudly: the model is
				// disabled and the gateway believes it should not be.
				debuglog.Error("proxy: model answered while its auto-disable was in flight, and re-enabling it failed", "model", modelName, "provider", provider, "error", rerr)
				return
			}
			debuglog.Info("proxy: reverted auto-disable, model answered while the write was in flight", "model", modelName, "provider", provider)
			// Deliberately no alert and no group revalidation: as far as
			// operators are concerned nothing happened, and publishing a
			// retirement for a model that is still enabled would be a lie.
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
			vctx, vcancel := context.WithTimeout(context.Background(), goneWriteTimeout)
			_, verr := h.failoverRepo.RevalidateCustomGroups(vctx)
			vcancel()
			if verr != nil {
				debuglog.Error("proxy: custom-group revalidation after auto-disable failed", "model", modelName, "error", verr)
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
//
// It also cancels a disable that has been decided but not yet written. The
// ordering matters: mark the streak cancelled FIRST, then drop it from the map.
// A queued disable holds a pointer to the streak, not the map entry, so marking
// before deleting is what makes the flag visible to it — deleting first would
// leave the goroutine holding an orphan it can never be told about.
func (h *Handler) noteModelServed(m *model.Model) {
	if m == nil || m.ID == uuid.Nil {
		return
	}
	raw, ok := h.goneStrikes.Load(m.ID)
	if !ok {
		return
	}
	if streak, ok := raw.(*goneStreak); ok {
		streak.cancelled.Store(true)
	}
	h.goneStrikes.Delete(m.ID)
	debuglog.Debug("proxy: model answered again, cleared gone-strikes", "model", m.ModelID)
}
