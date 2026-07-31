package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
// It bounds the GAP between consecutive strikes, not the span of the streak.
// Refusals at 0, 29 and 58 minutes are one streak of three, because neither gap
// exceeds the window; the count only restarts when a strike arrives more than
// this long after the one before it. Written out because "three strikes within
// 30 minutes" is the natural way to say it and is not what the code does.
//
// Thirty minutes is chosen against real traffic rather than as a round number:
// each refusal only has to arrive within half an hour of the last one, which a
// gateway with any load does easily, while a model refused once an hour never
// accumulates a streak at all. That second case is deliberate — a model too idle
// to fail twice in half an hour is also too idle for its staying enabled to cost
// anything, and traffic-driven retirement should not be guessing from sparse
// evidence.
const goneStrikeWindow = 30 * time.Minute

// goneProbeCooldown is the shortest interval between two probes of the same
// model, and it is what makes an inconclusive probe a postponement rather than a
// standing invitation to keep asking.
//
// The case it exists for is the one the design names: a provider rate limiting
// the gateway. Real traffic draws the retirement prose, the probe draws a 429,
// nothing is established. Without a cooldown the streak had to be dropped so a
// later refusal could re-probe, and the steady state became one extra upstream
// request per three refusals — forever, with no bound. A client retrying a dead
// model at 10 req/s produced roughly three probes a second at a provider that
// was already shedding load, and the probe deliberately bypasses both the rate
// limiter and the circuit breaker, so nothing else was going to slow it down.
// Before the probe existed, strike three disabled the model and the traffic
// stopped; the cooldown is what restores that bound.
//
// The floor is goneProbeTimeout (30s) and it is not a soft one: claimProbe
// claims at SPAWN time, so the claim doubles as the "a probe is already in
// flight" guard, and a cooldown shorter than the probe's own deadline would let
// a second probe be claimed while the first was still waiting on the provider.
// Five minutes clears it by an order of magnitude and caps the cost at 12
// probes per model per hour in the worst case — a model under constant refusal
// whose probe never establishes anything — against the roughly 1,200 an hour
// the uncapped version allowed for the same traffic.
//
// It bounds one model. goneProbeMaxConcurrent bounds the burst across models.
const goneProbeCooldown = 5 * time.Minute

// goneProbeMaxConcurrent bounds how many retirement probes may be in flight
// against one provider at a time.
//
// The cooldown above bounds one model, and one model was never the shape of the
// problem: a provider event that nominates 200 models nominates them all within
// the same few seconds, and every nomination that reached the threshold spawned
// its own goroutine holding a connection to the SAME host for up to
// goneProbeTimeout. Two hundred simultaneous verification requests at a
// provider already misbehaving is the gateway adding to an incident it is
// supposed to be diagnosing — and each HA member does it independently.
//
// Per provider rather than one counter for the whole gateway, because the harm
// is per host. A cap on the total cannot tell 200 probes at one struggling
// provider from 200 spread over 200 healthy ones, so it either throttles the
// case that is fine or fails to throttle the case that is not. Keyed by
// provider it throttles exactly the burst that lands on one host, and a second
// provider's retirements are not postponed by the first provider's incident.
//
// Four slots, following the argon2 semaphore in internal/user: enough that an
// ordinary trickle of retirements never queues, small enough that the worst case
// is a handful of extra connections. The acquire is non-blocking on purpose (see
// probeForRetirement): a probe that cannot get a slot postpones rather than
// waiting, because the streak's nextProbeAt is already stamped and the retry
// arrives on its own after the cooldown. Blocking would hold goroutines and
// slots for work whose whole justification is that it is not urgent.
const goneProbeMaxConcurrent = 4

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

// goneStreakKey identifies a streak: one model, on one upstream surface.
//
// The surface is the PROBE endpoint (see probeEndpointForFamily), not the
// endpoint family, so chat and messages share a streak while embeddings keeps
// its own. That is the right grain because it is exactly what the probe can ask:
// two front doors onto the same question are one question, and a different door
// onto a different question is a different streak.
type goneStreakKey struct {
	model    uuid.UUID
	endpoint string
}

