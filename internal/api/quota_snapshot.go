package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// snapshotFresherThan and snapshotWithinAge exist so a future-dated snapshot can
// never read as fresh.
//
// Both questions used to be asked with a bare time.Since/Sub comparison, which
// returns a NEGATIVE duration when the stamp is in the future. Negative
// satisfies every "< interval" and fails every "> maxAge", so a single
// future-dated row silently made a provider permanently fresh: its upstream poll
// was skipped forever, and its stale quota kept counting as evidence. The
// repository now clamps on write, so these guards are defence in depth for rows
// that predate the clamp or arrive by another path (a direct database write, a
// restored backup).
//
// Same class of bug, and the same repair, as the fleet rate-limit divisor's
// staleness check: treat a negative age as untrustworthy rather than as fresh.
func snapshotFresherThan(now, fetchedAt time.Time, within time.Duration) bool {
	age := now.Sub(fetchedAt)
	return age >= 0 && age < within
}

// snapshotWithinAge reports whether a snapshot is recent enough to be trusted as
// evidence. A future stamp is not.
func snapshotWithinAge(now, fetchedAt time.Time, maxAge time.Duration) bool {
	age := now.Sub(fetchedAt)
	return age >= 0 && age <= maxAge
}

// quotaKindFor maps a provider type to the snapshot kind it produces, or
// ok=false when the type exposes no quota/usage/balance/account endpoint.
func quotaKindFor(providerType string) (string, bool) {
	switch providerType {
	case "nanogpt", "zai-coding", "kimi-code", "minimax", "openrouter", "neuralwatt":
		return "usage", true
	case "deepseek":
		return "balance", true
	case "ollama-cloud":
		return "account", true
	default:
		return "", false
	}
}

// fetchQuotaSnapshot performs the live upstream call for a provider and returns
// the JSON body, the HTTP status the endpoint would send, and an error only for
// unexpected failures. A dead credential becomes 424; NeuralWatt free-tier
// (nil result) becomes 204 with a null payload. This is the single source of
// truth shared by the poller, manual refresh, and cold lazy-fill.
//
// Each discovery result is captured into its concrete typed variable before
// marshalling. Assigning a typed pointer into an `any` would wrap a nil pointer
// in a non-nil interface, so NeuralWatt's `nil` free-tier result must be
// detected on the typed value, not via an interface `== nil` check.
func fetchQuotaSnapshot(ctx context.Context, disc *provider.DiscoveryService, prov *provider.Provider, masterKey string) (string, json.RawMessage, int, error) {
	providerType := provider.TypeOf(prov)
	kind, ok := quotaKindFor(providerType)
	if !ok {
		return "", nil, 0, errors.New("provider type does not expose quota")
	}

	marshal := func(v any) (json.RawMessage, int, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, 0, err
		}
		return b, http.StatusOK, nil
	}

	var (
		payload json.RawMessage
		status  int
		err     error
	)
	switch providerType {
	case "nanogpt":
		var res *provider.NanoGPTUsageResponse
		if res, err = disc.GetNanoGPTUsage(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "zai-coding":
		var res *provider.ZAICodingQuotaResponse
		if res, err = disc.GetZAICodingQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "kimi-code":
		var res *provider.KimiCodeQuotaResponse
		if res, err = disc.GetKimiCodeQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "minimax":
		var res *provider.MiniMaxQuotaResponse
		if res, err = disc.GetMiniMaxQuota(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "openrouter":
		var res *provider.OpenRouterBalance
		if res, err = disc.GetOpenRouterBalance(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "neuralwatt":
		var res *provider.NeuralWattQuotaResponse
		if res, err = disc.GetNeuralWattQuota(ctx, prov, masterKey); err == nil {
			if res == nil {
				return kind, json.RawMessage("null"), http.StatusNoContent, nil
			}
			payload, status, err = marshal(res)
		}
	case "deepseek":
		var res *provider.DeepSeekBalanceResponse
		if res, err = disc.GetDeepSeekBalance(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	case "ollama-cloud":
		var res *provider.OllamaCloudAccount
		if res, err = disc.GetOllamaCloudAccount(ctx, prov, masterKey); err == nil {
			payload, status, err = marshal(res)
		}
	}
	if err != nil {
		if errors.Is(err, provider.ErrProviderKeyInvalid) {
			return kind, nil, http.StatusFailedDependency, nil
		}
		return kind, nil, 0, err
	}
	return kind, payload, status, nil
}

// PollQuotasOnce refreshes the snapshot for every enabled quota-capable
// provider. Called by the background quota loop. Each provider fetch is bounded
// by its own timeout so one slow upstream cannot stall the pass, and failures
// are recorded (via RecordFailure) without discarding the last good snapshot.
func (h *Handler) PollQuotasOnce(ctx context.Context) {
	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		debuglog.Error("quota: list providers failed", "error", err)
		// We have no fresh provider list to rebuild advice from, and the last
		// computed map could be arbitrarily stale by the time providers are
		// listable again. Fail closed: clear it rather than let a frozen
		// deadline keep pinning a circuit's cooldown.
		h.ClearQuotaAdvice(ctx)
		return
	}
	disc := h.discoveryService()
	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}
		kind, ok := quotaKindFor(provider.TypeOf(prov))
		if !ok {
			continue
		}

		// Fleet dedup: if Front Desk recently distributed a snapshot for this
		// provider, skip the upstream call. The primary (and any node FD is not
		// feeding) has no recent fleet snapshot and still self-polls, so quota is
		// never worse than standalone.
		interval := time.Duration(h.settingsRepo.GetInt(ctx, "quota_refresh_interval_min", 5)) * time.Minute
		if interval > 0 {
			if snap, _ := h.quotaRepo.Get(ctx, prov.ID, kind); snap != nil && snap.Source == "fleet" && snapshotFresherThan(time.Now(), snap.FetchedAt, interval) {
				continue
			}
		}

		h.pollQuotaForProvider(ctx, disc, prov, kind)
	}

	h.RefreshQuotaAdvice(ctx)
}

