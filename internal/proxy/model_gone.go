package proxy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/metrics"
)

// goneStrikeThreshold is how many consecutive KindProviderModelGone responses a
// model must draw, with no successful request in between, before the gateway
// PROBES it.
//
// It is NOT the retirement bar. What disables a model is probeModel refusing it
// on a request the gateway makes itself (see noteModelGone); the strikes decide
// only that the question is worth one upstream call.
//
// Why traffic and not discovery: a provider listing is not a promise. Google
// kept gemini-2.0-flash in /models for two months after shutting it down,
// OpenCode Zen lists claude-sonnet-4 and refuses it. RecordMissingModels can
// only act when a model leaves the listing, so none of those were going to be
// caught by discovery. The only source that knows a model is dead is a real
// request to it, which is what classifyUpstreamError labels.
//
// Three rather than the discovery sweep's two, because requests can arrive in a
// burst during a provider incident while a scan is a deliberate, spaced
// observation. It is a cost control rather than the last line of defence: a
// wrong nomination costs one cheap request, and the probe throws it out.
const goneStrikeThreshold = 3

// goneStrikeWindow is how long a strike stays part of the streak it belongs to.
//
// A strike arriving after this long starts the streak over rather than adding to
// it, so a retirement is always drawn from one run of recent traffic. Without
// it, two strikes from this morning's incident plus one this afternoon retired a
// model on unrelated evidence — including evidence an operator may already have
// acted on by re-enabling it.
//
// It bounds the GAP between consecutive strikes, not the span of the streak.
// Refusals at 0, 29 and 58 minutes are one streak of three, because neither gap
// exceeds the window. Written out because "three strikes within 30 minutes" is
// the natural way to say it and is not what the code does.
//
// A model refused once an hour therefore never accumulates a streak, which is
// deliberate: one too idle to fail twice in half an hour is also too idle for
// staying enabled to cost anything.
const goneStrikeWindow = 30 * time.Minute

// goneProbeCooldown is the shortest interval between two probes of the same
// model, and it is what makes an inconclusive probe a postponement rather than a
// standing invitation to keep asking.
//
// The case it exists for is a provider rate limiting the gateway: real traffic
// draws the retirement prose, the probe draws a 429, nothing is established.
// Unbounded, a client retrying a dead model at 10 req/s produced roughly three
// probes a second at a provider already shedding load — and the probe
// deliberately bypasses both the rate limiter and the circuit breaker, so
// nothing else was going to slow it down.
//
// The floor is goneProbeTimeout (30s) and it is not a soft one: claimProbe
// claims at SPAWN time, so the claim doubles as the "a probe is already in
// flight" guard, and a shorter cooldown would let a second probe be claimed
// while the first was still waiting on the provider. Five minutes clears that by
// an order of magnitude and caps the worst case at 12 probes per model per hour.
//
// It bounds one model. goneProbeMaxConcurrent bounds the burst across models.
const goneProbeCooldown = 5 * time.Minute

// goneProbeInconclusiveWarnAfter is how many probes in a row may establish
// nothing before the postponement is reported at Warn instead of Info.
//
// Nothing bounds how long a streak can sit unadjudicable. A provider that rate
// limits the gateway, or one whose model cannot be reached on the surface the
// probe asks (an endpointTypeMessages candidate is probed over the
// OpenAI-compatible surface — see newProbeState), postpones every time: the
// model keeps its strikes and spends one upstream request per cooldown for as
// long as traffic keeps refusing it. Before the probe existed that model was
// disabled and the traffic stopped.
//
// The direction is safe and the rate is bounded, but the cost is real and at
// Info it was indistinguishable from an ordinary single postponement. Three in a
// row is roughly fifteen minutes of paying for an answer that is not coming.
//
// It warns on every inconclusive probe past the threshold rather than only on
// the crossing, because the line is what the cost looks like: one per model per
// goneProbeCooldown, exactly the rate of the requests being spent. The run ends
// with the evidence it belonged to — see clearLocked.
const goneProbeInconclusiveWarnAfter = 3

