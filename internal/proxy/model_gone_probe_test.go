package proxy

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Retirement tests about the PROBE: claiming a slot, the cooldown, what an
// inconclusive or rate-limited probe does, and when a probe retires a model.

// TestNoteModelGone_RealRetirementStillDisables is the paired positive: the
// veto must not have blunted the feature itself.
func TestNoteModelGone_RealRetirementStillDisables(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")
	body := `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available. Please update your code to use a newer model."}}`

	for range goneStrikeThreshold {
		if kind, _ := classifyUpstreamError(404, body, "gemini-2.0-flash"); kind == KindProviderModelGone {
			h.noteModelGone(cand, endpointTypeChat)
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("a genuinely retired model must still be disabled, got %d disable calls", len(calls))
	}
}

// TestNoteModelGone_AnsweredProbePreventsTheDisable is the whole point of
// verifying before retiring: three refusals read out of provider prose are a
// nomination, not a verdict, and a model that answers a request the gateway
// makes itself is not retired however the classifier read the traffic.
//
// Delete this and a drifted gone-pattern — a phrasing some provider starts using
// for a transient fault — takes a working model out of routing after three
// requests, which is exactly what the old code did.
func TestNoteModelGone_AnsweredProbePreventsTheDisable(t *testing.T) {
	t.Parallel()

	// Guard on the fixture rather than trusting it. A served verdict and an
	// inconclusive one are indistinguishable from outside — neither writes
	// anything and both drop the streak — so a body that failed to parse would
	// let every assertion below pass without the served branch ever running.
	if !probeDeliveredContent(endpointTypeChat, []byte(goneServedAnswer)) {
		t.Fatal("the fixture answer does not read as served, so this test would pass on an inconclusive probe")
	}

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	// disableCalls records every attempt, committed or not, and RevertAutoRetire
	// records into the same list — so an empty list is the full claim: nothing
	// was written, and nothing had to be undone.
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered a direct probe must not be retired, got %+v", calls)
	}
	// The count is reset by the served branch, so the model needs three FRESH
	// refusals before it is reconsidered. The entry itself stays — it is what
	// carries the probe cooldown, which the test below is about.
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)
	// And the outcome came from asking rather than from nothing happening.
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeChatEndpoint, paths)
	}
}

// TestNoteModelGone_AnsweredProbeKeepsTheCooldown pins the bound on the one
// verdict that is expected to REPEAT.
//
// A model whose real traffic keeps drawing retirement prose while a minimal
// probe keeps answering is not an edge case: it is the disagreement the whole
// feature exists to catch, and it does not resolve itself. Three refusals, a
// probe, a served verdict, and the traffic goes straight back to refusing. If
// the served branch drops the whole streak the way a real success does, the
// cooldown goes with it, the rebuilt streak starts from a zero-valued one that
// admits immediately, and the gateway probes once per three refusals for as long
// as the disagreement lasts — roughly three upstream requests a second under a
// client retry loop, at a provider that is already answering everything with an
// error.
//
// Delete this test and that regression is invisible: every other served-probe
// assertion still passes, because the model is still not retired.
func TestNoteModelGone_AnsweredProbeKeepsTheCooldown(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	// Wait for the probe to have happened, then for the served branch to have
	// finished with the streak. Without both, "no second probe" and "the first
	// probe has not landed yet" are the same observation.
	waitForProbes(t, script, 1)
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)

	// The traffic goes back to refusing, which is what a real disagreement looks
	// like. The count is allowed to climb again — that is the backoff working —
	// but not one of these may buy an upstream request inside the cooldown.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered a direct probe must not be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("refusals inside the cooldown must cost no further upstream requests, got %v", paths)
	}

	// And it is a delay rather than a lockout: once the cooldown lapses the model
	// is reconsidered on the strikes it has rebuilt since.
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)
	waitForProbes(t, script, 2)
}

