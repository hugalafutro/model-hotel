package proxy

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// goneStrikeThreshold is how many consecutive KindProviderModelGone responses a
// model must draw, with no successful request in between, before the gateway
// PROBES it.
//
// It is no longer the retirement bar. What disables a model is probeModel
// refusing it on a request the gateway makes itself (see noteModelGone); the
// strikes decide only that the question is worth one upstream call. Reading
// this as the retirement bar is the mistake to avoid: three strikes on their
// own now disable nothing.
//
// Why traffic and not discovery: a provider listing is not a promise. Google
// kept gemini-2.0-flash in /models for two months after shutting it down,
// OpenCode Zen lists claude-sonnet-4 and refuses it, OpenCode Go lists
// hy3-preview and refuses it. RecordMissingModels can only act when a model
// leaves the listing, so none of those were ever going to be caught by
// discovery. The only source that knows a model is dead is a real request to
// it, which is exactly what classifyUpstreamError labels.
//
// Three rather than the discovery sweep's two: a scan is a deliberate, spaced
// observation, whereas requests can arrive in a burst during a provider
// incident. Requiring three consecutive refusals with no success in between
// keeps a brief upstream wobble that happens to match a gone-pattern from
// spending a probe on a live model — and, before the probe existed, from
// retiring one outright. The probe has since taken over the second job, which
// is why three is now a cost control rather than the last line of defence, and
// why it has not been raised: a wrong nomination costs one cheap request, and
// the probe throws it out.
const goneStrikeThreshold = 3

// goneStrikeWindow is how long a strike stays part of the streak it belongs to.
//
// "Three consecutive refusals" was counting refusals with no bound on how far
// apart they were, so two strikes from a provider incident this morning and one
// this afternoon retired a model on evidence that had nothing to do with each
// other. Worse, the older strikes are exactly the ones an operator may already
// have acted on — they enable the model, and a count they cannot see finishes
// and turns it off again.
//
// A strike arriving after this long starts the streak over rather than adding to
// it, so a retirement is always drawn from one run of recent traffic.
//
// Thirty minutes is chosen against real traffic rather than as a round number:
// three requests to the same model have to fall inside it, which a gateway with
// any load does easily, while a model receiving one request an hour will never
// accumulate a streak. That second case is deliberate — a model too idle to fail
// three times in half an hour is also too idle for its staying enabled to cost
// anything, and traffic-driven retirement should not be guessing from sparse
// evidence.
const goneStrikeWindow = 30 * time.Minute

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
//
// The count and the time of the last strike are guarded by mu rather than being
// separate atomics. Two atomics cannot express this: deciding whether the window
// has lapsed and then applying the decision is one operation, and splitting it
// loses strikes. A reset racing two increments stores 1 after both have added,
// erasing them, so a model refused three times ends the burst on a count of one
// and never reaches the threshold.
//
// The lock is held for a comparison and an addition, on a per-model struct that
// is only contended while that one model is failing, so it costs nothing worth
// measuring.
//
// cancelled stays atomic because it is read by the detached disable goroutine
// while the request path may be writing it, and it is independent of the pair
// above.
type goneStreak struct {
	mu         sync.Mutex
	n          int64
	lastStrike time.Time

	cancelled atomic.Bool
}

// strike records a refusal and returns the length of the streak it belongs to.
//
// A strike arriving more than goneStrikeWindow after the previous one starts the
// count over instead of extending it, so a retirement is always drawn from one
// run of recent traffic rather than from unrelated failures that happen to share
// a model.
func (s *goneStreak) strike(now time.Time) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastStrike.IsZero() && now.Sub(s.lastStrike) > goneStrikeWindow {
		s.n = 1
	} else {
		s.n++
	}
	s.lastStrike = now
	return s.n
}

