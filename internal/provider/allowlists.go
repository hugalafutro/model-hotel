package provider

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// execer is the subset of pgx that both a pool and a transaction satisfy, so
// PruneAllowLists can run inside whichever the caller already has open.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// The two allow-list columns holding provider UUIDs, rewritten identically.
// Spelled out per table rather than built from a table name, so the SQL stays
// greppable and no statement is ever assembled from a string at runtime.
//
// The rewrite is set-based rather than one array_remove call per id, so a bulk
// delete costs one statement per table however many providers went away:
//
//   - array_agg over unnest keeps the surviving members in their original order;
//   - COALESCE(..., '{}') is what makes a fully pruned list come back as an
//     EMPTY array rather than NULL, which is the entire point (see below);
//   - the && overlap test skips rows referencing none of the deleted ids, and
//     since NULL && anything is NULL rather than true, it also guarantees an
//     unrestricted row is never selected and so never rewritten.
const pruneVirtualKeysSQL = `
	UPDATE virtual_keys vk
	   SET allowed_providers = COALESCE((
	           SELECT array_agg(elem)
	             FROM unnest(vk.allowed_providers) AS elem
	            WHERE elem <> ALL($1)
	       ), '{}'::text[])
	 WHERE vk.allowed_providers && $1`

const pruneUsersSQL = `
	UPDATE users u
	   SET allowed_providers = COALESCE((
	           SELECT array_agg(elem)
	             FROM unnest(u.allowed_providers) AS elem
	            WHERE elem <> ALL($1)
	       ), '{}'::text[])
	 WHERE u.allowed_providers && $1`

// PruneAllowLists removes the given provider ids from every virtual key's
// allowed_providers and every user account's allowed_providers. Call it in the
// same transaction as the DELETE that removed those providers, so a failure
// cannot leave the providers gone with their ids still referenced.
//
// It MUST be called by any future code path that deletes a provider, and
// NOTHING ENFORCES THAT. A Postgres array cannot carry a foreign key, so there
// is no declarative referential action to fall back on, and the trigger that
// would have covered raw SQL too was rejected because it breaks the backup
// restore rail: every pg_dump would then carry a FUNCTION and a TRIGGER, which
// checkDangerousObjects in internal/api/backup_restore.go refuses, and no
// name-based exemption can tell those objects from a tampered dump reusing
// their names. The full reasoning is recorded in
// internal/db/migrations/066_prune_provider_allowlists.sql. This missing
// enforcement is a known, accepted gap, not an oversight.
//
// A provider deleted without calling this leaves its id dangling in both
// columns. That is inert at the proxy, which matches candidates by id, but it
// silently widens a restricted key when config-sync re-exports it: the export
// translates ids to provider names and an unresolvable id just vanishes.
//
// The two callers today are Repository.Delete and the declarative provider
// replace in internal/api/configsync_apply.go.
//
// Emptying a list is a correct outcome here, not a failure to guard against.
// The proxy reads any non-NULL allowed_providers as "exactly these providers,
// including none of them", so a key or account scoped solely to deleted
// providers ends up denying everything, which is what it already did while the
// ids were merely dangling. NULL, the only value meaning unrestricted, is never
// written by this function.
func PruneAllowLists(ctx context.Context, db execer, providerIDs []string) error {
	if len(providerIDs) == 0 {
		return nil
	}
	if _, err := db.Exec(ctx, pruneVirtualKeysSQL, providerIDs); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, pruneUsersSQL, providerIDs); err != nil {
		return err
	}
	return nil
}