// TestNoteModelGone_ARetirementThatStandsIsStillAnnounced pins that a disable
// which stuck gets its follow-through.
//
// Once AutoRetireIfConfirmed commits, the row is off. Three of the four ways out
// of the post-commit cancelled branch leave it off — no revert attempted, a
// revert that errored, a revert the row had moved past — and all three used to
// return before the custom-group revalidation and the alert. The revalidation is
// the load-bearing half: a disabled member does not resize the groups it belongs
// to, so skipping it leaves a group enabled with fewer than two routable members
// until an unrelated scan notices.
//
// The arm driven here is the one where the model is refusing again, which is
// also the one where the retirement is most clearly real.
func TestNoteModelGone_ARetirementThatStandsIsStillAnnounced(t *testing.T) {
	// Not parallel: it subscribes to the process-wide event bus.
	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	cand := goneCandidateFor(t, m, "Google AI Studio (Gemini)")

	// A success lands inside the commit window and fresh refusals rebuild the
	// case behind it, so the revert stands down and the model stays retired.
	repo.afterConfirm = func() {
		h.noteModelServed(m, endpointTypeChat)
		for range goneStrikeThreshold {
			h.noteModelGone(cand, endpointTypeChat)
		}
	}

	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "model.auto_disabled_gone" || ev.Metadata["model_uuid"] != m.ID.String() {
				continue
			}
			return
		case <-deadline:
			t.Fatal("a retirement that was never reverted was never announced")
		}
	}
}

// TestNoteModelGone_TheRetirementEventReportsTheRealStrikeCount pins that the
// number an operator reads is the number the decision was made on.
//
// Reporting goneStrikeThreshold would be accurate only if a retirement could
// only be triggered by the caller that saw exactly three. Under claimProbe a
// write can equally stand on a single refusal that re-claimed a streak parked
// since the last cooldown, or on a streak already past the threshold.
//
// The sequence below produces three different plausible answers, which is the
// point of its shape: the constant would say 3, a count read back after the
// probe and the write would say 14, and the count the claiming refusal actually
// saw is 4. Only the last one answers "how was this decision reached".
func TestNoteModelGone_TheRetirementEventReportsTheRealStrikeCount(t *testing.T) {
	// Not parallel: it subscribes to the process-wide event bus.
	const extraRefusals = 10

	repo := &mockModelRepo{
		setEnabledGate:    make(chan struct{}),
		setEnabledEntered: make(chan struct{}),
	}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	// Rate limited first, so the first probe establishes nothing and parks the
	// streak at the threshold instead of retiring on it.
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "Google AI Studio (Gemini)", srv.URL)

	ch := events.Subscribe()
	defer events.Unsubscribe(ch)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	waitForProbes(t, script, 1)

	// The incident passes and the model turns out to be genuinely gone. One
	// refusal now claims the probe, and the count it sees is one past the
	// threshold — a number no constant could produce.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	select {
	case <-repo.setEnabledEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the disable write never started")
	}

	// The client keeps retrying the dead model while the write is held open.
	// None of these buys a probe (the cooldown is running again) and none of
	// them was part of the decision, but every one is a strike.
	for range extraRefusals {
		h.noteModelGone(cand, endpointTypeChat)
	}
	close(repo.setEnabledGate)

	want := int64(goneStrikeThreshold + 1)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			// The bus fans out to every subscriber, so other tests' events
			// interleave; filter to this model's retirement.
			if ev.Type != "model.auto_disabled_gone" || ev.Metadata["model_uuid"] != m.ID.String() {
				continue
			}
			if got := ev.Metadata["strikes"]; got != want {
				t.Fatalf("event reported strikes = %v (%T), want %d", got, got, want)
			}
			return
		case <-deadline:
			t.Fatal("the retirement was never announced")
		}
	}
}

// TestProbeForRetirement_NilCandidatePostponesInsteadOfPanicking pins that the
// guard is where the dereferences are.
//
// probeModel checks the same two fields, but every field probeForRetirement
// touches on the way there — the provider's id for the semaphore, the model's id
// for the postpone log — would already have panicked, so the downstream guard
// was promising an outcome it could never deliver. The panic is caught by the
// disable goroutine's recover and reported as a panic, which is the wrong answer
// to "is this model still served": nothing was established, and that is what
// probeInconclusive means.
func TestProbeForRetirement_NilCandidatePostponesInsteadOfPanicking(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	cases := []struct {
		name      string
		candidate modelCandidate
	}{
		{"no provider", modelCandidate{model: &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}}},
		{"no model", modelCandidate{provider: &provider.Provider{ID: uuid.New(), Name: "Google AI Studio (Gemini)"}}},
		{"neither", modelCandidate{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := h.probeForRetirement(tc.candidate, endpointTypeChat); got != probeInconclusive {
				t.Fatalf("verdict = %s, want inconclusive", got)
			}
		})
	}
}

