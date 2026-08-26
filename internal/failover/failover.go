package failover

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

// FailoverGroup represents a configured failover group for a model.
//
//nolint:revive // stutter and exported are acceptable: FailoverGroup is a domain concept
type FailoverGroup struct {
	ID            uuid.UUID       `json:"id"`
	DisplayModel  string          `json:"display_model"`
	DisplayName   *string         `json:"display_name"`
	Description   string          `json:"description"`
	PriorityOrder []uuid.UUID     `json:"priority_order"`
	EntryEnabled  map[string]bool `json:"entry_enabled"`
	GroupEnabled  bool            `json:"group_enabled"`
	AutoCreated   bool            `json:"auto_created"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Repository provides persistence for failover groups.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new failover group repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetByModel retrieves a failover group by its display model name.
func (r *Repository) GetByModel(ctx context.Context, modelID string) (*FailoverGroup, error) {
	if fg, ok := GetCachedFailoverByModel(modelID); ok {
		return fg, nil
	}

	var fg FailoverGroup
	var priorityJSON []byte
	var entryEnabledJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE display_model = $1
	`, modelID).Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
		&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// Upsert creates or updates a failover group with the given priority order.
func (r *Repository) Upsert(ctx context.Context, displayModel string, priorityOrder []uuid.UUID) (*FailoverGroup, error) {
	return r.UpsertWithConfig(ctx, displayModel, priorityOrder, nil, nil, nil, nil, nil)
}

// UpsertWithConfig creates or updates a failover group with full configuration options.
func (r *Repository) UpsertWithConfig(ctx context.Context, displayModel string, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description *string, autoCreated *bool) (*FailoverGroup, error) {
	priorityJSON, err := jsonMarshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := jsonMarshal(entryEnabled)
	if err != nil {
		return nil, err
	}

	groupEnabledVal := true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	autoCreatedVal := false
	if autoCreated != nil {
		autoCreatedVal = *autoCreated
	}

	// Build ON CONFLICT DO UPDATE SET clause dynamically
	// so that nil display_name/description means "don't touch",
	// not "overwrite with NULL".
	// The INSERT VALUES positions are fixed ($1-$7), so the DO UPDATE SET
	// clause can reference them directly — we just conditionally include
	// display_name and description columns.
	// An empty-string pointer signals "clear to NULL".
	// auto_disabled_at is cleared on every group_enabled write that is not the
	// discovery auto-disable itself (migration 062). This path covers the sync's
	// upsertAutoGroup, which re-enables an auto group: an enabled group is by
	// definition not a claim, and a stale stamp left behind would make the NEXT
	// auto-disable read as an old one. The config-sync member import does NOT
	// go through here; it clears conditionally on its own (see the ON CONFLICT
	// clause in internal/api/configsync_apply.go).
	doSetClauses := []string{
		"priority_order = $2",
		"entry_enabled = $3",
		"group_enabled = $4",
		"auto_disabled_at = NULL",
	}
	// Pre-process displayName: empty string means "clear to NULL"
	insertDisplayName := displayName
	if displayName != nil && *displayName == "" {
		insertDisplayName = nil
		doSetClauses = append(doSetClauses, "display_name = NULL")
	} else if displayName != nil {
		doSetClauses = append(doSetClauses, "display_name = $5")
	}
	if description != nil {
		doSetClauses = append(doSetClauses, "description = $6")
	}
	doSetClauses = append(doSetClauses, "auto_created = $7", "updated_at = now()")

	query := fmt.Sprintf(`INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, display_name, description, auto_created)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (display_model)
		DO UPDATE SET %s
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`, strings.Join(doSetClauses, ", "))

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	err = r.pool.QueryRow(ctx, query, displayModel, priorityJSON, entryEnabledJSON, groupEnabledVal, insertDisplayName, description, autoCreatedVal).
		Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &rawPriority, &rawEntryEnabled,
			&fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// Delete removes a failover group by its display model name.
func (r *Repository) Delete(ctx context.Context, displayModel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = $1`, displayModel)
	InvalidateFailoverCache()
	return err
}

// DeleteByID removes a failover group by its ID.
func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE id = $1`, id)
	InvalidateFailoverCache()
	return err
}