// count reports the current streak length.
func (s *goneStreak) count() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// noteModelGone records one strike against a model the provider refused as
// retired, and once the streak reaches goneStrikeThreshold asks the provider
// directly before disabling anything.
//
// The classifier nominates, the probe adjudicates. Every strike here was read
// out of provider prose by classifyUpstreamError, and prose drifts: a phrasing
// that means "retired" today is a phrasing some provider will use for something
// else tomorrow, and the disable was being written on that reading alone. The
// threshold is now the bar for spending one upstream request on the question
// rather than the verdict itself, and the model is retired only when a real
// request to it is refused as well.
//
// The probe can only ever PREVENT a disable. It never causes one that three
// strikes had not already earned, so widening it cannot make the gateway retire
// more than it did before.
//
// The whole candidate is taken rather than the model plus a provider name
// because the probe needs a provider to talk to: its base URL, its dialect and
// the already-decrypted key. A name was enough to log with and is not enough to
// verify anything.
//
// Strikes are in-memory and deliberately not persisted. They are a heuristic
// over recent traffic, not an audit trail: losing them on restart just means a
// genuinely dead model re-earns them on the next few requests, while keeping
// the hot path free of a database write per failed request. Each HA member
// therefore reaches its own conclusion from its own traffic, which is the safer
// direction — nothing fans a disable out across the fleet on one member's
// evidence.
func (h *Handler) noteModelGone(candidate modelCandidate, endpointType string) {
	m := candidate.model
	// A nil provider now stops the retirement outright instead of being papered
	// over. Under the old signature it could not: providerName was a string, so
	// a candidate with no provider still counted strikes and still disabled the
	// model. There is nothing to probe without one, and an unprobeable
	// retirement is exactly what this change exists to stop writing.
	if m == nil || m.ID == uuid.Nil || candidate.provider == nil {
		return
	}

	// The family gate, and it runs BEFORE the strike is recorded.
	//
	// Only chat and embeddings can be probed cheaply (see
	// probeEndpointForFamily). A chat probe against an image, TTS, STT or rerank
	// model fails for reasons that have nothing to do with retirement, and that
	// failure would read as confirmation of it — the verification would
	// manufacture the very answer it was added to check. Falling back to
	// classifier-only for those families is not the alternative it looks like
	// either: it would leave the guessing this change removes running
	// unsupervised in the corner where it is least observed.
	//
	// Gating before the strike rather than after is what keeps the counter
	// honest. A streak that can never fire is not evidence, so recording it
	// would only manufacture the appearance of some; and a model that takes
	// chat traffic and image traffic must not have its chat streak — the one
	// that CAN retire it — topped up by image refusals nothing will ever
	// adjudicate.
	//
	// Debug rather than Info, and that is a consequence of returning before the
	// strike rather than a preference. Nothing throttles this line: no counter
	// is kept, so there is no threshold to cross and nothing ever goes quiet. A
	// genuinely retired image or TTS model used to log twice, reach the
	// threshold, be disabled and stop; it now logs once per refused request for
	// as long as traffic keeps arriving, precisely BECAUSE it is never retired
	// to make it stop. At Info that is an unbounded line in every operator's log
	// for a condition that is by design permanent.
	if _, ok := probeEndpointForFamily(endpointType); !ok {
		debuglog.Debug("proxy: provider reports model gone on an endpoint family that cannot be probed, so it is never auto-retired", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType)
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
	strikes := streak.strike(time.Now())

	if strikes < goneStrikeThreshold {
		debuglog.Info("proxy: provider reports model gone", "model", m.ModelID, "provider", candidate.provider.Name, "strikes", strikes, "threshold", goneStrikeThreshold)
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
	// the model ever answers again, and the two paths below that reach no
	// conclusion — a probe that established nothing, a disable that could not be
	// written — clear it so the attempt is retried rather than lost.
	if strikes > goneStrikeThreshold {
		return
	}

	// Threshold reached. Probe and disable out of band: this runs on the request
	// path and must not add latency to the error response the caller is already
	// getting — least of all an upstream round trip to a third party.
	modelID, modelName, provider := m.ID, m.ModelID, candidate.provider.Name

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

		// Ask the provider itself. The three strikes decided this model was
		// worth one upstream request; what retires it is the answer to that
		// request.
		//
		// Placed after the cancelled check above and before the write below, and
		// both halves of that are deliberate. After, because a success that
		// landed while the disable was queued has already settled the question,
		// and paying for a probe to re-establish it would spend an upstream call
		// on an answer already in hand. Before, because the whole point is that
		// the write is not made on the classifier's reading alone.
		//
		// The deadline is created here rather than inside probeModel so the
		// production budget stays at the call site that owns the decision. It is
		// generous — a cold model can take tens of seconds to answer, and a
		// probe that times out on a slow but living model would postpone the
		// retirement rather than confirm it, which is the safe direction but
		// still a wasted call. Nothing on the request path is waiting on it.
		pctx, pcancel := context.WithTimeout(context.Background(), goneProbeTimeout)
		verdict := h.probeModel(pctx, candidate, endpointType)
		pcancel()

		switch verdict {
		case probeServed:
			// The model refused real traffic three times and then answered a
			// direct request. Warn rather than Info: whatever is going on —
			// drifted classifier patterns, a provider returning retirement prose
			// for a transient fault, traffic that carries something the probe
			// does not — an operator should see it, because it is the case in
			// which the old code would have retired a working model.
			//
			// noteModelServed is the existing machinery for "this model works":
			// it stands down any queued disable and clears the streak, so the
			// model needs three FRESH refusals before it is reconsidered. That
			// is the backoff, and it is why no durable "never retire this model"
			// store is being added — a model that is genuinely dead simply earns
			// the strikes again, and one that is alive keeps clearing them.
			debuglog.Warn("proxy: not auto-disabling, the model answered a direct probe after being reported gone", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", goneStrikeThreshold)
			h.noteModelServed(m)
			return
		case probeInconclusive:
			// Nothing was established: a 429, a 5xx, an entitlement failure, a
			// connection that never landed, an expired deadline. Postpone.
			//
			// The model is deliberately NOT credited with a success.
			// noteModelServed is not called, so no in-flight disable is stood
			// down and no cancelled flag is set — the model has not proved
			// anything, and treating "we could not tell" as "it works" would let
			// a provider outage clear the streaks of everything behind it.
			//
			// Dropping the count is a retry mechanism, not a verdict. Without it
			// the streak stays parked at the threshold, where the strikes >
			// goneStrikeThreshold check above makes every later refusal a no-op,
			// and a genuinely dead model whose first probe happened to hit a 429
			// would stay enabled forever. This is the same reasoning, and the
			// same call, as the failed-write path a few lines below —
			// CompareAndDelete on identity included, so a stale goroutine cannot
			// throw away a newer streak on its way out.
			h.goneStrikes.CompareAndDelete(modelID, streak)
			debuglog.Info("proxy: postponing auto-disable, the retirement probe established nothing", "model", modelName, "provider", provider, "endpoint", endpointType)
			return
		case probeRefused:
			// The provider refused the model by name to a request the gateway
			// made itself. That is the fourth independent piece of evidence and
			// the one the disable is actually written on; fall through.
		default:
			// Unreachable while probeVerdict has three values, and present
			// because the cost of the two outcomes is not symmetric. Go does not
			// check switch exhaustiveness and no linter here does either, so a
			// fourth verdict added later would silently fall out of this switch
			// and into the write below — the one direction the probe is not
			// allowed to take, since it may only ever PREVENT a disable. Failing
			// closed means a new verdict postpones until someone decides
			// otherwise, which is the same answer this file gives to every other
			// case it cannot substantiate.
			debuglog.Warn("proxy: postponing auto-disable, unrecognised retirement probe verdict", "model", modelName, "provider", provider, "endpoint", endpointType, "verdict", verdict.String())
			return
		}

		// Staged, not written outright. confirm runs with the row already
		// updated but the transaction still open, so a success that lands
		// before the commit abandons the write instead of producing a disabled
		// state that other sessions can see and act on. That mattered: a
		// concurrent custom-group revalidation sampling the intermediate state
		// auto-disables the group for having too few routable members, and
		// re-enabling the model does not bring the group back.
		dctx, cancel := context.WithTimeout(context.Background(), goneWriteTimeout)
		committed, err := h.modelRepo.AutoRetireIfConfirmed(dctx, modelID, func() bool {
			return !streak.cancelled.Load()
		})
		cancel()
		if err != nil {
			debuglog.Error("proxy: failed to auto-disable retired model", "model", modelName, "provider", provider, "error", err)
			// Clear the streak so the next refusals can rebuild it and try
			// again. Without this a transient database error would leave the
			// count parked above the threshold and the model enabled forever.
			//
			// Conditional on identity, because this goroutine may be stale. It
			// can sit in the write while a success clears the streak it was
			// started for and later refusals build a NEW one; deleting by model
			// id would then throw away that newer count on its way out, and the
			// model would keep restarting from zero instead of reaching the
			// threshold. Only the streak this attempt actually belongs to may
			// be retired by it.
			h.goneStrikes.CompareAndDelete(modelID, streak)
			return
		}

		if !committed {
			// Either the model answered while the write was staged, or the row
			// had already moved on — an operator's own disable or enable, or
			// another member retiring it first. The repository logs which; from
			// here they are the same outcome, and the same correct response is
			// to leave the row alone.
			debuglog.Info("proxy: auto-disable did not commit, the model or its state changed while the write was staged", "model", modelName, "provider", provider)
			return
		}

		// One window is left, and it is the only one staging cannot close: a
		// success that lands after confirm has already returned, while the
		// commit is on its way to the database. That write cannot be recalled,
		// so it is undone instead.
		//
		// Serialising would close it, and is not available here.
		// noteModelServed runs on the request path BEFORE a non-streaming
		// response is written to the client (see proxy_failover.go), so holding
		// a lock across the write would put client latency behind a database
		// write belonging to an unrelated request's error path.
		//
		// The disabled state IS briefly visible on this path, which is what the
		// staging above exists to avoid. Accepted, because the alternative is
		// worse: leaving the model disabled when it has just proved it works.
		// The window is now one commit rather than a whole check-write-check
		// cycle, and the model ends up correct either way.
		if streak.cancelled.Load() {
			// The success that cancelled this retirement can itself be out of
			// date. Its streak was dropped, new refusals can have built another
			// one, and that replacement stands down when it finds the model
			// already retired — by this very write. Reverting on the strength of
			// the older success would then re-enable a model that current
			// evidence says is gone, and the fresh streak, parked above the
			// threshold, would never disable it again.
			//
			// So the revert defers to whatever the model is saying NOW rather
			// than to the success that scheduled it.
			if fresh, ok := h.goneStrikes.Load(modelID); ok {
				if s, ok := fresh.(*goneStreak); ok && s.count() >= goneStrikeThreshold {
					debuglog.Info("proxy: not reverting the auto-disable, the model is refusing again", "model", modelName, "provider", provider)
					return
				}
			}

			rctx, rcancel := context.WithTimeout(context.Background(), goneWriteTimeout)
			// Conditional on the row still being as the retirement left it. An
			// operator can disable the model by hand inside this same window,
			// and an unconditional re-enable would put their disabled model
			// back into routing — replacing a deliberate decision with a stale
			// automatic one.
			reverted, rerr := h.modelRepo.RevertAutoRetire(rctx, modelID)
			rcancel()
			switch {
			case rerr != nil:
				// Nothing safe is left to try. Log loudly: the model is
				// disabled and the gateway believes it should not be.
				debuglog.Error("proxy: model answered while its auto-disable was in flight, and re-enabling it failed", "model", modelName, "provider", provider, "error", rerr)
				return
			case !reverted:
				// Someone else owns the row's state now. Leaving it alone is
				// the whole point of the condition.
				debuglog.Info("proxy: auto-disable was superseded before it could be reverted", "model", modelName, "provider", provider)
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

// streamProducedOutput reports whether a finished stream actually delivered
// content.
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
func streamProducedOutput(logData *requestLogData) bool {
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
	switch verdictForStream(logData.errorKind, logData.upstreamKind, streamProducedOutput(logData)) {
	case verdictGone:
		// The endpoint family comes off the log entry rather than being assumed:
		// it is what decides whether this refusal can be adjudicated at all, and
		// a stream reaching here can have started at /v1/chat/completions or at
		// /v1/messages. newPendingRequestLog stamps it at ingest, so it is set on
		// every path that can reach this.
		h.noteModelGone(candidate, logData.endpointType)
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
	streak, ok := raw.(*goneStreak)
	if !ok {
		return
	}
	streak.cancelled.Store(true)
	// Also conditional on identity: between the load above and here the streak
	// can be dropped by a failing disable and a fresh one started by new
	// refusals, and this success is not evidence about that later streak.
	h.goneStrikes.CompareAndDelete(m.ID, streak)
	debuglog.Debug("proxy: model answered again, cleared gone-strikes", "model", m.ModelID)
}
