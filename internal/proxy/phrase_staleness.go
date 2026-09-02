package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The attempt trail records which phrase decided each 429
// (attemptRecord.Phrase). This file checks the rate-limit phrase table against
// that trail daily and names any phrase that has matched nothing in
// PhraseStalenessHorizon, which points at a provider that rewrote its error
// text.

// PhraseStalenessHorizon is how long a phrase may go unmatched before it is
// reported. Ninety days covers a provider an operator only touches quarterly.
const PhraseStalenessHorizon = 90 * 24 * time.Hour

// StalePhrase is one table entry the report names: the phrase, the provider it
// was observed on and when it was added.
type StalePhrase struct {
	Phrase   string
	Provider string
	Observed string
}

// StalePhrases lists the phrase-table entries that have matched no attempt in
// the horizon ending at now. An entry added inside the horizon is never stale:
// it has not had the horizon to prove itself.
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
// instant names the phrase. The containment predicate is served by the GIN
// index on attempts; created_at bounds the scan.
func phraseMatchedSince(ctx context.Context, pool *pgxpool.Pool, phrase string, since time.Time) (bool, error) {
	// json.Marshal, not %q: Go's quoting is not JSON quoting, and a phrase
	// with a byte outside JSON's escapes would abort the report.
	needle, err := json.Marshal([]map[string]string{{"phrase": phrase}})
	if err != nil {
		return false, err
	}
	var matched bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM request_logs
			WHERE created_at >= $1 AND attempts @> $2::jsonb
		)`, since, string(needle)).Scan(&matched)
	return matched, err
}

// ReportStalePhrases runs the check once and logs the result: one Warn naming
// every stale phrase, or a Debug saying the table is healthy.
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
// ctx ends.
func PhraseStalenessLoop(ctx context.Context, pool *pgxpool.Pool) {
	phraseStalenessLoop(ctx, pool, 10*time.Minute, 24*time.Hour)
}

func phraseStalenessLoop(ctx context.Context, pool *pgxpool.Pool, first, every time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		ReportStalePhrases(ctx, pool, time.Now())
		timer.Reset(every)
	}
}