// TestGoneStreak_ASuccessBetweenTheStrikeAndTheClaimWins pins the window between
// the two lock acquisitions noteModelGone makes.
//
// A refusal strikes and then claims, and a success can land in between: it
// clears the count and sets the tombstone, and the claim would then proceed on
// strikes that no longer exist — spending an upstream request, wiping the
// tombstone that success set, and reporting a strike count the streak does not
// hold. The claim reads the count for exactly this reason, which is what makes
// the comment's "a claim starts the next decision, on strikes the success is
// older than" true rather than merely usual.
func TestGoneStreak_ASuccessBetweenTheStrikeAndTheClaimWins(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	now := time.Now()
	for range goneStrikeThreshold {
		s.strike(now)
	}

	// The success lands after the last strike and before the claim.
	s.supersede()

	if ok, reason := s.canClaimProbe(now); ok {
		t.Error("the cheap read admitted a claim on evidence a success had already cleared")
	} else if !strings.Contains(reason, "cleared") {
		t.Errorf("reason = %q, want the cleared-strikes reason rather than the cooldown one", reason)
	}
	if s.claimProbe(now) {
		t.Fatal("claimed a probe on evidence a success had already cleared")
	}
	if !s.cancelled.Load() {
		t.Fatal("the claim wiped the tombstone the success set, so the queued disable would not stand down")
	}

	// Fresh refusals rebuild the case, and then the claim is real again.
	for range goneStrikeThreshold {
		s.strike(now)
	}
	if !s.claimProbe(now) {
		t.Fatal("three fresh strikes must buy a claim")
	}
	if s.cancelled.Load() {
		t.Error("a claim on strikes newer than the success must clear the tombstone")
	}
}

// TestGoneStreak_CanClaimProbeDoesNotTakeTheClaim pins the read that lets the
// cheap per-model reason to stop be checked before the shared per-provider one.
//
// It has to agree with claimProbe and it has to take nothing. If it stamped, a
// refusal that only asked whether a probe was possible would extend the cooldown
// it was asking about; if it disagreed, a model would either be denied a probe
// it had earned or reach the semaphore on every refusal, which is what put the
// per-provider slots in the path of traffic that was never going to spend one.
func TestGoneStreak_CanClaimProbeDoesNotTakeTheClaim(t *testing.T) {
	t.Parallel()

	s := &goneStreak{}
	now := time.Now()
	// A claim needs evidence as well as an expired cooldown, so the streak has
	// to be at the threshold before either can be asked about.
	for range goneStrikeThreshold {
		s.strike(now)
	}

	if ok, _ := s.canClaimProbe(now); !ok {
		t.Fatal("a streak at the threshold that has never been probed must admit a claim")
	}
	if ok, _ := s.canClaimProbe(now); !ok {
		t.Fatal("asking twice was refused, so the read spent the claim it was asking about")
	}
	if !s.claimProbe(now) {
		t.Fatal("the real claim must still be available after the read")
	}

	// And now both must see the cooldown.
	if ok, reason := s.canClaimProbe(now); ok {
		t.Fatal("the read did not see the claim that was just taken")
	} else if !strings.Contains(reason, "cooldown") {
		t.Errorf("reason = %q, want the cooldown reason", reason)
	}
	if s.claimProbe(now) {
		t.Fatal("claimProbe granted a second claim inside the cooldown")
	}

	// They agree on the far side of it too.
	lapsed := now.Add(goneProbeCooldown)
	if ok, _ := s.canClaimProbe(lapsed); !ok {
		t.Fatal("the read must admit a claim once the cooldown has lapsed")
	}
	if !s.claimProbe(lapsed) {
		t.Fatal("claimProbe must grant one once the cooldown has lapsed")
	}
}