// goneProbeMaxConcurrent bounds how many retirement probes may be in flight
// against one provider at a time.
//
// The cooldown above bounds one model, and one model was never the shape of the
// problem: a provider event nominates 200 models within the same few seconds,
// each spawning its own goroutine holding a connection to the SAME host for up
// to goneProbeTimeout. That is the gateway adding to an incident it is supposed
// to be diagnosing, and each HA member does it independently.
//
// Per provider rather than one counter for the whole gateway, because the harm
// is per host: a total cap cannot tell 200 probes at one struggling provider
// from 200 spread over 200 healthy ones.
//
// Four slots, following the argon2 semaphore in internal/user. The acquire is
// non-blocking: a probe that cannot get a slot drops out rather than waiting and
// costs nothing on the way, since the slot is taken before the model's claim is
// spent (see noteModelGone). Blocking would hold goroutines for work whose whole
// justification is that it is not urgent.
const goneProbeMaxConcurrent = 4

// goneWriteTimeout bounds each out-of-band write the auto-disable makes.
//
// Each write gets its OWN deadline rather than sharing one across the sequence.
// They run one after another, so a shared budget lets a slow first write starve
// everything after it: a disable that took the full ten seconds but succeeded
// would leave group revalidation to fail instantly with a spent deadline —
// exactly when an undersized group most needs resizing. Nothing on the request
// path is waiting on the total.
const goneWriteTimeout = 10 * time.Second

