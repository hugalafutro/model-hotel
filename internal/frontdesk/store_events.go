package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// InsertEvent persists a control-plane event. ID and CreatedAt are assigned
// when empty. The returned Event carries the persisted ID/timestamp.
func (s *Store) InsertEvent(ctx context.Context, e Event) (Event, error) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	var metaJSON *string
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return Event{}, fmt.Errorf("frontdesk: marshal event metadata: %w", err)
		}
		str := string(b)
		metaJSON = &str
	}
	var memberID *string
	if e.MemberID != "" {
		memberID = &e.MemberID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, type, severity, source, message, metadata, member_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Type, e.Severity, e.Source, e.Message, metaJSON, memberID, e.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return Event{}, fmt.Errorf("frontdesk: insert event: %w", err)
	}
	return e, nil
}

// ListEvents returns events matching the filter (newest first) plus the total
// count of matching rows (ignoring limit/offset) for pagination.
func (s *Store) ListEvents(ctx context.Context, f EventFilter) ([]Event, int, error) {
	where, args := eventWhere(f)

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("frontdesk: count events: %w", err)
	}

	//nolint:gosec // `where` is built only from fixed clause strings; all values are bound parameters.
	query := `SELECT id, type, severity, source, message, metadata, member_id, created_at FROM events` + where + ` ORDER BY created_at DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("frontdesk: list events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

// PruneEvents deletes events older than retentionDays and returns the count
// removed.
func (s *Store) PruneEvents(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays < 1 {
		return 0, fmt.Errorf("%w: retention must be at least 1 day", ErrValidation)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixNano()
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("frontdesk: prune events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func eventWhere(f EventFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.MemberID != "" {
		clauses = append(clauses, "member_id = ?")
		args = append(args, f.MemberID)
	}
	if f.Type != "" {
		clauses = append(clauses, "type = ?")
		args = append(args, f.Type)
	}
	if f.Severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, f.Severity)
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.Since.UTC().UnixNano())
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, f.Until.UTC().UnixNano())
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