// TestNoteModelGone_ASuccessDoesNotResetTheProbeCooldown closes the other door
// into the same unbounded loop.
//
// Clearing the count on a served probe was only half the bound, because a real
// request succeeding clears it too — and that is the MIXED case, which is at
// least as likely as the pure one: a provider that refuses one request shape
// with retirement prose while serving another produces both signals from the
// same client traffic. If the success drops the whole streak, the probe cooldown
// goes with it, and three more refusals buy another upstream call immediately.
// A success clears what the model is accused of; it does not buy the gateway a
// free probe.
func TestNoteModelGone_ASuccessDoesNotResetTheProbeCooldown(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusOK, goneServedAnswer)
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	waitForProbes(t, script, 1)
	waitForStreakCount(t, h, m.ID, probeChatEndpoint, 0)

	// An ordinary request to the same model succeeds, exactly as the request
	// path reports it.
	h.noteModelServed(m, endpointTypeChat)

	// And the traffic that does not succeed carries on refusing. None of it may
	// buy an upstream request while the cooldown is running.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a model that answered must not be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("a success must not reset the probe cooldown, got %v", paths)
	}

	// Still a delay rather than a lockout.
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)
	waitForProbes(t, script, 2)
}

// TestNoteModelGone_RefusedProbeStillDisables is the paired control for the test
// above: identical strikes, identical code path, the provider's answer the only
// difference, and the opposite outcome.
//
// Together they pin that the retirement is written on what the probe found and
// not on the strike count. This is the half that keeps the verification from
// quietly disarming the feature — the probe may only ever PREVENT a disable,
// never weaken one that three refusals and a refused probe have earned.
func TestNoteModelGone_RefusedProbeStillDisables(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gpt-5.6-sol"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a probe that refused the model must still retire it, got %+v", calls)
	}
	if calls[0].id != m.ID || calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable of %s, got %+v", m.ID, calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeChatEndpoint, paths)
	}
}

// TestNoteModelGone_RateLimitedProbePostponesThenRetires pins the postponement
// and its exit in one sequence.
//
// A 429 is the provider incident case: it establishes nothing about whether the
// model exists, so retiring on it would turn one bad afternoon at a provider
// into a mass retirement of its whole catalog. But postponing must not become
// permanent either — a model that is genuinely dead has to be retirable once the
// incident passes. Deleting this test loses one or the other: either the
// gateway retires models it never verified, or the inconclusive branch parks the
// streak above the threshold where every later refusal is a no-op and the dead
// model stays enabled forever.
func TestNoteModelGone_RateLimitedProbePostponesThenRetires(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	// One server that changes its answer, not two servers: the candidate is built
	// once and carries the base URL, so the provider has to be the same host
	// before and after the incident.
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a rate-limited probe proved nothing, so nothing may be retired on it, got %+v", calls)
	}

	// The postponement KEEPS the evidence. Dropping the streak to stay reachable
	// would leave the probe rate unbounded — three fresh refusals buying another
	// probe, forever, at a provider already rate limiting us — so the claim gates
	// the retry instead and the strikes stay where they are.
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("a postponed retirement must keep the strikes it was built on, got a streak of %d", n)
	}

	// The incident passes and the model turns out to be genuinely gone. One
	// refusal is now enough, precisely because the earlier strikes survived.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a postponed retirement must still be reachable once the provider answers again, got %+v", calls)
	}
	if calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable, got %+v", calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 2 {
		t.Errorf("expected one probe per claim, got %v", paths)
	}
}

func TestNoteModelGone_ProbeCooldownBoundsTheProbeRate(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusTooManyRequests, goneRateLimitedAnswer)
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}
	// Wait for the first probe rather than assuming it: "no second probe" and
	// "no probe yet" would otherwise be indistinguishable, and the test would
	// pass against code that never probed at all.
	waitForProbes(t, script, 1)

	// A client retrying the dead model. Every one of these lands on a streak
	// that is already at the threshold, and not one of them may buy a probe.
	for range 10 * goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("nothing established anything, so nothing may be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Fatalf("30 refusals inside the cooldown must cost exactly one upstream request, got %v", paths)
	}

	// The bound is a delay, not a lockout: once the cooldown lapses the next
	// refusal probes again.
	script.answer(http.StatusNotFound, goneRefusalBody(m.ModelID))
	expireProbeCooldown(t, h, m.ID, probeChatEndpoint)
	h.noteModelGone(cand, endpointTypeChat)

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("the cooldown must expire, got %+v", calls)
	}
	if paths := waitForProbes(t, script, 2); len(paths) != 2 {
		t.Errorf("expected exactly one probe per claim, got %v", paths)
	}
}

