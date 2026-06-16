package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DiscoveryChangeEntry is one provider's recorded background-discovery diff.
type DiscoveryChangeEntry struct {
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
		len(d.FailoverUpdatedGroups) == 0
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
		len(d.FailoverUpdatedGroups)
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

// listPendingDiscoveryChanges returns the unseen recorded diffs, newest-first.
func listPendingDiscoveryChanges(ctx context.Context, pool *pgxpool.Pool) ([]DiscoveryChangeEntry, error) {
	rows, err := pool.Query(ctx,
		`SELECT provider_name, source, detected_at, diff
		   FROM discovery_changes
		  WHERE NOT seen
		  ORDER BY detected_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DiscoveryChangeEntry
	for rows.Next() {
		var (
			entry    DiscoveryChangeEntry
			diffJSON []byte
		)
		if err := rows.Scan(&entry.ProviderName, &entry.Source, &entry.DetectedAt, &diffJSON); err != nil {
			return nil, err
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

// markDiscoveryChangesSeen flips every unseen row to seen, clearing the badge.
func markDiscoveryChangesSeen(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `UPDATE discovery_changes SET seen = true WHERE NOT seen`)
	return err
}
