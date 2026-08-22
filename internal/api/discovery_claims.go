package api

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClaimWindow bounds three things that MUST agree:
//
//  1. how far back flap counts are computed,
//  2. how long journal rows are retained before pruning,
//  3. how long a quiet gone model waits before it stops counting.
//
// They are one constant by necessity, not convenience: pruning discards flap
// history past the window, so an auto-dismiss horizon longer than the pruning
// horizon would read a flap count that has already been deleted and conclude
// "never flapped" from missing data.
const ClaimWindow = 30 * 24 * time.Hour

// ClaimState is what discovery currently believes about one model.
type ClaimState string

const (
	// ClaimStateGone means discovery disabled it and it is still missing. Counted.
	ClaimStateGone ClaimState = "gone"
	// ClaimStateStale means gone for longer than ClaimWindow with no flapping,
	// so it is almost certainly retired rather than broken. Shown, not counted.
	ClaimStateStale ClaimState = "stale"
	// ClaimStateSuspect means still enabled but mid-streak, one bad scan from
	// being disabled. Early warning only, never counted.
	ClaimStateSuspect ClaimState = "suspect"
	// ClaimStateRetired means the PROXY disabled it from live traffic: the
	// provider kept listing the model and refused every request for it
	// (models.auto_retired_at, migration 063). Counted, like gone.
	//
	// It is a separate state because the operator's next step is different and
	// the other states' wording is actively wrong here. A gone model is missing
	// from the provider's listing, so "last seen" dates it and a retest is the
	// obvious move. A retired model is still listed and was seen moments ago, so
	// a retest finds it present and proves nothing — what happened is that
	// requests for it failed.
	ClaimStateRetired ClaimState = "retired"
	// ClaimStatePinned means the operator enabled the model by hand while the
	// provider's listing still omits it. The pin blocks listing-based
	// auto-disable, so the row is informational: shown so a forgotten pin stays
	// visible, never counted, because the operator has already adjudicated it.
	ClaimStatePinned ClaimState = "pinned"
)

// ModelClaim is one model's current standing.
type ModelClaim struct {
	ModelID string     `json:"model_id"`
	State   ClaimState `json:"state"`
	// LastSeenAt is when the provider last listed the model, which for a gone
	// model is when it went missing.
	LastSeenAt   time.Time `json:"last_seen_at"`
	MissingScans int       `json:"missing_scans"`
	// FlapWindow counts membership transitions over ClaimWindow;
	// FlapSinceReview counts them since the operator last opened the modal.
	FlapWindow      int `json:"flap_window"`
	FlapSinceReview int `json:"flap_since_review"`
	// RetiredAt is when the proxy retired it from traffic, set only on a retired
	// claim. LastSeenAt cannot serve here: the provider still lists the model, so
	// it keeps being refreshed and would read as "last seen a minute ago" beside
	// a row saying the model is unavailable.
	RetiredAt *time.Time `json:"retired_at,omitempty"`
	// PinnedAt is when the operator enabled the model by hand, set only on a
	// pinned claim. It dates the decision the pin records, which LastSeenAt
	// cannot: that is when the provider last listed the model, i.e. the fact the
	// operator overrode.
	PinnedAt *time.Time `json:"pinned_at,omitempty"`
}

// ProviderClaims groups one provider's claims by state.
type ProviderClaims struct {
	ProviderID   string       `json:"provider_id"`
	ProviderName string       `json:"provider_name"`
	Gone         []ModelClaim `json:"gone"`
	Stale        []ModelClaim `json:"stale"`
	Suspect      []ModelClaim `json:"suspect"`
	Retired      []ModelClaim `json:"retired"`
	Pinned       []ModelClaim `json:"pinned"`
}

// GroupClaim is one failover group that discovery disabled, i.e. one model name
// whose `hotel/` routing is dead until someone fixes it. It is the group-level
// peer of ModelClaim and, like it, is derived from live state on every request:
// the row disappears from the response the moment the group is re-enabled.
//
// Deleted groups are deliberately NOT represented here. They are not derivable
// (the row is gone), and both deletion reasons — "no enabled providers found"
// and "only 1 enabled provider" — are downstream of gone-model claims that are
// already counted, so claiming them would double-count the root cause and put
// the journal back in the position of sole evidence. They stay informational.
type GroupClaim struct {
	DisplayModel string `json:"display_model"`
	// MemberCount and RoutableCount together are what make the row actionable:
	// "1 of 3 members routable" points the operator at a specific broken member,
	// where a bare "group disabled" would not. Both are counted live, not read
	// back from the journal entry that recorded the disable.
	MemberCount   int `json:"member_count"`
	RoutableCount int `json:"routable_count"`
	// DisabledAt is when discovery disabled it (model_failover_groups
	// .auto_disabled_at), so the modal can age the row like ModelClaim's
	// LastSeenAt.
	DisabledAt time.Time `json:"disabled_at"`
}

