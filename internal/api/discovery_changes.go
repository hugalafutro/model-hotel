package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DiscoveryChangeEntry is one provider's recorded background-discovery diff.
type DiscoveryChangeEntry struct {
	// ProviderID is empty when the provider was deleted after the row was
	// recorded (the column is nullable). The dashboard uses it to offer a
	// per-provider Retest action from the changes modal.
	ProviderID   string         `json:"provider_id,omitempty"`
	ProviderName string         `json:"provider_name"`
	Source       string         `json:"source"`
	DetectedAt   time.Time      `json:"detected_at"`
	Diff         *DiscoveryDiff `json:"diff"`
}

// DiscoveryChangesResponse is the payload for GET /api/discovery/changes: the
// unseen entries newest-first plus the total affected-model count for the badge.
type DiscoveryChangesResponse struct {
	Entries []DiscoveryChangeEntry `json:"entries"`
	Count   int                    `json:"count"`
}

// diffIsEmpty reports whether a diff carries no changes worth recording.
func diffIsEmpty(d *DiscoveryDiff) bool {
	if d == nil {
		return true
	}
	return len(d.Added) == 0 &&
		len(d.Reenabled) == 0 &&
		len(d.Disabled) == 0 &&
		len(d.Updated) == 0 &&
		len(d.FailoverDeletedGroups) == 0 &&
		len(d.FailoverUpdatedGroups) == 0 &&
		len(d.FailoverDisabledGroups) == 0
}

// countAffected sums the entities a diff touched — the badge number.
func countAffected(d *DiscoveryDiff) int {
	if d == nil {
		return 0
	}
	return len(d.Added) +
		len(d.Reenabled) +
		len(d.Disabled) +
		len(d.Updated) +
		len(d.FailoverDeletedGroups) +
		len(d.FailoverUpdatedGroups) +
		len(d.FailoverDisabledGroups)
}

// floatPtrEq reports pointer-aware equality at float32 precision. Prices ride in
// REAL columns, so comparing at float32 matches what discovery's diffFloatPtr did
// when it recorded the change. Both nil is equal (field unset on both ends); one
// nil is not (a fill or clear is a genuine change).
func floatPtrEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return float32(*a) == float32(*b)
}

// roundTripKey identifies a single metadata field of one model under one provider
// across the cumulative entries, so swings of the same field can be chained.
type roundTripKey struct {
	provider string
	modelID  string
	field    string
}

// providerKeyOf keys a provider by ID when known (survives a rename) and falls
// back to the name for rows whose provider was deleted before review.
func providerKeyOf(e DiscoveryChangeEntry) string {
	if e.ProviderID != "" {
		return "id:" + e.ProviderID
	}
	return "name:" + e.ProviderName
}

// collapseRoundTrips folds metadata round-trips out of the cumulative
// background-discovery entries. When the same provider's model+field swings out
// and back across several recorded runs (e.g. OpenRouter reports a price from a
// different upstream and then the original one again), the net change is zero yet
// each run honestly recorded a real live change, so the review modal stacks a
// meaningless pair like "$0.49 → $0.182" above "$0.182 → $0.49". This drops any
// (provider, model, field) whose earliest pre-scan value equals its latest
// post-scan value from every entry, removes emptied model updates and entries,
// and preserves the original (newest-first) order. Membership churn
// (added/reenabled/disabled) and failover changes are left untouched: an add then
// remove still tells the reviewer the model flapped.
func collapseRoundTrips(entries []DiscoveryChangeEntry) []DiscoveryChangeEntry {
	if len(entries) < 2 {
		return entries
	}

	// Chain each field's earliest pre-scan value and latest post-scan value by
	// detection time, independent of the newest-first slice order.
	type endpoints struct {
		firstAt time.Time
		first   *float64
		lastAt  time.Time
		last    *float64
		seen    bool
	}
	chains := make(map[roundTripKey]*endpoints)
	for _, e := range entries {
		if e.Diff == nil {
			continue
		}
		pk := providerKeyOf(e)
		for _, u := range e.Diff.Updated {
			for _, c := range u.Changes {
				k := roundTripKey{provider: pk, modelID: u.ModelID, field: c.Field}
				ep := chains[k]
				if ep == nil {
					ep = &endpoints{firstAt: e.DetectedAt, first: c.Old, lastAt: e.DetectedAt, last: c.New, seen: true}
					chains[k] = ep
					continue
				}
				if e.DetectedAt.Before(ep.firstAt) {
					ep.firstAt, ep.first = e.DetectedAt, c.Old
				}
				if !e.DetectedAt.Before(ep.lastAt) {
					ep.lastAt, ep.last = e.DetectedAt, c.New
				}
			}
		}
	}

	// A field round-tripped when its earliest "from" equals its latest "to".
	drop := make(map[roundTripKey]bool)
	for k, ep := range chains {
		if ep.seen && floatPtrEq(ep.first, ep.last) {
			drop[k] = true
		}
	}
	if len(drop) == 0 {
		return entries
	}

	out := make([]DiscoveryChangeEntry, 0, len(entries))
	for _, e := range entries {
		if e.Diff == nil {
			out = append(out, e)
			continue
		}
		pk := providerKeyOf(e)
		updated := make([]ModelUpdate, 0, len(e.Diff.Updated))
		for _, u := range e.Diff.Updated {
			changes := make([]FieldChange, 0, len(u.Changes))
			for _, c := range u.Changes {
				if drop[roundTripKey{provider: pk, modelID: u.ModelID, field: c.Field}] {
					continue
				}
				changes = append(changes, c)
			}
			if len(changes) > 0 {
				updated = append(updated, ModelUpdate{ModelID: u.ModelID, Changes: changes})
			}
		}
		// Copy the diff so the trimmed Updated slice never mutates the caller's
		// value; everything else passes through unchanged.
		trimmed := *e.Diff
		trimmed.Updated = updated
		if diffIsEmpty(&trimmed) {
			continue
		}
		e.Diff = &trimmed
		out = append(out, e)
	}
	return out
}

