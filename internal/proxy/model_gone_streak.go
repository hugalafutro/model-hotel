package proxy

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

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

// goneStreak is one model's consecutive-refusal count plus a tombstone for the
// window between deciding to disable and the write landing.
//
// The counters are guarded by mu rather than being separate atomics, because
// none of the three decisions can be expressed atomically: deciding whether the
// strike window has lapsed and then applying the decision is one operation, and
// splitting it loses strikes (a reset racing two increments stores 1 after both
// have added). nextProbeAt is the same shape — read the deadline, decide, stamp
// the new one — and splitting it would let two callers past the same expired
// deadline and issue two probes where the point is to issue one. The lock is
// held for a comparison and an addition on a per-model struct that is contended
// only while that model is failing.
//
// cancelled is a tombstone for the window between deciding to disable and the
// write landing, during which the model can answer a request and prove it is
// alive. It stays atomic because the detached disable goroutine reads it while
// the request path may be writing it. It is read twice and both reads are
// needed: before the write to prevent a disable now known to be wrong, and after
// it to catch a success that landed while the write was in flight — which the
// first read is by definition too early to see, and which can only be undone
// rather than prevented.
type goneStreak struct {
	mu           sync.Mutex
	n            int64
	lastStrike   time.Time
	nextProbeAt  time.Time
	inconclusive int

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
// It is one operation and not two, which is what makes it serve both jobs.
// Claiming at spawn time rather than crediting at completion means a second
// caller arriving while the first probe is still in flight finds the stamp
// already set and drops out, so this is also the "a probe is already in flight"
// guard — hence goneProbeCooldown's floor being the probe's own deadline. A
// burst of concurrent refusals therefore issues a single probe, and a parked
// streak stays re-probeable by the next refusal past the cooldown, so postponing
// does not have to throw the evidence away to stay reachable.
//
// A zero stamp admits the first caller, which is what a model that has never
// been probed should get.
//
// The count check is the other half of the same critical section and not a
// formality. The caller strikes and then claims, and those are two lock
// acquisitions: a success can land in between, clear the count and set the
// tombstone, and the claim would otherwise spend an upstream request on strikes
// that no longer exist.
//
// Granting a claim also clears the tombstone, which is what makes a streak
// reusable. The flag means "a success landed after the disable now in flight was
// decided", so it belongs to one decision; a claim starts the next one, and the
// count check guarantees the strikes it starts on are newer than any success
// that set the flag. Leaving it set would make the disable this claim spawns
// stand down at its own pre-write check, and the model would never be retired
// again on this streak. Nothing can be racing an earlier probe: goneProbeCooldown
// is five minutes and the whole goroutine lives at most goneProbeTimeout plus
// one write.
func (s *goneStreak) claimProbe(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n < goneStrikeThreshold {
		return false
	}
	if !s.nextProbeAt.IsZero() && now.Before(s.nextProbeAt) {
		return false
	}
	s.nextProbeAt = now.Add(goneProbeCooldown)
	s.cancelled.Store(false)
	return true
}

// canClaimProbe answers the same question as claimProbe without taking anything.
//
// It is not a substitute for the claim and cannot be: two callers can both read
// true and only one may proceed. It exists so the cheap, per-model reason to
// stop is checked before the shared, per-provider one, which is what keeps the
// semaphore counting adjudications rather than refusals — without it, a retry
// storm across models that are all inside their cooldowns holds all four slots
// for the microseconds each take-and-release costs, denying a genuine probe to
// the one model whose cooldown HAS expired.
//
// It mirrors both of claimProbe's conditions, including the count: reading only
// the stamp would let a refusal whose evidence a success had already cleared go
// take a provider slot, to be turned away by the claim a moment later.
//
// The reason is returned because the caller logs it and the two cases are not
// the same event: a cooldown is a deliberate wait, while a count below the
// threshold means a success cleared the evidence between the strike and this
// read.
func (s *goneStreak) canClaimProbe(now time.Time) (ok bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n < goneStrikeThreshold {
		return false, "the strikes were cleared while this refusal was being recorded"
	}
	if !s.nextProbeAt.IsZero() && now.Before(s.nextProbeAt) {
		return false, "the model is inside its probe cooldown"
	}
	return true, ""
}

// park clears the evidence a streak has accumulated and keeps the probe
// cooldown it has earned. It is what "clear the streak" means everywhere in
// this file; nothing removes the entry.
//
// Deleting the entry would silently discard the rate bound: nextProbeAt lives on
// the streak, so the next refusal would build a fresh zero-valued one whose
// claimProbe admits immediately. Every reason a streak is cleared is a reason it
// may be rebuilt at once — a probe that answered while traffic keeps drawing
// retirement prose, a success between refusals, a disable that failed to write —
// so each would turn into three fresh refusals buying another upstream call,
// forever.
//
// Parking keeps both halves of the bound: the count is reset, so the model needs
// three FRESH refusals, and the stamp survives, so the reconsideration also
// waits out the cooldown.
//
// Mutating in place rather than reaching into h.goneStrikes by model id makes
// every caller identity-scoped for free: sync.Map holds the pointer, so a park
// lands on the map entry while this is still the live streak.
//
// The tombstone is deliberately not touched: a success sets it (noteModelServed)
// and a claim clears it (claimProbe), and both are decisions about a disable
// rather than about the evidence.
//
// Nothing is ever deleted, so the map holds one small struct per model that has
// ever drawn a gone-classified refusal — bounded by the catalog, and the point:
// a model's probe cooldown has to outlive the streak that earned it.
func (s *goneStreak) park() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

// supersede is park plus the tombstone: the model answered, so a disable that
// has been decided stands down and the evidence behind it is cleared.
//
// One critical section, which is why this is not two calls at the call site. The
// disable goroutine reads the tombstone without the lock and then asks for the
// count, so the two updates have to be indivisible from its point of view:
// seeing cancelled set while the count still shows the strikes that caused this
// retirement reads as "the model is refusing again", the revert is skipped, and
// the count is parked at zero immediately afterwards so nothing triggers one
// again — leaving a model that has just answered disabled until an operator
// re-enables it by hand. Storing inside the lock closes that: once a reader
// observes the tombstone the writer still holds mu.
//
// It reports whether it changed anything, for the caller's log line rather than
// for control flow. The early return is deliberately narrow — a count of zero
// with no tombstone can still belong to a disable this success has to cancel.
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
//
// The inconclusive run goes with it: it counts probes that could not answer a
// question, so clearing the question ends the run. Every caller is the model
// answering — a success, or a probe that got content out of it — which is the
// outcome the run was waiting for.
func (s *goneStreak) clearLocked() {
	s.n = 0
	s.lastStrike = time.Time{}
	s.inconclusive = 0
}

// noteInconclusiveProbe records a probe that established nothing and reports how
// many in a row this streak has now taken. See goneProbeInconclusiveWarnAfter.
func (s *goneStreak) noteInconclusiveProbe() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inconclusive++
	return s.inconclusive
}

// count reports the current streak length.
func (s *goneStreak) count() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}