// pollQuotaForProvider fetches and stores one provider's quota snapshot. The
// fetch is bounded by its own timeout so a single slow upstream cannot stall
// the caller, and a failure is recorded (via RecordFailure) without discarding
// the last good snapshot. kind comes from the caller, which has already
// established that this provider type exposes quota.
func (h *Handler) pollQuotaForProvider(ctx context.Context, disc *provider.DiscoveryService, prov *provider.Provider, kind string) {
	provCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, payload, status, ferr := fetchQuotaSnapshot(provCtx, disc, prov, h.cfg.MasterKey)
	if ferr != nil {
		debuglog.Warn("quota: poll fetch failed", "provider", prov.Name, "error", ferr)
		// Its own context: a fetch that hung to its deadline is exactly the case
		// worth recording, and provCtx is already expired by then, so writing the
		// failure through it would drop the record for every timeout.
		recCtx, recCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer recCancel()
		if rerr := h.quotaRepo.RecordFailure(recCtx, prov.ID, kind, ferr.Error()); rerr != nil {
			debuglog.Warn("quota: record failure failed", "provider", prov.Name, "error", rerr)
		}
		return
	}
	if uerr := h.quotaRepo.Upsert(provCtx, quota.Snapshot{
		ProviderID: prov.ID,
		Kind:       kind,
		Payload:    payload,
		HTTPStatus: status,
		Source:     "poll",
	}); uerr != nil {
		debuglog.Warn("quota: poll upsert failed", "provider", prov.Name, "error", uerr)
	}
}

// quotaNudgeDebounce is the minimum spacing between breaker-triggered polls of
// one provider. A flapping circuit opens over and over, and every open would
// otherwise become another upstream quota call against a provider that is
// already refusing traffic.
const quotaNudgeDebounce = 60 * time.Second

// quotaNudgeTimeout is the budget each stage of a nudge gets: the provider
// lookup, the poll, and the advice refresh are bounded separately rather than
// sharing one allowance, so a slow stage cannot starve the one after it. The
// background pass needs no equivalent, since its refresh runs on the long-lived
// loop context and the per-provider fetches are children that cannot exhaust it.
const quotaNudgeTimeout = 30 * time.Second