// AppendDiscoveryChange records one provider's background-discovery diff for
// later review. Empty diffs are skipped. Returns true when a row was written so
// the caller can decide whether to publish a live-update event. providerID may
// be nil. Exported for the scheduled discovery loop in package main.
func AppendDiscoveryChange(ctx context.Context, pool *pgxpool.Pool, source string, providerID *uuid.UUID, providerName string, diff *DiscoveryDiff) (bool, error) {
	if diffIsEmpty(diff) {
		return false, nil
	}
	payload, err := json.Marshal(diff)
	if err != nil {
		return false, fmt.Errorf("marshal discovery diff: %w", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO discovery_changes (source, provider_id, provider_name, diff)
		 VALUES ($1, $2, $3, $4)`,
		source, providerID, providerName, payload)
	if err != nil {
		return false, fmt.Errorf("insert discovery change: %w", err)
	}
	return true, nil
}

// scanDiscoveryChangeRows decodes (provider_id, provider_name, source,
// detected_at, diff) rows into entries, shared by the list and ack queries.
func scanDiscoveryChangeRows(rows pgx.Rows) ([]DiscoveryChangeEntry, error) {
	defer rows.Close()
	var entries []DiscoveryChangeEntry
	for rows.Next() {
		var (
			entry      DiscoveryChangeEntry
			diffJSON   []byte
			providerID *string // nullable provider_id::text
		)
		if err := rows.Scan(&providerID, &entry.ProviderName, &entry.Source, &entry.DetectedAt, &diffJSON); err != nil {
			return nil, err
		}
		if providerID != nil {
			entry.ProviderID = *providerID
		}
		var diff DiscoveryDiff
		if err := json.Unmarshal(diffJSON, &diff); err != nil {
			return nil, fmt.Errorf("unmarshal discovery diff: %w", err)
		}
		entry.Diff = &diff
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// listPendingDiscoveryChanges returns the unseen recorded diffs, newest-first.
func listPendingDiscoveryChanges(ctx context.Context, pool *pgxpool.Pool) ([]DiscoveryChangeEntry, error) {
	rows, err := pool.Query(ctx,
		`SELECT provider_id::text, provider_name, source, detected_at, diff
		   FROM discovery_changes
		  WHERE NOT seen
		  ORDER BY detected_at DESC`)
	if err != nil {
		return nil, err
	}
	return scanDiscoveryChangeRows(rows)
}

// markDiscoveryChangesSeen flips every unseen row to seen and returns exactly the
// rows it marked (newest-first). Marking and reading in one UPDATE … RETURNING is
// atomic, so a row recorded between a separate SELECT and UPDATE can't be acked
// without ever being handed back for review — the caller snapshots the modal from
// this return value rather than a possibly-stale cache.
func markDiscoveryChangesSeen(ctx context.Context, pool *pgxpool.Pool) ([]DiscoveryChangeEntry, error) {
	rows, err := pool.Query(ctx,
		`WITH updated AS (
			UPDATE discovery_changes SET seen = true
			 WHERE NOT seen
			 RETURNING provider_id::text, provider_name, source, detected_at, diff
		)
		SELECT provider_id::text, provider_name, source, detected_at, diff
		  FROM updated
		 ORDER BY detected_at DESC`)
	if err != nil {
		return nil, err
	}
	return scanDiscoveryChangeRows(rows)
}