// listGroupClaims returns every failover group discovery disabled, with its live
// member and routable-member counts.
//
// The `auto_disabled_at IS NOT NULL` half of the predicate is the whole point:
// group_enabled = false alone is also what the operator writes when switching a
// group off by hand, and counting those would nag them about their own
// configuration forever (migration 062 carries the full reasoning).
//
// Members are matched on m.id::text rather than casting the JSON element to
// uuid: a single malformed entry would make the cast error out and take the
// entire discovery-status endpoint down with it, where a text compare simply
// fails to match that one member.
func listGroupClaims(ctx context.Context, pool *pgxpool.Pool) ([]GroupClaim, error) {
	rows, err := pool.Query(ctx, `
		SELECT g.display_model,
		       jsonb_array_length(COALESCE(g.priority_order, '[]'::jsonb)),
		       (SELECT count(*)
		          FROM jsonb_array_elements_text(COALESCE(g.priority_order, '[]'::jsonb)) AS e(member_id)
		          JOIN models m ON m.id::text = e.member_id
		          JOIN providers p ON p.id = m.provider_id
		         WHERE m.enabled AND p.enabled),
		       g.auto_disabled_at
		  FROM model_failover_groups g
		 WHERE g.group_enabled = false
		   AND g.auto_disabled_at IS NOT NULL
		 ORDER BY g.display_model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Never nil: the JSON response promises group_claims: GroupClaim[] with no
	// null guard on the client, matching how ProviderClaims' buckets are built.
	out := []GroupClaim{}
	for rows.Next() {
		var c GroupClaim
		if err := rows.Scan(&c.DisplayModel, &c.MemberCount, &c.RoutableCount, &c.DisabledAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// claimRow is one candidate model straight from the derivation query.
type claimRow struct {
	ProviderID   string
	ProviderName string
	ModelID      string
	LastSeenAt   time.Time
	Enabled      bool
	MissingScans int
	// RetiredAt is set when the proxy retired the model from traffic rather than
	// discovery disabling it for vanishing. Nil for every other row.
	RetiredAt *time.Time
	// PinnedAt is set when the operator enabled the model by hand
	// (models.manually_enabled_at, migration 070), which exempts it from
	// listing-based auto-disable. Nil for every other row.
	PinnedAt *time.Time
}

// flapKey identifies one model under one provider for flap counting.
type flapKey struct {
	providerID string
	modelID    string
}

// listClaimRows returns every model that is either discovery-disabled and not
// dismissed, or still enabled but mid-miss-streak. Dismissed rows are excluded
// outright, and so are manually disabled models and models on a disabled
// provider: neither is discovery's opinion.
//
// A pinned model arrives through the mid-miss-streak branch and needs no clause
// of its own: the pin stops the streak from disabling the row, so it stays
// enabled with missing_scans climbing, which is exactly that branch.
func listClaimRows(ctx context.Context, pool *pgxpool.Pool) ([]claimRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT m.provider_id::text, p.name, m.model_id,
		       COALESCE(m.last_seen_at, m.created_at), m.enabled, m.missing_scans,
		       m.auto_retired_at, m.manually_enabled_at
		  FROM models m
		  JOIN providers p ON p.id = m.provider_id
		 WHERE p.enabled = true
		   AND m.disabled_manually = false
		   AND (
		         (m.enabled = false AND m.discovery_dismissed_at IS NULL)
		      OR (m.enabled = true AND m.missing_scans > 0)
		       )`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []claimRow
	for rows.Next() {
		var r claimRow
		if err := rows.Scan(&r.ProviderID, &r.ProviderName, &r.ModelID, &r.LastSeenAt, &r.Enabled, &r.MissingScans, &r.RetiredAt, &r.PinnedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// flapCounts tallies membership transitions per (provider, model) from the
// journal since the given time. Concatenating the three membership arrays and
// expanding them once is cheaper than three passes, and the metadata-only
// `updated` bucket is deliberately excluded: a price move is not a flap.
func flapCounts(ctx context.Context, pool *pgxpool.Pool, since time.Time) (map[flapKey]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT dc.provider_id::text, e->>'model_id', COUNT(*)
		  FROM discovery_changes dc
		  CROSS JOIN LATERAL jsonb_array_elements(
		           COALESCE(dc.diff->'added',     '[]'::jsonb) ||
		           COALESCE(dc.diff->'reenabled', '[]'::jsonb) ||
		           COALESCE(dc.diff->'disabled',  '[]'::jsonb)
		       ) AS e
		 WHERE dc.detected_at >= $1
		   AND dc.provider_id IS NOT NULL
		 GROUP BY 1, 2`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[flapKey]int)
	for rows.Next() {
		var k flapKey
		var n int
		if err := rows.Scan(&k.providerID, &k.modelID, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// buildProviderClaims classifies rows and groups them by provider, returning
// the groups in display order and the badge count (see countedClaims: Gone and
// Retired, never Stale or Suspect).
//
// Auto-dismiss is applied here as a predicate rather than by stamping a column:
// nothing is written, so aged-out stays distinguishable from operator-dismissed,
// changing ClaimWindow re-derives every claim with no backfill, and a stale
// model that flaps again starts counting on its own with no un-dismiss step.
func buildProviderClaims(rows []claimRow, window, sinceReview map[flapKey]int, now time.Time) ([]ProviderClaims, int) {
	byProvider := make(map[string]*ProviderClaims)
	count := 0

	for _, r := range rows {
		k := flapKey{providerID: r.ProviderID, modelID: r.ModelID}
		c := ModelClaim{
			ModelID:         r.ModelID,
			LastSeenAt:      r.LastSeenAt,
			MissingScans:    r.MissingScans,
			FlapWindow:      window[k],
			FlapSinceReview: sinceReview[k],
			RetiredAt:       r.RetiredAt,
		}

		g := byProvider[r.ProviderID]
		if g == nil {
			// Every bucket starts as [] rather than nil so the JSON
			// response always serializes them as [], never null: Claims and
			// Informational already do this (buildProviderClaims below,
			// discovery_changes.go), and the frontend types promise
			// ModelClaim[] with no null guard.
			g = &ProviderClaims{
				ProviderID:   r.ProviderID,
				ProviderName: r.ProviderName,
				Gone:         []ModelClaim{},
				Stale:        []ModelClaim{},
				Suspect:      []ModelClaim{},
				Retired:      []ModelClaim{},
				Pinned:       []ModelClaim{},
			}
			byProvider[r.ProviderID] = g
		}

		switch {
		// Ahead of the suspect case because both describe the same shape of row
		// (still enabled, mid-miss-streak) and only the pin tells them apart. A
		// suspect row is a warning that discovery is about to disable the model;
		// a pinned row is a decision the operator already made that discovery
		// will not overrule, so warning about it would nag them about their own
		// configuration.
		case r.Enabled && r.PinnedAt != nil:
			c.PinnedAt = r.PinnedAt
			c.State = ClaimStatePinned
			g.Pinned = append(g.Pinned, c)
		case r.Enabled:
			c.State = ClaimStateSuspect
			g.Suspect = append(g.Suspect, c)
		// Ahead of the stale check on purpose. Staleness is measured from
		// last_seen_at, and a retired model is still being listed, so that clock
		// keeps resetting and it could never age out anyway — but reading the
		// order as "retired models can go stale" would be wrong, and the reason
		// is worth stating where the decision is made.
		case r.RetiredAt != nil:
			c.State = ClaimStateRetired
			g.Retired = append(g.Retired, c)
			count++
		case now.Sub(r.LastSeenAt) > ClaimWindow && c.FlapWindow == 0:
			c.State = ClaimStateStale
			g.Stale = append(g.Stale, c)
		default:
			c.State = ClaimStateGone
			g.Gone = append(g.Gone, c)
			count++
		}
	}

	out := make([]ProviderClaims, 0, len(byProvider))
	for _, g := range byProvider {
		sortClaims(g.Gone)
		sortClaims(g.Stale)
		sortClaims(g.Suspect)
		sortClaims(g.Retired)
		sortClaims(g.Pinned)
		out = append(out, *g)
	}
	// Most counted claims first, then most suspect, then by name, then by ID.
	// A stale-only provider scores (0,0) and lands at the bottom, which is
	// where a section that resolved during the session should sink to without
	// vanishing. The final ID tiebreak makes the ordering fully deterministic:
	// provider name has no uniqueness guarantee, out came from ranging over a
	// Go map (unordered), and sort.Slice is not stable, so without it two
	// identically-named providers with identical bucket counts could swap
	// position between refreshes and jitter the UI.
	sort.Slice(out, func(i, j int) bool {
		if countedClaims(out[i]) != countedClaims(out[j]) {
			return countedClaims(out[i]) > countedClaims(out[j])
		}
		if len(out[i].Gone) != len(out[j].Gone) {
			return len(out[i].Gone) > len(out[j].Gone)
		}
		if len(out[i].Suspect) != len(out[j].Suspect) {
			return len(out[i].Suspect) > len(out[j].Suspect)
		}
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		return out[i].ProviderID < out[j].ProviderID
	})
	return out, count
}

// countedClaims is how many of a provider's claims count towards the badge, and
// therefore towards the alert. Gone and Retired both do; Stale, Suspect and
// Pinned never have. Pinned is the clearest of the three: the operator enabled
// the model themselves, so counting it would ask them to decide again.
//
// One function rather than the count repeated at each site: the badge total, the
// provider ordering and the alert's per-provider figures all have to agree, and
// they disagreed the moment Retired was added to the total but not to the other
// two.
func countedClaims(p ProviderClaims) int { return len(p.Gone) + len(p.Retired) }

func sortClaims(cs []ModelClaim) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].ModelID < cs[j].ModelID })
}

// setModelsDismissed stamps the operator dismissal for the given models. Returns
// how many rows changed so the handler can report an unknown model instead of
// silently succeeding.
//
// Stamp-only: there is no clear direction. A dismissal is undone by discovery
// itself, since Upsert nulls the column on any sighting, so nothing needs to
// clear it by hand. A traffic-retired model is the one exception to that
// sighting rule (Upsert keeps its stamp, or it could never be silenced), and it
// gets there instead via an operator enabling the model: that drops the
// retirement stamp, and the next sighting clears the dismissal as usual.
//
// The UPDATE only ever touches rows that are currently gone (enabled = false)
// and not manually disabled: Upsert clears the stamp on a SIGHTING, and a
// suspect (still enabled, missing_scans > 0) or healthy model is by definition
// not being sighted, so pre-dismissing one would silently hide a real claim the
// next time it actually goes gone.
func setModelsDismissed(ctx context.Context, pool *pgxpool.Pool, providerID uuid.UUID, modelIDs []string) ([]string, error) {
	// RETURNING, not a row count. A count says HOW MANY of the requested models
	// were dismissed but not WHICH, and the caller cannot derive the difference:
	// the WHERE clause can skip a model for reasons the caller has no view of (it
	// was sighted and re-enabled, disabled by hand, or deleted since the list was
	// read). A dashboard left guessing then mislabels the ones that did land - the
	// UI reads a dismissed model's absence from the next status read as "listed
	// again" - so the endpoint names them instead.
	rows, err := pool.Query(ctx,
		`UPDATE models SET discovery_dismissed_at = now()
		  WHERE provider_id = $1 AND model_id = ANY($2)
		    AND enabled = false AND disabled_manually = false
		RETURNING model_id`,
		providerID, modelIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Never nil: the JSON response promises dismissed: string[] with no null guard
	// on the client.
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// setModelsUnpinned drops the operator pin from the given models, handing them
// back to discovery's listing-based auto-disable. Returns which rows it cleared,
// so the handler can report a model that carried no pin instead of silently
// succeeding.
//
// missing_scans is reset in the same statement. Unpinning restarts automatic
// management from fresh evidence, and the streak is part of that evidence: a pin
// holds while the streak keeps climbing, so by the time anyone unpins it is
// usually well past MissingScanThreshold. Clearing only the stamp would disable
// the model on the very next scan, which contradicts what the operator is told
// ("disabled again after two scans") and is not the decision they made.
//
// The `manually_enabled_at IS NOT NULL` guard is what makes the RETURNING list
// meaningful: without it every named row would come back, including ones that
// were never pinned, and the caller could no longer tell an unpin from a no-op.
func setModelsUnpinned(ctx context.Context, pool *pgxpool.Pool, providerID uuid.UUID, modelIDs []string) ([]string, error) {
	// RETURNING, not a row count, for the same reason as setModelsDismissed: a
	// count says HOW MANY of the requested models were unpinned but not WHICH,
	// and the caller cannot derive the difference — a sighting may have cleared
	// the pin, or the model may have been deleted, since the list was read.
	rows, err := pool.Query(ctx,
		`UPDATE models SET manually_enabled_at = NULL, missing_scans = 0
		  WHERE provider_id = $1 AND model_id = ANY($2)
		    AND manually_enabled_at IS NOT NULL
		RETURNING model_id`,
		providerID, modelIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Never nil: the JSON response promises unpinned: string[] with no null guard
	// on the client.
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PruneDiscoveryChanges deletes seen journal rows older than the window. Safe
// only because claims are derived from `models`: a journal row can no longer be
// the sole evidence of a pending claim.
func PruneDiscoveryChanges(ctx context.Context, pool *pgxpool.Pool, before time.Time) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM discovery_changes WHERE seen AND detected_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