// goneProbeSurfaces is every endpoint a streak can be keyed on.
//
// It has to agree with probeEndpointForFamily's returns, and
// TestProbeEndpointForFamily_SurfacesAreCovered fails if a new probeable family
// introduces a surface that is missing here. The one caller that needs it is
// noteModelServed, which has a model and no family: a success clears the model's
// streaks on every surface, so it enumerates them rather than scanning the map.
var goneProbeSurfaces = [...]string{probeChatEndpoint, probeEmbeddingsEndpoint}

// modalityRulesOutSurface reports whether a gone-classified refusal that arrived
// on this upstream surface may count against the model at all.
//
// The two surfaces answer to different burdens of proof, and the asymmetry is
// the point rather than an inconsistency. What is at stake is the same in both
// cases — the disable is model-wide, so a strike drawn on the wrong surface can
// take a working model out of routing everywhere — but the cost of guessing is
// not.
//
// The embeddings surface requires POSITIVE evidence: the catalog has to say this
// model produces embeddings. Nothing filters a request by modality on the way
// in, so `POST /v1/embeddings` naming a chat model is forwarded to the
// provider's embeddings endpoint, and a provider that answers "gpt-4o is not
// supported for embeddings" has named the model beside a gone-phrase. The probe
// cannot rescue it: it asks on the surface the strikes arrived on, so it
// reproduces the misuse and confirms. Being wrong here retires a live chat model
// gateway-wide; being cautious only means an embeddings model whose catalog
// entry declares nothing is never auto-retired — the same trade
// probeEndpointForFamily already takes for rerank, and it matters because
// liveModelStub writes "[]" for every model no catalog covers.
//
// The chat surface keeps the opposite default, because there the two costs
// invert. Chat is what most models are and what most refusals arrive on, so
// demanding a declared modality would switch traffic-driven retirement off for
// every uncatalogued model at once — silently, and precisely where it does the
// most work. Only a positively embedding-ONLY model rules the chat surface out.
//
// Reading output modalities rather than input: discovery classifies an
// embeddings model by what it PRODUCES (["embedding"]), which is the only field
// that separates it from a chat model taking the same text input.
func modalityRulesOutSurface(m *model.Model, probeEndpoint string) bool {
	var out []string
	if m.OutputModalities != "" && m.OutputModalities != "[]" {
		if json.Unmarshal([]byte(m.OutputModalities), &out) != nil {
			out = nil
		}
	}
	embeds := slices.Contains(out, "embedding")
	if probeEndpoint == probeEmbeddingsEndpoint {
		return !embeds
	}
	// A model that produces embeddings alongside text says nothing against the
	// chat surface; only an embedding-only one does.
	return embeds && len(out) == 1
}

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
// The count, the time of the last strike and the next time a probe may be spent
// are guarded by mu rather than being separate atomics. Atomics cannot express
// any of the three: deciding whether the window has lapsed and then applying the
// decision is one operation, and splitting it loses strikes. A reset racing two
// increments stores 1 after both have added, erasing them, so a model refused
// three times ends the burst on a count of one and never reaches the threshold.
// nextProbeAt is the same shape of decision — read the deadline, decide, stamp
// the new one — and splitting it would let two callers past the same expired
// deadline and issue two probes where the whole point is to issue one.
//
// The lock is held for a comparison and an addition, on a per-model struct that
// is only contended while that one model is failing, so it costs nothing worth
// measuring.
//
// cancelled stays atomic because it is read by the detached disable goroutine
// while the request path may be writing it, and it is independent of the three
// above.
type goneStreak struct {
	mu          sync.Mutex
	n           int64
	lastStrike  time.Time
	nextProbeAt time.Time

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

// claimProbe reports whether this caller may spend an upstream request on the
// model, and takes the right to do so if it may.
//
// It is one operation and not two, which is what makes it serve both of its
// jobs. Claiming at spawn time rather than crediting at completion means a
// second caller arriving while the first probe is still in flight finds the
// stamp already set and drops out, so this is also the "a probe is already in
// flight" guard — hence goneProbeCooldown's floor being the probe's own
// deadline rather than a matter of taste.
//
// The two jobs it replaced:
//
//   - "exactly one caller sees the threshold value", which the old
//     strikes == goneStrikeThreshold test did. A burst of concurrent refusals
//     still issues a single probe, but now because everyone after the first is
//     inside the cooldown rather than because their count happened to overshoot.
//   - the retry, which the inconclusive path used to get by deleting the streak.
//     A parked streak is re-probed by the next refusal past the cooldown, so
//     postponing no longer has to throw the evidence away to stay reachable.
//
// The zero value admits the first caller, which is what a model that has never
// been probed should get.
//
// Granting a claim also clears the cancelled tombstone, and that is what makes
// a streak reusable rather than something to be thrown away. The flag means "a
// success landed after the disable now in flight was decided", so it belongs to
// one decision; a claim starts the next one, on three fresh strikes that the
// success itself is older than. Leaving it set would make the disable this claim
// spawns stand down at its own pre-write check, and the model would never be
// retired again on this streak. Nothing can be racing it: a claim cannot be
// granted while an earlier probe is alive, because goneProbeCooldown is five
// minutes and the whole goroutine lives at most goneProbeTimeout plus one write.
func (s *goneStreak) claimProbe(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nextProbeAt.IsZero() && now.Before(s.nextProbeAt) {
		return false
	}
	s.nextProbeAt = now.Add(goneProbeCooldown)
	s.cancelled.Store(false)
	return true
}

// park clears the evidence a streak has accumulated and keeps the probe
// cooldown it has earned. It is what "clear the streak" means everywhere in
// this file; nothing removes the entry.
//
// Deleting was the obvious spelling and it silently discarded the rate bound.
// nextProbeAt lives on the streak, so dropping the entry drops the stamp, and
// the next refusal builds a fresh zero-valued streak whose claimProbe admits
// immediately. Every reason a streak used to be deleted is a reason it is
// likely to be rebuilt at once: a probe that answered while traffic keeps
// drawing retirement prose, a real request that succeeded between refusals, a
// disable that failed to write. Each of those turned into three fresh refusals
// buying another upstream call, forever, which is precisely the loop
// goneProbeCooldown exists to close.
//
// Parking gives both halves of the bound instead. The count is reset, so the
// model needs three FRESH refusals before it is reconsidered, and the stamp
// survives, so the reconsideration also waits out the cooldown.
//
// Mutating the streak in place rather than reaching into h.goneStrikes by model
// id is what makes every caller identity-scoped for free: sync.Map holds the
// pointer, so a park lands on the map entry while this is still the live streak
// and touches nothing once it is not.
//
// The tombstone is deliberately not touched here: a success has to set it (see
// noteModelServed) and a claim has to clear it (see claimProbe), and both of
// those are decisions about a disable rather than about the evidence.
//
// Nothing is ever deleted, so the map holds one small struct per model that has
// ever drawn a gone-classified refusal. That is bounded by the catalog and is
// the point: a model's probe cooldown has to outlive the streak that earned it.
func (s *goneStreak) park() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

// supersede is park plus the tombstone: the model answered, so a disable that
// has been decided stands down and the evidence behind it is cleared.
//
// One critical section, and that is the whole reason this is not two calls at
// the call site. The disable goroutine reads the tombstone without the lock and
// then asks for the count, so the two updates have to be indivisible from its
// point of view: seeing cancelled set while the count still shows the strikes
// that caused this retirement reads as "the model is refusing again", and the
// revert is skipped — leaving a model that has just answered disabled, with the
// count parked at zero immediately afterwards so nothing ever triggers a revert
// again. An operator would have to re-enable it by hand, which is the exact
// outcome the cancellation exists to prevent.
//
// Storing inside the lock is what closes it: once a reader observes the
// tombstone the writer is still holding mu, so the count it goes on to ask for
// cannot be read until this has finished zeroing it.
//
// It reports whether it changed anything, for the caller's log line rather than
// for control flow. A streak already at zero with the tombstone already set has
// been superseded by an earlier success and there is nothing left to stand down
// — which is the steady state for any model that drew one refusal and then went
// on serving, since nothing removes entries. The early return is deliberately
// that narrow: a count of zero with no tombstone can still belong to a disable
// this success has to cancel.
func (s *goneStreak) supersede() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n == 0 && s.cancelled.Load() {
		return false
	}
	s.cancelled.Store(true)
	s.clearLocked()
	return true
}

