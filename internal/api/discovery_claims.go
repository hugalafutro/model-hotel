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
}

// ProviderClaims groups one provider's claims by state.
type ProviderClaims struct {
	ProviderID   string       `json:"provider_id"`
	ProviderName string       `json:"provider_name"`
	Gone         []ModelClaim `json:"gone"`
	Stale        []ModelClaim `json:"stale"`
	Suspect      []ModelClaim `json:"suspect"`
}

// claimRow is one candidate model straight from the derivation query.
type claimRow struct {
	ProviderID   string
	ProviderName string
	ModelID      string
	LastSeenAt   time.Time
	Enabled      bool
	MissingScans int
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
func listClaimRows(ctx context.Context, pool *pgxpool.Pool) ([]claimRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT m.provider_id::text, p.name, m.model_id,
		       COALESCE(m.last_seen_at, m.created_at), m.enabled, m.missing_scans
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
		if err := rows.Scan(&r.ProviderID, &r.ProviderName, &r.ModelID, &r.LastSeenAt, &r.Enabled, &r.MissingScans); err != nil {
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
// the groups in display order and the badge count (Gone only).
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
		}

		g := byProvider[r.ProviderID]
		if g == nil {
			g = &ProviderClaims{ProviderID: r.ProviderID, ProviderName: r.ProviderName}
			byProvider[r.ProviderID] = g
		}

		switch {
		case r.Enabled:
			c.State = ClaimStateSuspect
			g.Suspect = append(g.Suspect, c)
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

func sortClaims(cs []ModelClaim) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].ModelID < cs[j].ModelID })
}

// setModelsDismissed stamps or clears the operator dismissal for the given
// models. Returns how many rows changed so the handler can report an unknown
// model instead of silently succeeding.
//
// The UPDATE only ever touches rows that are currently gone (enabled = false)
// and not manually disabled: Upsert clears the stamp on a SIGHTING, and a
// suspect (still enabled, missing_scans > 0) or healthy model is by
// definition not being sighted, so pre-dismissing one would silently hide a
// real claim the next time it actually goes gone. Undo (dismissed=false)
// still works under this same restriction, because a dismissed model is still
// enabled = false.
func setModelsDismissed(ctx context.Context, pool *pgxpool.Pool, providerID uuid.UUID, modelIDs []string, dismissed bool) (int64, error) {
	var stamp any
	if dismissed {
		stamp = time.Now()
	}
	tag, err := pool.Exec(ctx,
		`UPDATE models SET discovery_dismissed_at = $3
		  WHERE provider_id = $1 AND model_id = ANY($2)
		    AND enabled = false AND disabled_manually = false`,
		providerID, modelIDs, stamp)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// pruneDiscoveryChanges deletes seen journal rows older than the window. Safe
// only because claims are derived from `models`: a journal row can no longer be
// the sole evidence of a pending claim.
func pruneDiscoveryChanges(ctx context.Context, pool *pgxpool.Pool, before time.Time) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM discovery_changes WHERE seen AND detected_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