// noteModelGone records one strike against a model the provider refused as
// retired, and once the streak reaches goneStrikeThreshold asks the provider
// directly before disabling anything.
//
// The classifier nominates, the probe adjudicates. Every strike here was read
// out of provider prose by classifyUpstreamError, and prose drifts: a phrasing
// that means "retired" today is one some provider will use for something else
// tomorrow. The threshold is the bar for spending one upstream request on the
// question, not the verdict itself.
//
// The probe can only ever PREVENT a disable. It never causes one that three
// strikes had not already earned.
//
// The whole candidate is taken rather than the model plus a provider name
// because the probe needs a provider to talk to: its base URL, its dialect and
// the already-decrypted key.
//
// Strikes are in-memory and deliberately not persisted. They are a heuristic
// over recent traffic, not an audit trail: losing them on restart means a
// genuinely dead model re-earns them on the next few requests, while the hot
// path stays free of a database write per failed request. Each HA member reaches
// its own conclusion from its own traffic, so nothing fans a disable out across
// the fleet on one member's evidence.
func (h *Handler) noteModelGone(candidate modelCandidate, endpointType string) {
	m := candidate.model
	// A nil provider stops the retirement outright: there is nothing to probe
	// without one, and an unprobeable retirement is what this path exists to stop
	// writing.
	if m == nil || m.ID == uuid.Nil || candidate.provider == nil {
		return
	}

	// The family gate, and it runs BEFORE the strike is recorded.
	//
	// Only chat and embeddings can be probed cheaply (see
	// probeEndpointForFamily). A chat probe against an image, TTS, STT or rerank
	// model fails for reasons that have nothing to do with retirement, and that
	// failure would read as confirmation of it. Falling back to classifier-only
	// for those families would leave the guessing running unsupervised where it
	// is least observed.
	//
	// Before the strike rather than after, because a streak that can never fire
	// is not evidence, and a model taking both chat and image traffic must not
	// have its chat streak — the one that CAN retire it — topped up by image
	// refusals nothing will ever adjudicate.
	//
	// Debug rather than Info because nothing throttles this line: the model is
	// never retired to make it stop, so it fires once per refused request for as
	// long as traffic keeps arriving.
	probeEndpoint, ok := probeEndpointForFamily(endpointType)
	if !ok {
		debuglog.Debug("proxy: provider reports model gone on an endpoint family that cannot be probed, so it is never auto-retired", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType)
		return
	}

	// The second gate: the refusal has to be about a surface this model is FOR.
	//
	// Chat and embeddings are both probeable, so here the mismatch is between the
	// model and the surface rather than between the surface and the probe — and
	// the probe cannot catch it, because it asks on the surface the strikes
	// arrived on and would reproduce the misuse faithfully. What each surface
	// demands of the catalog is argued in modalityRulesOutSurface.
	if modalityRulesOutSurface(m, probeEndpoint) {
		debuglog.Debug("proxy: ignoring a gone-classified refusal on a surface this model is not known to serve", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "input_modalities", m.InputModalities, "output_modalities", m.OutputModalities)
		return
	}

	// The third gate, and the one that is about the ACTION rather than the
	// evidence. Everything above decides which surface a refusal is about; the
	// disable it can lead to is model-wide, and for a model that serves both
	// probeable surfaces those two are not the same scope. See
	// modalityAdmitsBothProbeSurfaces.
	if modalityAdmitsBothProbeSurfaces(m) {
		debuglog.Debug("proxy: ignoring a gone-classified refusal on a model that serves both probeable surfaces, which one disable cannot separate", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "input_modalities", m.InputModalities, "output_modalities", m.OutputModalities)
		return
	}

	// A dead model is exactly the one that gets hammered concurrently — clients
	// retry it, and a failover group can try it on several requests at once — so
	// a lost update is not theoretical: two racing refusals would store the same
	// increment, the streak would stall below the threshold, and the model would
	// stay routable indefinitely. sync.Map holds a per-model *goneStreak, so
	// LoadOrStore only races on creating it (harmless, the winner's is used by
	// everyone) and the increment itself is guarded.
	//
	// Keyed by model AND probe surface, not by model alone: a model can be served
	// on one surface and refused on another, and the probe is sent to the surface
	// the strikes came from, so pooling them let two chat refusals plus one
	// embeddings refusal buy an embeddings probe. Chat and messages share a key
	// by design — they resolve to the same probe endpoint, so they are one
	// question asked through two front doors.
	raw, _ := h.goneStrikes.LoadOrStore(goneStreakKey{model: m.ID, endpoint: probeEndpoint}, &goneStreak{})
	streak, ok := raw.(*goneStreak)
	if !ok {
		return
	}
	now := time.Now()
	strikes := streak.strike(now)

	if strikes < goneStrikeThreshold {
		debuglog.Info("proxy: provider reports model gone", "model", m.ModelID, "provider", candidate.provider.Name, "strikes", strikes, "threshold", goneStrikeThreshold)
		return
	}

	// At or above the threshold every refusal asks for a probe, and the streak's
	// own cooldown decides which one gets it. A burst issues ONE probe — fifty
	// concurrent retries all ask, one wins the claim, the rest drop out inside
	// the cooldown — and a parked streak stays reachable, so a single refusal
	// after the cooldown re-probes a streak still sitting at three. Postponing
	// therefore costs no evidence and the retry costs no extra strikes.
	//
	// The counter is deliberately NOT cleared on the way to a disable: clearing
	// it would let a burst against a dead model re-enter the threshold every
	// three strikes. Exactly two things clear it and both are the model
	// answering: noteModelServed, and the served-probe branch below. A disable
	// that could not be written clears nothing, so the next refusal past the
	// cooldown retries it. A retirement that did land needs no guard either — the
	// model is disabled, so no traffic reaches it, and a straggler finds
	// AutoRetireIfConfirmed refusing to write over an already-retired row.
	//
	// The provider's slot is taken BEFORE the model's claim. Both are
	// non-blocking, but only one is spent: the claim stamps a five-minute
	// cooldown, while a refused slot costs nothing. Taking the claim first meant
	// a model that lost the race for a slot had already burnt its cooldown for
	// zero upstream work — so a provider incident nominating two hundred models
	// would adjudicate four and push the rest out a full cooldown each,
	// converging in hours rather than in the time four slots take to turn over.
	// The refused caller leaves the streak untouched and the next refusal retries
	// immediately, which is safe because the semaphore is itself the rate bound
	// in that window.
	//
	// The cooldown is read FIRST, without taking anything, so a refusal that
	// could not have probed anyway never reaches the semaphore. See
	// canClaimProbe, which also explains why that read cannot replace the claim.
	if claimable, reason := streak.canClaimProbe(now); !claimable {
		// The steady state for every refusal against a model already at the
		// threshold. Without a line here, strikes 1 and 2 log at Info and then
		// five minutes go quiet, with nothing to distinguish waiting out the
		// cooldown from a strike dropped by one of the gates above.
		//
		// Debug because nothing throttles it: a client retry loop against a dead
		// model reaches it on every request.
		debuglog.Debug("proxy: postponing auto-disable, "+reason, "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "strikes", streak.count(), "retry_after", goneProbeCooldown.String())
		return
	}

	// The breaker is read before the claim for the same reason the cooldown is: a
	// claim must only be spent on a probe that can actually leave the process.
	// probeModel asks this too, but from inside the goroutine, by which point the
	// five minutes are already gone.
	//
	// The window is narrow: resolveCandidates skips providers whose circuit is
	// open, so while it stays open nothing is routed to them and no strike is
	// recorded. Only nominations already at the threshold when the circuit opened
	// are affected, and each is delayed once.
	//
	// GetState rather than IsOpen, exactly as probeModel does — IsOpen is the
	// routing gate and would spend the provider's one half-open trial slot on a
	// request the operator never made. The check inside probeModel stays where it
	// is: it is that function's own contract.
	if h.circuitBreaker != nil && h.circuitBreaker.GetState(candidate.provider.ID) == failover.StateOpen {
		debuglog.Debug("proxy: postponing auto-disable, the provider's circuit is open", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType)
		return
	}

	release, ok := h.acquireProbeSlot(candidate.provider.ID)
	if !ok {
		// Debug because nothing throttles this line: the slot is taken before the
		// claim is spent, so every refusal past the cooldown reaches it, and the
		// case it reports is a provider-wide nomination event under a client's
		// retry loop. The postponement costs nothing and is retried by the next
		// refusal.
		debuglog.Debug("proxy: postponing auto-disable, too many retirement probes are already in flight for this provider", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "limit", goneProbeMaxConcurrent)
		return
	}
	if !streak.claimProbe(now) {
		// Another refusal won the claim between the read above and here. It owns
		// the probe; this one gives the slot back and drops out.
		release()
		return
	}

	// Threshold reached. Probe and disable out of band: this runs on the request
	// path and must not add latency to the error response the caller is already
	// getting — least of all an upstream round trip to a third party.
	modelID, modelName, provider := m.ID, m.ModelID, candidate.provider.Name

	// announceRetirement is everything a disable owes the rest of the system once
	// the row is actually off, in one function because the goroutine below has
	// four exits that leave it off and only one that does not.
	//
	// The revalidation is unconditional among those four: disabling a member does
	// not resize the custom failover groups it belongs to, so a group can be left
	// enabled with fewer than two routable members until some unrelated scan
	// notices. Discovery already revalidates after its own disables (see
	// discovery_diff.go). Best-effort — a failure here must not undo the disable.
	//
	// The alert is a parameter rather than assumed because it asserts that THIS
	// model is retired, so it is published only where that is still known to be
	// true.
	announceRetirement := func(publish bool) {
		if h.failoverRepo != nil {
			vctx, vcancel := context.WithTimeout(context.Background(), goneWriteTimeout)
			_, verr := h.failoverRepo.RevalidateCustomGroups(vctx)
			vcancel()
			if verr != nil {
				debuglog.Error("proxy: custom-group revalidation after auto-disable failed", "model", modelName, "error", verr)
			}
		}
		if !publish {
			return
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
				"strikes":       strikes,
				"reason":        string(KindProviderModelGone),
				"probe_verdict": probeRefused.String(),
				"endpoint_type": endpointType,
			},
		})
	}

	go func() {
		// The slot belongs to the upstream request, not to the whole retirement.
		// It is released explicitly the moment the probe returns, below; this is
		// the net for the paths that never get there — the cancelled check, and
		// a panic. release is idempotent, so both firing is fine.
		defer release()

		// Every other detached goroutine in this package recovers
		// (touchProviderLastUsed, the log writer, the usage recorder), and this
		// one makes an upstream request and runs bytes a provider chose through
		// the dialect translators — so a panic in json, the transport or a
		// translator would take the whole gateway down over one model's
		// retirement. Recovering leaves the model enabled, which is the direction
		// every unproven case on this path takes.
		defer func() {
			if r := recover(); r != nil {
				debuglog.Error("proxy: panic while auto-disabling a retired model", "model", modelName, "provider", provider, "error", r)
			}
		}()

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

		// Ask the provider itself. The strikes decided this model was worth one
		// upstream request; what retires it is the answer.
		//
		// After the cancelled check because a success that landed while the
		// disable was queued has already settled the question, and before the
		// write because the whole point is that the write is not made on the
		// classifier's reading alone.
		verdict := h.probeForRetirement(candidate, endpointType)
		// The upstream request is over, so the provider's slot goes back now
		// rather than at the end of the write. Holding it across
		// AutoRetireIfConfirmed and the revalidation would tie a cap on
		// CONNECTIONS to how fast the database is.
		release()

		switch verdict {
		case probeServed:
			// The model refused real traffic and then answered a direct request.
			// Warn rather than Info: whatever is going on — drifted classifier
			// patterns, a provider returning retirement prose for a transient
			// fault, traffic carrying something the probe does not — an operator
			// should see it, because it is the case in which the classifier alone
			// would have retired a working model.
			//
			// The park is the backoff, and why no durable "never retire this
			// model" store is needed: the count is reset, so a genuinely dead
			// model simply earns three FRESH refusals again while a live one keeps
			// clearing them.
			//
			// Parked directly rather than through noteModelServed because there is
			// nothing here for the tombstone to stand down: the only disable this
			// streak can have queued is this goroutine, which is about to return.
			debuglog.Warn("proxy: not auto-disabling, the model answered a direct probe after being reported gone", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", strikes, "retry_after", goneProbeCooldown.String())
			streak.park()
			return
		case probeInconclusive:
			// Nothing was established: a 429, a 5xx, an entitlement failure, a
			// connection that never landed, or an expired deadline. Postpone.
			//
			// The count is left exactly where it is. The strikes were real
			// evidence and nothing has contradicted them, so an unanswered
			// question must not cost the model its whole case; the next refusal
			// past goneProbeCooldown re-probes the parked streak.
			//
			// The model is NOT credited with a success either. noteModelServed is
			// not called, so no in-flight disable stands down and no cancelled
			// flag is set: treating "we could not tell" as "it works" would let a
			// provider outage clear the streaks of everything behind it.
			//
			// Only verdicts are reported here. The postponements that happen
			// before this goroutine exists — a busy semaphore, an open circuit, a
			// cooldown still running — never produce one, and log on their own
			// lines on the request path.
			if run := streak.noteInconclusiveProbe(); run >= goneProbeInconclusiveWarnAfter {
				// A model that cannot be adjudicated is costing an upstream
				// request per cooldown indefinitely. See
				// goneProbeInconclusiveWarnAfter.
				debuglog.Warn("proxy: auto-disable postponed repeatedly, the retirement probe keeps establishing nothing", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", streak.count(), "inconclusive_probes", run, "retry_after", goneProbeCooldown.String())
				return
			}
			debuglog.Info("proxy: postponing auto-disable, the retirement probe established nothing", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", streak.count(), "retry_after", goneProbeCooldown.String())
			return
		case probeRefused:
			// The provider refused the model by name to a request the gateway
			// made itself. That is the fourth independent piece of evidence and
			// the one the disable is actually written on; fall through.
		default:
			// Unreachable while probeVerdict has three values, and present
			// because Go does not check switch exhaustiveness and no linter here
			// does either: a fourth verdict added later would fall through into
			// the write below, the one direction the probe is not allowed to
			// take. Failing closed postpones until someone decides otherwise.
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
			debuglog.Error("proxy: failed to auto-disable retired model", "model", modelName, "provider", provider, "error", err, "retry_after", goneProbeCooldown.String())
			// Nothing is touched, and that is the retry. The streak keeps its
			// count and its stamp, so the next refusal past goneProbeCooldown
			// claims a probe and the disable is attempted again. Dropping the
			// streak instead would throw away the cooldown with it, turning a
			// database outage into three fresh refusals buying another upstream
			// probe on repeat, for every model refusing traffic at the time.
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
		// success that lands after confirm has returned, while the commit is on
		// its way to the database. That write cannot be recalled, so it is undone
		// instead.
		//
		// Serialising would close it and is not available here: noteModelServed
		// runs on the request path (see proxy_failover.go), so holding a lock
		// across the write would put client latency behind a database write
		// belonging to an unrelated request's error path.
		//
		// The disabled state IS briefly visible here, which is what the staging
		// above exists to avoid. Accepted, because the alternative is leaving the
		// model disabled when it has just proved it works, and the window is one
		// commit rather than a whole check-write-check cycle.
		if streak.cancelled.Load() {
			// The success that cancelled this retirement can itself be out of
			// date: it cleared the count, and refusals arriving since can have
			// rebuilt it past the threshold inside this same write window.
			// Reverting on the older success would re-enable a model that current
			// evidence says is gone, and the rebuilt streak would stand down at
			// its own claim on finding the model already retired — by this write.
			// So the revert defers to what the model is saying NOW.
			//
			// The count is read off this streak directly: nothing removes an entry
			// from h.goneStrikes, and supersede's single critical section is what
			// guarantees the count read here belongs to the tombstone read above.
			//
			// Three of the four ways out leave the model DISABLED and each still
			// owes the retirement its revalidation; only a revert that landed is
			// exempt, because then nothing happened as far as anyone outside this
			// goroutine is concerned. The alert is judged separately because it
			// asserts something stronger: it is published only on the arms where
			// the row is still exactly as this write left it.
			if streak.count() >= goneStrikeThreshold {
				debuglog.Info("proxy: not reverting the auto-disable, the model is refusing again", "model", modelName, "provider", provider)
				announceRetirement(true)
				return
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
				// disabled and the gateway believes it should not be. The
				// retirement is still announced, because it is what happened —
				// an operator reading only the error line would have a model
				// disabled by this path with no event to match it to.
				debuglog.Error("proxy: model answered while its auto-disable was in flight, and re-enabling it failed", "model", modelName, "provider", provider, "error", rerr)
				announceRetirement(true)
				return
			case !reverted:
				// Someone else owns the row's state now. Leaving it alone is
				// the whole point of the condition — and it is also why this
				// one revalidates without alerting: the groups have to be
				// re-checked whatever the row ended up as, but this goroutine
				// no longer knows what to claim about it.
				debuglog.Info("proxy: auto-disable was superseded before it could be reverted", "model", modelName, "provider", provider)
				announceRetirement(false)
				return
			}
			debuglog.Info("proxy: reverted auto-disable, model answered while the write was in flight", "model", modelName, "provider", provider)
			// Deliberately no alert and no group revalidation: as far as
			// operators are concerned nothing happened, and publishing a
			// retirement for a model that is still enabled would be a lie.
			return
		}

		// The verdict is stamped on the line and in the event so a verified
		// retirement can be told from an unverified one: the words were identical
		// before the probe existed, and the probe is a call the operator did not
		// make. The endpoint family rides along because it decides which surface
		// was asked and whether the model was eligible for auto-retirement at all.
		//
		// The strike count is the streak's own, not goneStrikeThreshold: under
		// claimProbe this write can stand on a streak sitting at fifty under a
		// retry loop, or on a single refusal that re-claimed a parked streak.
		//
		// It is the count this refusal SAW, captured before the probe rather than
		// read back here — this point is up to a probe timeout and a database
		// write later, so reading it now would report a number inflated by every
		// refusal that arrived while the decision was being made.
		debuglog.Warn("proxy: auto-disabled retired model", "model", modelName, "provider", provider, "strikes", strikes, "endpoint", endpointType, "probe_verdict", probeRefused.String())
		announceRetirement(true)
	}()
}

// probeForRetirement runs the pre-retirement probe under the production
// deadline and reports what it found.
//
// Separate from probeModel because the deadline belongs beside the decision that
// spends it rather than buried in a helper, and a test can then drive the real
// probe with a deadline measured in milliseconds.
//
// The per-provider slot is NOT taken here. The caller takes it before the
// model's claim is spent, because a refused slot must cost nothing — see
// noteModelGone.
//
// The deadline is generous: a cold model can take tens of seconds, and a probe
// that times out on a slow but living model postpones rather than confirms.
// Nothing on the request path is waiting on any of it.
//
// The context is Background and not tied to server shutdown, which is what the
// detachment costs. A nomination landing just before shutdown can spend one
// upstream request and then find a closing pool, which produces a failed write
// on a path that already treats one as "change nothing and retry". Closing that
// properly means a handler-scoped parent context, a lifecycle this Handler does
// not have.
func (h *Handler) probeForRetirement(candidate modelCandidate, endpointType string) probeVerdict {
	// Before anything is dereferenced. probeModel makes the same check, but the
	// fields this function touches on the way there would already have panicked,
	// so the guard downstream was promising a postponement it could not deliver.
	// A panic is the wrong answer to "is this model still served": nothing was
	// established, which is what probeInconclusive means.
	if candidate.model == nil || candidate.provider == nil {
		return probeInconclusive
	}

	pctx, pcancel := context.WithTimeout(context.Background(), goneProbeTimeout)
	defer pcancel()
	verdict := h.probeModel(pctx, candidate, endpointType)

	// The metric is recorded HERE rather than at probeModel's many exits or at
	// the caller's switch, because this is the one function every production
	// probe passes through exactly once and it holds the verdict whole. The
	// switch in noteModelGone would need four call sites and would miss the
	// default arm's meaning; probeModel would need one per early return.
	//
	// Above the nil guard there is deliberately nothing to count: that return
	// has no provider or model to label, and it is defensive rather than
	// reachable — noteModelGone checks both before spawning the goroutine.
	//
	// What is counted is one ADJUDICATION ATTEMPT: a nomination that got past
	// every gate in noteModelGone and spent the model's five-minute claim. The
	// postponements before that point — a busy semaphore, an open circuit, a
	// cooldown still running — cost nothing and are deliberately not counted.
	//
	// Not the same as "an upstream request was sent", and the difference is the
	// inconclusive bucket's. probeModel returns inconclusive from a few paths
	// that bail before the request leaves the process: a request that would not
	// build, a missing transport, or the circuit opening in the window after
	// noteModelGone's own check. Those are rarer than the network cases and
	// indistinguishable from them here, which is why neither this metric nor the
	// wiki describes inconclusive as a spend figure. It is inexact in the other
	// direction too: a panic inside probeModel is caught by the goroutine's
	// recover, so a request that WAS sent can end up counted as nothing at all.
	metrics.RecordRetirementProbe(candidate.provider.Name, candidate.model.ModelID, verdict.String())
	return verdict
}

// acquireProbeSlot takes one of the provider's goneProbeMaxConcurrent probe
// slots without waiting, returning the release for it.
//
// The release is idempotent because the caller has two of them: an explicit one
// the moment the upstream request is done, so the slot is not held across a
// database write, and a deferred one covering the paths that never reach it.
// Without sync.OnceFunc the second call would drain a slot belonging to somebody
// else's probe.
//
// Load is tried first and LoadOrStore only on a miss, so the common case does
// not allocate a channel it will never use. A race on that miss only duplicates
// the channel that loses — the same pattern as the per-model streaks in
// goneStrikes.
//
// Entries are never removed: the key space is the operator's configured
// providers, a number in the tens, each one small channel. Reclaiming them would
// need to prove no probe is in flight for that provider first.
func (h *Handler) acquireProbeSlot(providerID uuid.UUID) (release func(), ok bool) {
	raw, loaded := h.goneProbeSlots.Load(providerID)
	if !loaded {
		raw, _ = h.goneProbeSlots.LoadOrStore(providerID, make(chan struct{}, goneProbeMaxConcurrent))
	}
	sem, isChan := raw.(chan struct{})
	if !isChan {
		// Unreachable by construction: this function is the only writer of the
		// map and only ever stores a chan struct{}. Logged rather than left
		// silent because the caller cannot tell it apart from a full semaphore
		// and would report the postponement as probe throttling.
		debuglog.Error("proxy: retirement probe slot map holds an unexpected value, so no slot can be taken", "provider_id", providerID, "type", fmt.Sprintf("%T", raw))
		return nil, false
	}
	select {
	case sem <- struct{}{}:
		return sync.OnceFunc(func() { <-sem }), true
	default:
		return nil, false
	}
}