// TestNoteModelGone_ExhaustedProbeSlotsPostponeWithoutAsking pins the other
// bound: the one across models rather than across time.
//
// A provider event nominates its whole catalog at once, and each nomination that
// reaches the threshold runs on its own detached goroutine. Without a cap they
// all open a connection to the same host simultaneously, each holding it for up
// to goneProbeTimeout, so the gateway's diagnosis of an incident becomes part of
// the incident. What the cap must NOT do is convert a missing slot into a
// retirement: no request was sent, so nothing was established, and the honest
// outcome is the same postponement every other unproven case gets.
func TestNoteModelGone_ExhaustedProbeSlotsPostponeWithoutAsking(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("claude-sonnet-4"))
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	// Every slot for this provider taken, as a burst of concurrent retirements
	// against the same host leaves them. Seeding the map is how the test gets
	// there without spawning goneProbeMaxConcurrent real probes and hoping they
	// overlap; the slots are per provider, so this constrains nothing outside
	// this test.
	full := make(chan struct{}, goneProbeMaxConcurrent)
	for range goneProbeMaxConcurrent {
		full <- struct{}{}
	}
	h.goneProbeSlots.Store(cand.provider.ID, full)

	// The provider WOULD refuse this model, so a probe that went out would
	// retire it. Nothing may be retired on a probe that never went out.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a probe that was never sent established nothing, so nothing may be retired, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("no slot was free, so no upstream request may be spent, got %v", paths)
	}
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("the strikes must survive so the retry is reachable, got a streak of %d", n)
	}
	// And the model's own claim was not spent on it. A refused slot costs
	// nothing: no upstream request was made, so there is nothing for the
	// five-minute cooldown to be bounding. Charging it anyway is what turned a
	// provider-wide nomination event into four models per cooldown — the very
	// burst the semaphore exists for, converging in hours instead of minutes.
	if next := probeClaimAt(t, h, m.ID, probeChatEndpoint); !next.IsZero() {
		t.Fatalf("a refused slot burnt the probe cooldown until %s", next)
	}

	// A slot frees up: the very next refusal probes, with no cooldown to wait
	// out, and the retirement lands on the evidence that was already there.
	<-full
	h.noteModelGone(cand, endpointTypeChat)

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a retirement postponed for want of a slot must still be reachable, got %+v", calls)
	}
	if calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable, got %+v", calls[0])
	}
	if paths := waitForProbes(t, script, 1); len(paths) != 1 {
		t.Errorf("expected exactly one probe, got %v", paths)
	}
}

// TestNoteModelGone_AnOpenCircuitCostsNoClaim pins that the cooldown is only
// spent on a probe that can actually leave the process.
//
// probeModel asks the breaker too, but it asks from inside the detached
// goroutine — by which point the five minutes are gone. A model whose third
// strike landed in the gap between its provider's last answer and its circuit
// opening would buy nothing with them and wait out the full cooldown before it
// could be adjudicated, once the provider came back.
//
// The strikes must survive either way: an open circuit says something about the
// provider and nothing about the model.
func TestNoteModelGone_AnOpenCircuitCostsNoClaim(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	h.circuitBreaker = failover.NewCircuitBreaker(nil)
	m := &model.Model{ID: uuid.New(), ModelID: "claude-sonnet-4"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("claude-sonnet-4"))
	cand := goneCandidateAt(m, "OpenCode Zen", srv.URL)

	// Driven through the breaker's own API rather than by reaching into its
	// state: this check has to agree with what the routing path sees, and the
	// threshold is the breaker's to define.
	for range h.circuitBreaker.Threshold {
		h.circuitBreaker.RecordFailure(cand.provider.ID, cand.provider.Name, "")
	}
	if state := h.circuitBreaker.GetState(cand.provider.ID, ""); state != failover.StateOpen {
		t.Fatalf("fixture: the circuit did not open, state = %v", state)
	}

	// The provider WOULD refuse this model, so a probe that went out would
	// retire it.
	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeChat)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("nothing may be retired on a probe that was never sent, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("a sidelined provider must not be asked, got %v", paths)
	}
	if next := probeClaimAt(t, h, m.ID, probeChatEndpoint); !next.IsZero() {
		t.Fatalf("the claim was spent on a probe that never left the process, cooldown until %s", next)
	}
	if n := goneStreakFor(t, h, m.ID, probeChatEndpoint).count(); n != goneStrikeThreshold {
		t.Fatalf("the strikes must survive: an open circuit is evidence about the provider, got %d", n)
	}
	// Between them, those two assertions are the whole property: the evidence is
	// intact and nothing stands between it and the next refusal, so the
	// retirement is adjudicated as soon as the provider is routable again rather
	// than a cooldown later. The recovery itself is not driven here because the
	// breaker only leaves the open state through its own cooldown, which is the
	// breaker's business and not this path's.
}