// NudgeQuotaPoll refreshes one provider's quota snapshot out of band and
// rebuilds the advice from it. The background pass polls every few minutes, so
// a circuit that opens because the provider's window is spent can be most of a
// cycle away from the reading that would pin its cooldown to the real reset
// time, and every probe it lets through until then is a guaranteed 429.
//
// The refresh this ends with retargets circuits that are already open, so the
// reading reaches the very circuit whose opening asked for it rather than only
// governing the next one to open.
//
// The upstream call runs on its own goroutine under a fresh context, so it
// never adds latency to whatever opened the circuit and never inherits that
// caller's cancellation. Providers that serve no traffic or expose no quota
// endpoint are rejected before any of that.
func (h *Handler) NudgeQuotaPoll(providerID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), quotaNudgeTimeout)
	defer cancel()

	prov, err := h.providerRepo.Get(ctx, providerID)
	if err != nil || prov == nil || !prov.Enabled {
		return
	}
	kind, ok := quotaKindFor(provider.TypeOf(prov))
	if !ok {
		return
	}
	if !h.allowQuotaNudge(providerID, time.Now()) {
		return
	}

	disc := h.discoveryService()
	go func() {
		pollCtx, pollCancel := context.WithTimeout(context.Background(), quotaNudgeTimeout)
		defer pollCancel()
		h.runQuotaNudge(pollCtx, disc, prov, kind)
	}()
}

// runQuotaNudge performs one nudge: the out-of-band poll under pollCtx, then
// the advice refresh under a budget of its own.
//
// The two must not share one budget. A fetch that hangs to its deadline leaves
// the refresh nothing to read with, and a refresh that cannot read fails closed
// by clearing every provider's advice rather than only this one's. That turns a
// single slow quota endpoint into a fleet-wide loss of pins until the next
// background pass succeeds, and it would fire under precisely the conditions
// this exists for: a provider that just failed enough requests to open a circuit
// is a provider whose quota endpoint is plausibly hanging too.
func (h *Handler) runQuotaNudge(pollCtx context.Context, disc *provider.DiscoveryService, prov *provider.Provider, kind string) {
	h.pollQuotaForProvider(pollCtx, disc, prov, kind)

	refreshCtx, cancel := context.WithTimeout(context.Background(), quotaNudgeTimeout)
	defer cancel()
	h.RefreshQuotaAdvice(refreshCtx)
}

// allowQuotaNudge reports whether a nudge for this provider is due, stamping it
// when it is. Deliberately in-memory: a restart re-arms every provider, which
// costs one extra poll at worst.
func (h *Handler) allowQuotaNudge(providerID uuid.UUID, now time.Time) bool {
	h.quotaNudgeMu.Lock()
	defer h.quotaNudgeMu.Unlock()

	if last, ok := h.quotaNudgeLast[providerID]; ok && now.Sub(last) < quotaNudgeDebounce {
		return false
	}
	if h.quotaNudgeLast == nil {
		h.quotaNudgeLast = make(map[uuid.UUID]time.Time)
	}
	h.quotaNudgeLast[providerID] = now
	return true
}