// clearLocked resets the evidence. Callers hold mu.
func (s *goneStreak) clearLocked() {
	s.n = 0
	s.lastStrike = time.Time{}
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
	probeEndpoint, ok := probeEndpointForFamily(endpointType)
	if !ok {
		debuglog.Debug("proxy: provider reports model gone on an endpoint family that cannot be probed, so it is never auto-retired", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType)
		return
	}

	// The second gate: the refusal has to be about a surface this model is FOR.
	//
	// This is the same argument as the family gate above, applied to the one
	// direction that gate cannot see. Chat and embeddings are both probeable, so
	// the mismatch is between the model and the surface rather than between the
	// surface and the probe — and the probe cannot catch it, because it asks on
	// the surface the strikes arrived on and would reproduce the misuse
	// faithfully. What each surface demands of the catalog, and why the two
	// differ, is argued in modalityRulesOutSurface.
	if modalityRulesOutSurface(m, probeEndpoint) {
		debuglog.Debug("proxy: ignoring a gone-classified refusal on a surface this model is not known to serve", "model", m.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "output_modalities", m.OutputModalities)
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
	//
	// Keyed by model AND probe surface, not by model alone. The two are separate
	// questions — a model can be served on one surface and refused on another —
	// and the probe is sent to the surface the strikes came from, so pooling them
	// let two chat refusals plus one embeddings refusal buy an embeddings probe.
	// Keyed this way, every streak names one surface, and what the probe asks is
	// always what the strikes were about. Chat and messages share a key by
	// design: they resolve to the same probe endpoint, so they are the same
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
	// own cooldown decides which one gets it. The old shape tested for the
	// threshold value exactly, so only the caller that saw a count of three ever
	// proceeded; claimProbe now does that job and two more besides.
	//
	// What each version buys:
	//
	//   - A burst still issues ONE probe. Fifty concurrent retries all reach
	//     here, all ask, one wins the claim and the other forty-nine drop out
	//     inside the cooldown. Same outcome as the equality test, on a rule that
	//     survives the count moving on.
	//   - A parked streak stays reachable. The equality test made every refusal
	//     past the threshold a permanent no-op, which is why the inconclusive
	//     path below had to DELETE the streak to be retried at all — and
	//     deleting it is what left the probe rate unbounded. With the claim
	//     gating instead, a single refusal after the cooldown re-probes a streak
	//     that is still sitting at three, so postponing costs no evidence and
	//     the retry costs no extra strikes.
	//   - The rate is bounded. See goneProbeCooldown.
	//
	// The counter is still deliberately NOT cleared on the way to a disable.
	// Clearing it would let the next refusal start a fresh count from zero, so a
	// burst against a dead model would re-enter the threshold every three
	// strikes. Exactly two things clear it, and both are the model answering:
	// noteModelServed, when a request to it succeeds, and the served-probe branch
	// below, when the probe gets content out of it. A disable that could not be
	// written clears nothing at all — the streak stays as it is and the next
	// refusal past the cooldown retries it. A retirement that did land does not
	// need the guard either: the model is disabled, so no traffic reaches it to
	// strike it again, and a straggler that arrives past the cooldown finds
	// AutoRetireIfConfirmed refusing to write over an already-retired row, which
	// reports as an uncommitted attempt and publishes no second alert.
	if !streak.claimProbe(now) {
		return
	}

	// Threshold reached. Probe and disable out of band: this runs on the request
	// path and must not add latency to the error response the caller is already
	// getting — least of all an upstream round trip to a third party.
	modelID, modelName, provider := m.ID, m.ModelID, candidate.provider.Name

	go func() {
		// Every other detached goroutine in this package recovers
		// (touchProviderLastUsed, the log writer, the usage recorder) and this
		// one now has far more reason to. It used to call repository methods and
		// nothing else; it now makes an upstream request and runs bytes a
		// provider chose through the dialect translators, so a panic anywhere in
		// json, the transport or a translator would take the whole gateway down
		// over one model's retirement. Recovering leaves the model enabled, which
		// is the direction every unproven case on this path already takes.
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
		// The deadline and the concurrency cap live in probeForRetirement, which
		// is still this file rather than probeModel: the production budget stays
		// with the decision that owns it, and probeModel stays a helper that
		// issues one request and reports what came back.
		verdict := h.probeForRetirement(candidate, endpointType)

		switch verdict {
		case probeServed:
			// The model refused real traffic three times and then answered a
			// direct request. Warn rather than Info: whatever is going on —
			// drifted classifier patterns, a provider returning retirement prose
			// for a transient fault, traffic that carries something the probe
			// does not — an operator should see it, because it is the case in
			// which the old code would have retired a working model.
			//
			// The park is the backoff, and it is why no durable "never retire
			// this model" store is being added: the count is reset, so the model
			// needs three FRESH refusals before it is reconsidered, and one that
			// is genuinely dead simply earns them again while one that is alive
			// keeps clearing them.
			//
			// The streak is parked directly rather than through noteModelServed,
			// which does the same to the count and additionally sets the
			// tombstone. There is nothing here for the tombstone to stand down:
			// the only disable this streak can have queued is this goroutine,
			// which is about to return.
			// The count that claimed the probe, for the reason the disable line
			// below gives: under claimProbe the number that bought it is not
			// always three.
			debuglog.Warn("proxy: not auto-disabling, the model answered a direct probe after being reported gone", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", strikes, "retry_after", goneProbeCooldown.String())
			streak.park()
			return
		case probeInconclusive:
			// Nothing was established: a 429, a 5xx, an entitlement failure, a
			// connection that never landed, an expired deadline, or no probe
			// slot free. Postpone.
			//
			// The count is left exactly where it is, and neither half of that is
			// incidental. Postponing means postponing: the strikes were real
			// evidence and nothing has contradicted them, so throwing them away
			// would make an unanswered question cost the model its whole case.
			//
			// This used to delete the streak, for a reason that no longer holds.
			// Under the old strikes == goneStrikeThreshold gate a parked streak
			// was unreachable — every later refusal was a no-op — so deleting it
			// was the only way a model whose first probe hit a 429 could ever be
			// retired. The cost was that three fresh refusals then bought another
			// probe, and another, with nothing bounding the rate at a provider
			// that was already rate limiting us. claimProbe replaces that: the
			// streak stays parked and the next refusal past goneProbeCooldown
			// re-probes it, so the retirement stays reachable at one probe per
			// cooldown instead of one per three refusals.
			//
			// The model is still deliberately NOT credited with a success.
			// noteModelServed is not called, so no in-flight disable is stood
			// down and no cancelled flag is set — the model has not proved
			// anything, and treating "we could not tell" as "it works" would let
			// a provider outage clear the streaks of everything behind it.
			debuglog.Info("proxy: postponing auto-disable, the retirement probe established nothing", "model", modelName, "provider", provider, "endpoint", endpointType, "strikes", streak.count(), "retry_after", goneProbeCooldown.String())
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
			debuglog.Error("proxy: failed to auto-disable retired model", "model", modelName, "provider", provider, "error", err, "retry_after", goneProbeCooldown.String())
			// Nothing is touched, and that is the retry. The streak keeps its
			// count and its stamp, so the next refusal past goneProbeCooldown
			// claims a probe and the disable is attempted again.
			//
			// This used to delete the streak, for the same reason the
			// inconclusive path did and with the same cost. Under the old
			// strikes == goneStrikeThreshold gate a parked count was
			// unreachable, so dropping it was the only way a model whose write
			// failed could ever be retired; claimProbe made it reachable, and
			// deleting also threw away the cooldown — which turned a database
			// outage into three fresh refusals buying another upstream probe,
			// on repeat, for every model refusing traffic at the time.
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
			// date: it cleared the count, and refusals arriving since can have
			// rebuilt it past the threshold inside this same write window.
			// Reverting on the strength of the older success would re-enable a
			// model that current evidence says is gone, and the rebuilt streak
			// would stand down at its own claim when it found the model already
			// retired — by this very write.
			//
			// So the revert defers to whatever the model is saying NOW rather
			// than to the success that scheduled it. The count is read off this
			// streak directly: nothing removes an entry from h.goneStrikes, so a
			// lookup by model id could only ever return the same struct, and
			// supersede's single critical section is what guarantees the count
			// read here is the one that belongs to the tombstone read above.
			if streak.count() >= goneStrikeThreshold {
				debuglog.Info("proxy: not reverting the auto-disable, the model is refusing again", "model", modelName, "provider", provider)
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

		// The verdict is stamped on the line and in the event, and it is the
		// point of the line rather than decoration. Before the probe existed
		// this log and this alert were written on three classifier readings of
		// provider prose; they now mean something materially different, and they
		// said exactly the same words. An operator upgrading could not tell a
		// verified retirement from an unverified one in their own logs, and the
		// refused verdict is both the common outcome and the only one that costs
		// an upstream call AND writes to the database. The design asked for this
		// directly: the probe is a call the operator did not make, so it should
		// be logged as what it is.
		//
		// The endpoint family rides along for the same reason. It decides which
		// surface was asked and whether the model was eligible for auto-
		// retirement at all, so a retirement that cannot be traced back to a
		// family cannot be argued with.
		//
		// The strike count is the streak's own, not goneStrikeThreshold. The
		// constant was accurate while a retirement could only be triggered by the
		// caller that saw exactly three; claimProbe replaced that gate, so this
		// write can equally stand on a streak sitting at fifty under a retry
		// loop, or on a single refusal that re-claimed a streak parked since the
		// last cooldown. Reporting the constant would tell every operator the
		// same story about a decision that was reached differently.
		//
		// It is the count this refusal SAW, captured before the probe rather than
		// read back here. Nothing clears the counter on the retire path, and this
		// point is up to a probe timeout and a database write later, so reading it
		// now would report a number inflated by every refusal that arrived while
		// the decision was being made — which is the opposite of "how was this
		// decision reached".
		debuglog.Warn("proxy: auto-disabled retired model", "model", modelName, "provider", provider, "strikes", strikes, "endpoint", endpointType, "probe_verdict", probeRefused.String())

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
				"strikes":       strikes,
				"reason":        string(KindProviderModelGone),
				"probe_verdict": probeRefused.String(),
				"endpoint_type": endpointType,
			},
		})
	}()
}

// probeForRetirement runs the pre-retirement probe under the per-provider
// concurrency cap and the production deadline, and reports what it found.
//
// It is a separate function because two budgets belong here and neither belongs
// inside probeModel: the deadline, so the number the operator's cost depends on
// stays beside the decision that spends it rather than buried in a helper (and
// so a test can drive the real probe with a deadline measured in milliseconds),
// and the semaphore, which is about how many retirements may be adjudicated at
// once and says nothing about how one request is made.
//
// A slot that is not free returns probeInconclusive, which is the honest answer
// and not a fallback: no request was sent, so nothing was established, and the
// caller's postpone branch is exactly the right handling. The acquire does not
// block. The streak's nextProbeAt was stamped before this goroutine was
// spawned, so the model is already scheduled to be re-probed after the cooldown
// — waiting here would hold a goroutine open to arrive at the same answer
// later, during the same burst that made slots scarce.
//
// The deadline is generous. A cold model can take tens of seconds to answer,
// and a probe that times out on a slow but living model postpones the
// retirement rather than confirming it, which is the safe direction but still a
// wasted call. Nothing on the request path is waiting on any of it.
func (h *Handler) probeForRetirement(candidate modelCandidate, endpointType string) probeVerdict {
	// Before anything is dereferenced, and that is the point of it being here.
	// probeModel makes the same check, but every field this function touches on
	// the way there — the provider's id for the semaphore, the model's id for
	// the log line — would already have panicked, so the guard downstream was
	// promising a postponement it could never deliver. A panic here is caught by
	// the disable goroutine's recover and reported as a panic, which is the
	// wrong answer to "is this model still served": nothing was established, and
	// nothing being established is what probeInconclusive means.
	if candidate.model == nil || candidate.provider == nil {
		return probeInconclusive
	}

	release, ok := h.acquireProbeSlot(candidate.provider.ID)
	if !ok {
		debuglog.Info("proxy: postponing auto-disable, too many retirement probes are already in flight for this provider", "model", candidate.model.ModelID, "provider", candidate.provider.Name, "endpoint", endpointType, "limit", goneProbeMaxConcurrent, "retry_after", goneProbeCooldown.String())
		return probeInconclusive
	}
	defer release()

	pctx, pcancel := context.WithTimeout(context.Background(), goneProbeTimeout)
	defer pcancel()
	return h.probeModel(pctx, candidate, endpointType)
}

// acquireProbeSlot takes one of the provider's goneProbeMaxConcurrent probe
// slots without waiting, returning the release for it.
//
// Load is tried first and LoadOrStore only on a miss, so the common case — a
// provider whose semaphore already exists, which is every probe after the
// first for that provider — does not allocate and immediately discard a
// channel it will never use. The allocation only happens on a genuine miss,
// and the per-provider semaphore is still created on first use through
// LoadOrStore there, so a race on that miss only duplicates the channel that
// loses, and the winner's is the one everyone counts against — the same
// pattern, and the same reasoning, as the per-model streaks in goneStrikes.
//
// Entries are never removed, and that is a bounded leak by construction rather
// than an oversight: the key space is the operator's configured providers, a
// number in the tens, and each entry is one small channel. Reclaiming them would
// need to prove no probe is in flight for that provider first, which is more
// machinery and more ways to be wrong than the bytes are worth.
func (h *Handler) acquireProbeSlot(providerID uuid.UUID) (release func(), ok bool) {
	raw, loaded := h.goneProbeSlots.Load(providerID)
	if !loaded {
		raw, _ = h.goneProbeSlots.LoadOrStore(providerID, make(chan struct{}, goneProbeMaxConcurrent))
	}
	sem, isChan := raw.(chan struct{})
	if !isChan {
		return nil, false
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	default:
		return nil, false
	}
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
//
// It runs on the request path, so what it costs there is part of the design: a
// model that has never been refused misses the map entirely, and one that has
// pays a miss plus an uncontended mutex on a request that is already waiting on
// an upstream call. Nothing is written and nothing touches the database.
//
// It also cancels a disable that has been decided but not yet written, which is
// why it goes through supersede rather than setting the flag and parking
// separately: a queued disable reads both, and has to see them change together.
//
// The entry is parked, not dropped, and the difference is the probe cooldown.
// Deleting it took nextProbeAt with it, so a success between refusals reset the
// rate bound — and a model that refuses some request shapes while serving
// others produces exactly that interleaving. Three refusals then bought a probe,
// a success wiped the stamp, three more refusals bought another, indefinitely.
// A success clears what the model is accused of; it does not buy the gateway
// another free upstream call. See park.
//
// Every surface, because a success is evidence about the MODEL. The caller has a
// model and no endpoint family (a streaming verdict, a pass-through 2xx and a
// chat 200 all arrive here the same way), and enumerating the two probe surfaces
// is cheaper and more predictable than scanning the map for a model's keys. It
// is also the answer that was wanted before the streaks were split: clearing a
// streak can only ever PREVENT a retirement, so being generous across surfaces
// costs nothing, while a strike is gated tightly in both directions.
//
// supersede reports whether it changed anything, and the log line is conditional
// on that. Since nothing removes entries, a model that drew one refusal at 09:00
// keeps its parked streak for the life of the process, and an unconditional line
// would then claim to have "cleared gone-strikes" on every successful request to
// that model from then on — a log that describes work nobody did.
func (h *Handler) noteModelServed(m *model.Model) {
	if m == nil || m.ID == uuid.Nil {
		return
	}
	for _, endpoint := range goneProbeSurfaces {
		raw, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint})
		if !ok {
			continue
		}
		streak, ok := raw.(*goneStreak)
		if !ok {
			continue
		}
		if streak.supersede() {
			debuglog.Debug("proxy: model answered again, cleared gone-strikes", "model", m.ModelID, "endpoint", endpoint)
		}
	}
}