// TestNoteModelGone_AModelServingBothSurfacesIsNeverRetired pins the gate that
// is about the ACTION rather than the evidence.
//
// Strikes, probes and successes are per surface, because a provider can retire
// one surface of a model and keep serving the other. The disable is not: it
// turns the model ROW off. For a model the catalog says serves chat AND
// embeddings those two scopes disagree, and no probe can catch it — the probe is
// right, the model really is gone on that surface, and the disable is simply
// broader than the finding. So nothing is written at all, and the model stays
// enabled until discovery drops it or an operator does.
//
// The refusals here would otherwise retire it: the fixture refuses by name, and
// the same sequence against a chat-only model disables it (see
// TestNoteModelGone_DisablesAfterThreshold).
func TestNoteModelGone_AModelServingBothSurfacesIsNeverRetired(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol", InputModalities: `["text"]`, OutputModalities: `["text","embedding"]`}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gpt-5.6-sol"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	// Well past the threshold on both surfaces: the gate is unconditional, not
	// a delay.
	for range goneStrikeThreshold + 3 {
		h.noteModelGone(cand, endpointTypeChat)
		h.noteModelGone(cand, endpointTypeEmbeddings)
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("a disable cannot separate the surfaces, so it must not be written, got %+v", calls)
	}
	if paths := script.requestedPaths(); len(paths) != 0 {
		t.Fatalf("a refusal that can never be acted on must not spend an upstream request, got %v", paths)
	}
	for _, endpoint := range []string{probeChatEndpoint, probeEmbeddingsEndpoint} {
		if _, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint}); ok {
			t.Errorf("a streak that can never fire is not evidence, and one was recorded on %s", endpoint)
		}
	}
}

// TestNoteModelGone_EmbeddingsAreProbedOnTheirOwnEndpoint pins that the probe
// asks the question on the surface the model actually serves.
//
// An embeddings model has no /chat/completions, so a chat probe against one
// fails for a reason that has nothing to do with retirement — and that failure
// reads as confirmation of the retirement it was supposed to adjudicate. The
// verification would then manufacture exactly the wrong answer, and every
// embeddings model that drew three classifier strikes would be retired
// unverified.
func TestNoteModelGone_EmbeddingsAreProbedOnTheirOwnEndpoint(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	// An embeddings model as the catalog describes one: a refusal on
	// /embeddings only counts against a model that says it produces embeddings.
	m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small", OutputModalities: `["embedding"]`}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("text-embedding-3-small"))
	cand := goneCandidateAt(m, "OpenAI", srv.URL)

	for range goneStrikeThreshold {
		h.noteModelGone(cand, endpointTypeEmbeddings)
	}

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("an embeddings model refused by its own endpoint must still be retired, got %+v", calls)
	}

	paths := script.requestedPaths()
	if len(paths) != 1 || paths[0] != probeEmbeddingsEndpoint {
		t.Fatalf("expected exactly one probe on %s, got %v", probeEmbeddingsEndpoint, paths)
	}
	for _, p := range paths {
		if p == probeChatEndpoint {
			t.Errorf("an embeddings model was probed on the chat endpoint (%s), which fails for reasons unrelated to retirement", p)
		}
	}
}