// RefreshQuotaAdvice rebuilds the in-memory quota advice from stored snapshots.
// It reads the table rather than the poll's own fetches so fleet-distributed
// snapshots (which PollQuotasOnce skips) are included.
//
// A snapshot older than three refresh intervals is ignored. If quota polling
// is disabled (quota_refresh_interval_min <= 0) the resolved maxAge is <= 0,
// and buildQuotaAdvice treats that as "advise nothing": we cannot trust the
// age of any stored snapshot without a live poll cadence to bound it, so
// never pin the breaker on data that could predate a plan change or a manual
// top-up.
func (h *Handler) RefreshQuotaAdvice(ctx context.Context) {
	if h.quotaAdvisor == nil {
		debuglog.Warn("quota: advice refresh skipped, no advisor wired")
		return
	}
	snaps, err := h.quotaRepo.List(ctx)
	if err != nil {
		debuglog.Warn("quota: advice refresh failed to list snapshots", "error", err)
		// Same rule as PollQuotasOnce's provider-list failure: a frozen advice
		// map could be arbitrarily stale by the time listing works again, so
		// fail closed rather than keep pinning on it.
		h.ClearQuotaAdvice(ctx)
		return
	}
	interval := time.Duration(h.settingsRepo.GetInt(ctx, "quota_refresh_interval_min", 5)) * time.Minute
	maxAge := 3 * interval

	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		debuglog.Warn("quota: advice refresh failed to list providers", "error", err)
		// Same fail-closed rule as above and as PollQuotasOnce.
		h.ClearQuotaAdvice(ctx)
		return
	}
	typeByID := make(map[uuid.UUID]string, len(providers))
	nameByID := make(map[uuid.UUID]string, len(providers))
	for _, p := range providers {
		typeByID[p.ID] = provider.TypeOf(p)
		nameByID[p.ID] = p.Name
	}

	// recovered is computed in the same pass as the advice, over the same
	// snapshots, so it costs no extra query. It is a separate map that is never
	// handed to the advisor, so Replace taking ownership of the advice below
	// cannot touch it.
	advice, recovered := buildQuotaAdvice(snaps, typeByID, maxAge, time.Now())
	advised := len(advice)

	// Retarget the circuits that are already open before handing the map over:
	// Replace takes ownership, so this is the last point at which advice may be
	// read. The breaker only pins at the instant a circuit opens, so without
	// this a reading that arrives even a second later reaches nothing until the
	// circuit fails a probe and opens again — which is most of what the poll a
	// breaker open triggers exists to prevent.
	if h.circuitBreaker != nil {
		h.circuitBreaker.ApplyQuotaPins(advice)
	}

	h.quotaAdvisor.Replace(advice)

	// This refresh succeeded, so every provider it assessed fresh and found no
	// longer exhausted has affirmatively recovered (a topped-up plan, a window
	// that reset early), and serving out the rest of a pin that can run to 24h
	// would bench a healthy provider for hours. Only reachable from here, never
	// from the failure paths above: a refresh that could not read says nothing
	// about provider health, and those paths only stop *new* pins rather than
	// disturbing pins already in force.
	//
	// Recovery must be affirmative, which is why this passes the recovered set
	// rather than inverting the exhausted one: a provider is equally missing
	// from the advice when its snapshot is stale, when its payload could not be
	// assessed, and when it has no snapshot at all — the states where quota
	// fetching is broken and the window is most likely still spent.
	//
	// This shortens a cooldown; it never closes a circuit. HTTP still decides
	// recovery through the ordinary half-open probe.
	if h.circuitBreaker != nil {
		h.circuitBreaker.ReleaseQuotaPins(recovered)
	}
	// Info once anything is actually advised: a quota pin can hold a circuit open
	// for a day, and Debug is off in normal production, so this is the only log
	// trail an operator has for why a provider went dark. The no-advice case
	// stays at Debug — a line on every poll pass would be noise. Counts only, no
	// payload values.
	if advised > 0 {
		debuglog.Info("quota: advice refreshed", "advised_providers", advised)
	} else {
		debuglog.Debug("quota: advice refreshed", "advised_providers", advised)
	}

	// Alert-only, and deliberately last: the schema-drift watch reuses the
	// snapshot list and provider maps above rather than re-querying, and
	// nothing it does can influence the advice just published.
	h.checkQuotaDrift(ctx, snaps, typeByID, nameByID, maxAge)
}

// ClearQuotaAdvice drops all quota advice immediately. Used whenever the
// in-memory map cannot be trusted to reflect current reality: quota polling
// has gone from enabled to disabled (the background loop stops calling
// RefreshQuotaAdvice entirely, so the last computed map would otherwise be
// retained for the process lifetime), or a poll pass could not even list
// providers. Safe to call when no advisor was ever wired (no-op).
func (h *Handler) ClearQuotaAdvice(_ context.Context) {
	if h.quotaAdvisor == nil {
		return
	}
	h.quotaAdvisor.Replace(nil)
}

