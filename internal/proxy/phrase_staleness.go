package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The rate-limit phrase table is data, and data rots: a provider rewrites its
// error text and the entry keeps matching nothing, silently. The attempt trail
// records which phrase decided each 429 (attemptRecord.Phrase), so once a day
// the table is checked against it and any phrase that has matched nothing in
// PhraseStalenessHorizon is named in the app log, where an operator reads it
// as a maintenance report rather than finding out from the next incident.

// PhraseStalenessHorizon is how long a phrase may go unmatched before it is
// reported. Ninety days: long enough to cover a provider an operator only
// touches quarterly, short enough that a rewritten error text is noticed
// inside a season.
const PhraseStalenessHorizon = 90 * 24 * time.Hour

// StalePhrase is one table entry the report names: the phrase, who it was
// observed on and when it was added.
type StalePhrase struct {
	Phrase   string
	Provider string
	Observed string
}

// StalePhrases lists the phrase-table entries that have matched no attempt in
// the horizon ending at now. An entry added inside the horizon is never stale:
// it has not had the horizon to prove itself, and the table's own dates are
// the only evidence from before the trail existed. Each phrase costs one
// indexed containment probe on request_logs.attempts.
func StalePhrases(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]StalePhrase, error) {
	since := now.Add(-PhraseStalenessHorizon)
	var stale []StalePhrase
	for _, p := range rateLimitPhrases {
		if observed, err := time.Parse("2006-01-02", p.observed); err == nil && observed.After(since) {
			continue
		}
		matched, err := phraseMatchedSince(ctx, pool, p.phrase, since)
		if err != nil {
			return nil, err
		}
		if !matched {
			stale = append(stale, StalePhrase{Phrase: p.phrase, Provider: p.provider, Observed: p.observed})
		}
	}
	return stale, nil
}

// phraseMatchedSince reports whether any attempt trail written since the
// instant names the phrase. The containment predicate is what the GIN index on
// attempts serves; created_at bounds the scan the way the logs page does.
func phraseMatchedSince(ctx context.Context, pool *pgxpool.Pool, phrase string, since time.Time) (bool, error) {
	needle := fmt.Sprintf(`[{"phrase":%q}]`, phrase)
	var matched bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM request_logs
			WHERE created_at >= $1 AND attempts @> $2::jsonb
		)`, since, needle).Scan(&matched)
	return matched, err
}

// ReportStalePhrases runs the check once and logs the result: one Warn naming
// every stale phrase, or a Debug saying the table is healthy. It is the daily
// maintenance report the phrase table's design asks for.
func ReportStalePhrases(ctx context.Context, pool *pgxpool.Pool, now time.Time) {
	stale, err := StalePhrases(ctx, pool, now)
	if err != nil {
		debuglog.Warn("rate-limit phrases: staleness check failed", "error", err)
		return
	}
	if len(stale) == 0 {
		debuglog.Debug("rate-limit phrases: every entry matched inside the horizon", "phrases", len(rateLimitPhrases), "horizon_days", int(PhraseStalenessHorizon.Hours()/24))
		return
	}
	phrases := make([]string, 0, len(stale))
	for _, s := range stale {
		phrases = append(phrases, fmt.Sprintf("%q (%s, added %s)", s.Phrase, s.Provider, s.Observed))
	}
	debuglog.Warn("rate-limit phrases: entries unmatched inside the horizon; the provider may have rewritten its error text", "count", len(stale), "horizon_days", int(PhraseStalenessHorizon.Hours()/24), "phrases", phrases)
}

// PhraseStalenessLoop reports once shortly after start and then daily, until
// ctx ends. Started from the server's background loops beside the stale-log
// sweep.
func PhraseStalenessLoop(ctx context.Context, pool *pgxpool.Pool) {
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		ReportStalePhrases(ctx, pool, time.Now())
		timer.Reset(24 * time.Hour)
	}
}