// TestNoteModelGone_UnprobeableFamiliesAreNeverRetired pins the family gate, and
// pins it before the strike rather than after.
//
// Image, speech and transcription models cannot be probed cheaply or safely: the
// call costs real money and real seconds, and a chat probe against one fails for
// reasons unrelated to retirement. Where the answer cannot be substantiated the
// correct outcome is to never auto-retire the family at all — falling back to
// the classifier alone would leave the guessing running unsupervised precisely
// where it is least observed.
//
// The streak assertion is the other half. A counter that can never fire is not
// evidence, and recording one would both manufacture the appearance of some and
// let image refusals top up a chat streak that CAN retire the model.
func TestNoteModelGone_UnprobeableFamiliesAreNeverRetired(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)

	families := []struct {
		name         string
		endpointType string
		modelID      string
	}{
		{"image", endpointTypeImage, "dall-e-3"},
		{"speech", endpointTypeTTS, "tts-1-hd"},
		// An entry whose family was never stamped is unprobeable for the same
		// reason as the two above: nothing can be established about it.
		{"unstamped", "", "gpt-5.6-sol"},
	}

	scripts := make([]*goneScriptedServer, len(families))
	for i, f := range families {
		m := &model.Model{ID: uuid.New(), ModelID: f.modelID}
		srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody(f.modelID))
		scripts[i] = script
		cand := goneCandidateAt(m, "OpenAI", srv.URL)

		// Well past the threshold: the gate is unconditional, not a delay.
		for range goneStrikeThreshold + 3 {
			h.noteModelGone(cand, f.endpointType)
		}
		// Every surface a streak can be keyed on, so this cannot pass by
		// looking under a key nothing would have written.
		for _, endpoint := range []string{probeChatEndpoint, probeEmbeddingsEndpoint} {
			if _, ok := h.goneStrikes.Load(goneStreakKey{model: m.ID, endpoint: endpoint}); ok {
				t.Errorf("%s: a family that can never be adjudicated must not record a streak (%s)", f.name, endpoint)
			}
		}
	}

	if calls := waitForDisable(t, repo); len(calls) != 0 {
		t.Fatalf("an unprobeable family must never be auto-retired, got %+v", calls)
	}
	for i, script := range scripts {
		if paths := script.requestedPaths(); len(paths) != 0 {
			t.Errorf("%s: an unprobeable family must not spend an upstream request, got %v", families[i].name, paths)
		}
	}
}

// TestNoteStreamOutcome_GoneVerdictReachesTheProbe covers the third call site.
//
// A model can be reported retired mid-stream, and that report arrives through
// noteStreamOutcome rather than through the non-streaming error path. If the
// gone verdict did not forward to the retirement machinery, a model that only
// ever fails on streaming requests would stay routable forever, which is most
// of the gateway's traffic.
//
// It does not claim to catch a forwarded family that differs from the one on
// the log entry, and deliberately does not try. noteStreamOutcome is reachable
// only from dispatchStreaming and serveHedgeWinner, both chat/messages
// surfaces, so a row driving any other family would pin an input the production
// code cannot produce.
//
// The probe assertion is what makes this about the verdict reaching the PROBE:
// a disable that landed without an upstream request would be the unverified
// retirement this whole change removes.
func TestNoteStreamOutcome_GoneVerdictReachesTheProbe(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "gemini-2.0-flash"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("gemini-2.0-flash"))
	cand := goneCandidateAt(m, "Google AI Studio (Gemini)", srv.URL)

	for range goneStrikeThreshold {
		// upstreamKind is what the provider said, which is the only thing that can
		// establish a retirement; the endpoint family is stamped at ingest exactly
		// as newPendingRequestLog leaves it.
		h.noteStreamOutcome(&requestLogData{endpointType: endpointTypeChat, upstreamKind: KindProviderModelGone}, cand)
	}

	calls := waitForDisable(t, repo)
	if len(calls) != 1 {
		t.Fatalf("a model reported retired mid-stream must be retired like any other, got %+v", calls)
	}
	if calls[0].id != m.ID || calls[0].enabled || !calls[0].committed {
		t.Errorf("expected a committed disable of %s, got %+v", m.ID, calls[0])
	}
	if paths := script.requestedPaths(); len(paths) != 1 || paths[0] != probeChatEndpoint {
		t.Fatalf("the stream verdict must be adjudicated by a probe on %s, got %v", probeChatEndpoint, paths)
	}
}