// DisableQuotaAdvice is the path taken when quota polling itself is switched
// off: it drops all advice *and* releases every quota pin already in force.
//
// The two must happen together. Clearing the advice alone stops new pins, but
// the pins already stamped on would be served out to the 24h ceiling with no
// refresh left to ever report a recovery — a provider benched for a day on
// evidence the operator deliberately stopped collecting, from an operator
// action whose documented meaning is "turn this feature off on this node".
// Absence of evidence keeps a pin only while the gateway is still looking.
//
// Deliberately not folded into ClearQuotaAdvice, which the failed-refresh paths
// also call: a database blip is exactly the case where the gateway is still
// looking and the pins must stand.
func (h *Handler) DisableQuotaAdvice(ctx context.Context) {
	h.ClearQuotaAdvice(ctx)
	if h.circuitBreaker == nil {
		return
	}
	// Counts only, no payload values. A pin can hold a provider dark for a day,
	// so an operator needs the line that says switching polling off ended one.
	if released := h.circuitBreaker.ReleaseAllQuotaPins(); released > 0 {
		debuglog.Info("quota: polling disabled, released pins in force", "released_pins", released)
	}
}

// buildQuotaAdvice is the pure filtering step of RefreshQuotaAdvice, split out
// so the staleness rule and the assessment filter are testable without a
// database. It returns both halves of the same judgement:
//
//   - advice: providers whose window is spent, mapped to their reset deadline.
//     This pins the cooldown of a circuit that opens later.
//   - recovered: providers a fresh snapshot was successfully assessed for and
//     found *not* exhausted. This is the only thing that lifts a pin already in
//     force, so it must be affirmative. A stale snapshot, a payload that could
//     not be assessed, a provider with no snapshot at all, and a snapshot whose
//     latest refresh attempt failed (LastError set — RecordFailure preserves the
//     last good payload and fetched_at, so a failed row can still look fresh and
//     healthy) are all simply absent from both sets: they are unknowns, not
//     recoveries, and the pin they would otherwise release is most likely still
//     deserved.
func buildQuotaAdvice(
	snaps []quota.Snapshot,
	typeByID map[uuid.UUID]string,
	maxAge time.Duration,
	now time.Time,
) (advice map[uuid.UUID]time.Time, recovered map[uuid.UUID]struct{}) {
	advice = make(map[uuid.UUID]time.Time)
	recovered = make(map[uuid.UUID]struct{})
	if maxAge <= 0 {
		// Quota polling is disabled (or the resolved interval is otherwise
		// non-positive): there is no cadence to bound snapshot age against, so
		// advise nothing rather than risk pinning the breaker on data that
		// could predate a plan change or a manual top-up. By the same token it
		// reports no recoveries: an unbounded snapshot is no more trustworthy as
		// evidence of health than as evidence of exhaustion.
		return advice, recovered
	}
	for _, s := range snaps {
		if !snapshotWithinAge(now, s.FetchedAt, maxAge) {
			continue
		}
		a := quota.Assess(typeByID[s.ProviderID], s)
		if !a.OK {
			continue
		}
		if a.Exhausted {
			advice[s.ProviderID] = a.ResetsAt
			continue
		}
		// A row whose latest refresh attempt failed still carries the last good
		// payload and fetched_at (RecordFailure deliberately preserves both), so
		// it can look fresh and healthy while the most recent attempt to verify
		// it did not succeed. This guard applies to the recovered path only,
		// not to advice above, and that asymmetry is deliberate: releasing a
		// pin requires affirmative proof that the provider is healthy, so a
		// reading whose latest refresh failed does not qualify. Holding a pin
		// does not require the same proof: the last known good reading, still
		// inside the staleness bound, is a reasonable basis for continuing to
		// hold, and the bound already handles a prolonged outage. Wrongly
		// holding costs a delayed probe; wrongly releasing puts a spent
		// provider back in rotation.
		if s.LastError != "" {
			continue
		}
		recovered[s.ProviderID] = struct{}{}
	}
	// A provider with rows of more than one kind could land in both sets (a
	// base URL edited from one quota-capable type to another leaves the old
	// row behind). Exhaustion wins: it is the reading that keeps the pin, and
	// keeping a pin one pass too long costs a delayed probe while dropping one
	// wrongly puts a spent provider back in rotation.
	for id := range advice {
		delete(recovered, id)
	}
	return advice, recovered
}