// GetByID retrieves a failover group by its ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*FailoverGroup, error) {
	var fg FailoverGroup
	var priorityJSON []byte
	var entryEnabledJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE id = $1
	`, id).Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
		&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// GetEnabled returns all enabled failover groups.
func (r *Repository) GetEnabled(ctx context.Context) ([]*FailoverGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), group_enabled, COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE group_enabled = true
		ORDER BY display_model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFailoverGroups(rows)
}

// Update modifies an existing failover group by ID.
//
// This is the OPERATOR-facing update: it always writes group_enabled, and it
// always clears auto_disabled_at because every call to it represents someone's
// deliberate choice about the group (PUT /api/failover-groups/{id}, including
// the dashboard cascade that disables a group when toggling a member drops it
// below two routable entries). Internal maintenance must NOT come through here
// — a discovery-disabled group would lose its claim stamp as a side effect.
// pruneStaleEntries uses pruneMembership for exactly that reason; anything new
// that adjusts a group without an operator behind it should do the same.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description, displayModel *string) (*FailoverGroup, error) {
	priorityJSON, err := jsonMarshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := jsonMarshal(entryEnabled)
	if err != nil {
		return nil, err
	}

	groupEnabledVal := true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	var setClauses []string
	var args []any
	argIdx := 2 // $1 is reserved for id

	setClauses = append(setClauses, fmt.Sprintf("priority_order = $%d", argIdx))
	args = append(args, priorityJSON)
	argIdx++

	setClauses = append(setClauses, fmt.Sprintf("entry_enabled = $%d", argIdx))
	args = append(args, entryEnabledJSON)
	argIdx++

	setClauses = append(setClauses, fmt.Sprintf("group_enabled = $%d", argIdx))
	args = append(args, groupEnabledVal)
	argIdx++

	// Update is reachable only from the operator's PUT /api/failover-groups/{id}
	// (including the dashboard's cascade that disables a group when toggling a
	// member drops it below two routable entries). Every write through here is
	// therefore operator intent, so the discovery stamp is cleared
	// unconditionally: an operator-disabled group must never be counted as a
	// discovery claim, and re-enabling must leave no stamp for a later
	// auto-disable to inherit (migration 062).
	setClauses = append(setClauses, "auto_disabled_at = NULL")

	if displayName != nil {
		if *displayName == "" {
			// Empty string = clear to NULL
			setClauses = append(setClauses, "display_name = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
			args = append(args, *displayName)
			argIdx++
		}
	}

	if description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *description)
		argIdx++
	}

	if displayModel != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_model = $%d", argIdx))
		args = append(args, *displayModel)
	}

	setClauses = append(setClauses, "updated_at = now()")

	args = append([]any{id}, args...)

	query := fmt.Sprintf(`UPDATE model_failover_groups SET %s WHERE id = $1
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`, strings.Join(setClauses, ", "))

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	err = r.pool.QueryRow(ctx, query, args...).
		Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &rawPriority, &rawEntryEnabled,
			&fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// List returns all failover groups.
func (r *Repository) List(ctx context.Context) ([]*FailoverGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		ORDER BY display_model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFailoverGroups(rows)
}

func scanFailoverGroups(rows pgx.Rows) ([]*FailoverGroup, error) {
	var groups []*FailoverGroup
	for rows.Next() {
		var fg FailoverGroup
		var priorityJSON []byte
		var entryEnabledJSON []byte
		if err := rows.Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
			&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt); err != nil {
			debuglog.Warn("failover: row scan failed", "error", err)
			return nil, fmt.Errorf("scanFailoverGroups: row scan failed: %w", err)
		}
		if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal priority for %s: %w", fg.DisplayModel, err)
		}
		if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal entry_enabled for %s: %w", fg.DisplayModel, err)
		}
		groups = append(groups, &fg)
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("failover: error iterating rows in scanFailoverGroups", "error", err)
		return nil, fmt.Errorf("scanFailoverGroups: iteration error: %w", err)
	}
	return groups, nil
}