// TestNoteModelGone_BurstIssuesOneProbeAndLeaksNoSlots pins the two things a
// concurrent burst against a dead model must not do.
//
// A dead model is exactly the one that gets hammered: clients retry it and a
// failover group can try it on several requests at once. Every one of those
// refusals arrives at the threshold and asks for a probe, so the claim decides
// which single caller may spend an upstream request.
//
// The second assertion is the one with teeth. The provider's slot is taken
// BEFORE the model's claim, so a caller that loses the claim is holding a slot
// it must give back on a path that returns without ever probing. A leak there
// is permanent and silent: after goneProbeMaxConcurrent losers the provider has
// no slots left, every later nomination reports "too many retirement probes are
// already in flight", and no model on that provider is ever adjudicated again
// for the life of the process. Nothing else in the suite would notice, because
// every other test issues one probe at a time.
//
// Its sensitivity is deliberately stated rather than assumed, because the loser
// path is a race and cannot be entered on demand: the window is between
// canClaimProbe and claimProbe, and the seeding plus the start barrier below are
// what widen it enough to be entered at all. Measured by deleting the release():
// caught 5 runs in 6 at this burst size, 4 in 5 at a quarter of it, and 0 in 5
// before the streak was pre-seeded (the burst spent itself building the count
// and never reached the claim).
//
// The error is one-sided, which is what makes that acceptable. Correct code
// always passes — the slots always come back — so this can miss a regression but
// cannot invent one. The first two assertions are exact and hold every run.
func TestNoteModelGone_BurstIssuesOneProbeAndLeaksNoSlots(t *testing.T) {
	t.Parallel()

	repo := &mockModelRepo{}
	h := newGoneHandler(t, repo)
	m := &model.Model{ID: uuid.New(), ModelID: "burst-model-1"}
	srv, script := newGoneScriptedServer(t, http.StatusNotFound, goneRefusalBody("burst-model-1"))
	candidate := goneCandidateAt(m, "Burst Provider", srv.URL)

	// Three strikes first, so every goroutine below arrives at a streak that is
	// already at the threshold and reaches the claim. Without this the burst
	// spends most of itself building the count, and the contended window this
	// test exists for is never entered.
	raw, _ := h.goneStrikes.LoadOrStore(goneStreakKey{model: m.ID, endpoint: probeChatEndpoint}, &goneStreak{})
	seeded, ok := raw.(*goneStreak)
	if !ok {
		t.Fatal("the streak map holds something that is not a streak")
	}
	for range goneStrikeThreshold {
		seeded.strike(time.Now())
	}

	// Released together, so the callers hit canClaimProbe at the same moment
	// rather than in the staggered order goroutine startup gives. That is what
	// puts more than one of them past the cooldown read and into the claim,
	// which is the only path that takes a provider slot and can then lose.
	const burst = 256
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(burst)
	for range burst {
		go func() {
			defer wg.Done()
			<-start
			h.noteModelGone(candidate, endpointTypeChat)
		}()
	}
	close(start)
	wg.Wait()

	if calls := waitForDisable(t, repo); len(calls) != 1 {
		t.Fatalf("recorded %d disables, want exactly 1", len(calls))
	}
	if paths := script.requestedPaths(); len(paths) != 1 {
		t.Errorf("provider was probed %d times, want exactly 1: %v", len(paths), paths)
	}

	// Every slot must be back. Taking all of them proves it; the acquires are
	// non-blocking, so a leaked slot fails here rather than hanging.
	var releases []func()
	for i := range goneProbeMaxConcurrent {
		release, ok := h.acquireProbeSlot(candidate.provider.ID)
		if !ok {
			t.Fatalf("slot %d of %d could not be acquired after the burst: a loser kept its slot", i+1, goneProbeMaxConcurrent)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}
