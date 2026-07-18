package frontdesk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// CreateMember validates and inserts a new member. name must be non-empty and
// rawURL must be a valid http(s) URL with a host; the URL is normalized (scheme
// lowercased, trailing slash trimmed) and deduped. token is optional; when set
// it is encrypted at rest with the store master key.
func (s *Store) CreateMember(ctx context.Context, name, rawURL, token string) (*Member, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	normURL, err := normalizeMemberURL(rawURL, s.allowHTTPMembers)
	if err != nil {
		return nil, err
	}

	cipher, nonce, salt, err := s.encryptToken(token)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	now := time.Now().UTC().UnixNano()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO members (id, name, url, state, token_cipher, token_nonce, token_salt, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, normURL, string(StateActive), cipher, nonce, salt, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateURL
		}
		return nil, fmt.Errorf("frontdesk: insert member: %w", err)
	}
	return s.GetMember(ctx, id)
}

// ListMembers returns all members ordered by creation time.
func (s *Store) ListMembers(ctx context.Context) ([]*Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, state, token_cipher, created_at, updated_at, last_config_sync_at, last_config_sync_reason, instance_id FROM members ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: list members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []*Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// GetMember returns one member by id, or ErrNotFound.
func (s *Store) GetMember(ctx context.Context, id string) (*Member, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, state, token_cipher, created_at, updated_at, last_config_sync_at, last_config_sync_reason, instance_id FROM members WHERE id = ?`, id,
	)
	m, err := scanMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// RenameMember updates a member's display name.
func (s *Store) RenameMember(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	return s.touchMember(ctx, `UPDATE members SET name = ?, updated_at = ? WHERE id = ?`, id, name)
}

// SetMemberToken encrypts and stores a member's admin token. An empty token
// clears it (no token stored).
func (s *Store) SetMemberToken(ctx context.Context, id, token string) error {
	cipher, nonce, salt, err := s.encryptToken(token)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE members SET token_cipher = ?, token_nonce = ?, token_salt = ?, updated_at = ? WHERE id = ?`,
		cipher, nonce, salt, time.Now().UTC().UnixNano(), id,
	)
	return affectedOrNotFound(res, err)
}

// SetMemberState sets a member's state (active or drained). Draining is refused
// when it would leave the fleet with zero active members: the Traefik backend
// pool would be empty and all proxy traffic would fail, so at least one member
// (the primary or any replica) must always stay routable. This guards the
// routing-pool count, not the primary's identity, so draining the primary is
// allowed as long as a replica is active (a legitimate maintenance action);
// conversely the last active member cannot be drained whoever it is. Activating
// is always allowed. The active-count check and the state write are a single
// atomic statement, so a concurrent drain elsewhere cannot slip between them and
// empty the pool.
func (s *Store) SetMemberState(ctx context.Context, id string, state MemberState) error {
	if state != StateActive && state != StateDrained {
		return fmt.Errorf("%w: invalid state %q", ErrValidation, state)
	}
	if state == StateActive {
		return s.touchMember(ctx, `UPDATE members SET state = ?, updated_at = ? WHERE id = ?`, id, string(state))
	}
	// Drain only if some other member is still active. The EXISTS sub-query makes
	// the guard and the write one atomic statement (no TOCTOU with a concurrent
	// drain that a two-step count+update would have).
	res, err := s.db.ExecContext(ctx, `
		UPDATE members SET state = ?, updated_at = ?
		WHERE id = ?
		  AND EXISTS (SELECT 1 FROM members WHERE state = ? AND id != ?)`,
		string(StateDrained), time.Now().UTC().UnixNano(), id, string(StateActive), id)
	if err != nil {
		return fmt.Errorf("frontdesk: drain member: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Zero rows means either the member is gone or the guard tripped;
		// disambiguate so the server returns 404 vs 409.
		var exists bool
		if qerr := s.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM members WHERE id = ?)`, id).Scan(&exists); qerr != nil {
			return fmt.Errorf("frontdesk: drain member existence check: %w", qerr)
		}
		if !exists {
			return ErrNotFound
		}
		return ErrLastActiveMember
	}
	return nil
}

// DeleteMember removes a member by id.
func (s *Store) DeleteMember(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM members WHERE id = ?`, id)
	if err := affectedOrNotFound(res, err); err != nil {
		return err
	}
	// If the removed member was the designated auto-sync primary, clear the
	// pointer so the auto-sync loop stops treating a now-gone member as the
	// source of truth. Best-effort: a failure here only leaves a dangling id the
	// loop already guards against.
	_, _ = s.db.ExecContext(ctx, `UPDATE settings SET auto_sync_primary_id = '' WHERE auto_sync_primary_id = ?`, id)
	return nil
}

// SetMemberInstanceID records the stable identity Front Desk learned for a
// member from its /api/system. Idempotent; a no-op if the row is gone.
func (s *Store) SetMemberInstanceID(ctx context.Context, id, instanceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE members SET instance_id = ? WHERE id = ?`, instanceID, id)
	return err
}

// DeleteMemberIfNotPrimary removes a member by id, but never the configured
// fleet primary. The primary is the config source of truth and can only be
// changed by re-running the Fleet Sync wizard (a token-gated repoint), so there
// is deliberately no way to delete it directly, with or without a token. The
// primary-status check and the delete are a single atomic SQL statement, so a
// concurrent primary reassignment cannot slip between the check and the delete
// (the TOCTOU window a two-step GetAutoSync + DeleteMember would have). Returns
// applied=false when the member is the current primary; the caller should
// respond 409 and point the operator at the wizard.
func (s *Store) DeleteMemberIfNotPrimary(ctx context.Context, id string) (applied bool, err error) {
	// The delete and its ghost-state cleanup run in one transaction, so a crash
	// mid-way can never leave a fleet_sync_state row naming a member that was
	// already removed (exactly the ghost that made the old badge misreport who the
	// primary was).
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("frontdesk: begin delete member: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op

	// Delete only if the member is NOT the fleet primary AND removing it would not
	// empty the routing pool (an active member must never be the last active one:
	// that is the same invariant SetMemberState enforces for draining, reached
	// here via the delete door). Draining a drained member is always safe (it is
	// already out of the pool). The sub-queries make the checks and the delete a
	// single atomic statement, so a concurrent repoint or drain cannot slip
	// between the check and the delete.
	res, err := tx.ExecContext(ctx, `
		DELETE FROM members
		WHERE id = ?
		  AND id NOT IN (SELECT auto_sync_primary_id FROM settings WHERE id = 1)
		  AND (state != ? OR EXISTS (SELECT 1 FROM members WHERE state = ? AND id != ?))`,
		id, string(StateActive), string(StateActive), id)
	if err != nil {
		return false, fmt.Errorf("frontdesk: delete member: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		// Not deleted: either the member is the primary (existing refusal, reported
		// as applied=false) or it is the last active member (new routing-pool
		// guard). The caller has already confirmed the member exists, so
		// disambiguate the two so the server returns the right 409.
		var isPrimary bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM settings WHERE id = 1 AND auto_sync_primary_id = ?)`,
			id).Scan(&isPrimary); err != nil {
			return false, fmt.Errorf("frontdesk: delete member primary check: %w", err)
		}
		if isPrimary {
			return false, nil
		}
		return false, ErrLastActiveMember
	}
	// A removed non-primary member must not linger as the auto-sync primary (it
	// never should, but stay defensive) nor as the stale "last run" marker.
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET auto_sync_primary_id = '' WHERE auto_sync_primary_id = ?`, id); err != nil {
		return false, fmt.Errorf("frontdesk: clear auto-sync primary: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fleet_sync_state SET primary_id = '', primary_name = '' WHERE id = 1 AND primary_id = ?`, id); err != nil {
		return false, fmt.Errorf("frontdesk: clear ghost fleet state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("frontdesk: commit delete member: %w", err)
	}
	return true, nil
}

// MemberToken decrypts and returns a member's stored admin token. ok is false
// when no token is stored for the member.
func (s *Store) MemberToken(ctx context.Context, id string) (token string, ok bool, err error) {
	var cipher, nonce, salt []byte
	row := s.db.QueryRowContext(ctx, `SELECT token_cipher, token_nonce, token_salt FROM members WHERE id = ?`, id)
	if err := row.Scan(&cipher, &nonce, &salt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrNotFound
		}
		return "", false, fmt.Errorf("frontdesk: load member token: %w", err)
	}
	if len(cipher) == 0 {
		return "", false, nil
	}
	plain, err := auth.Decrypt(cipher, nonce, salt, s.masterKey)
	if err != nil {
		return "", false, fmt.Errorf("frontdesk: decrypt member token: %w", err)
	}
	return plain, true, nil
}

// touchMember runs an UPDATE that sets one column plus updated_at and maps a
// zero-row result to ErrNotFound. The query must take (value, updated_at, id).
func (s *Store) touchMember(ctx context.Context, query, id, value string) error {
	res, err := s.db.ExecContext(ctx, query, value, time.Now().UTC().UnixNano(), id)
	return affectedOrNotFound(res, err)
}

// encryptToken encrypts a non-empty token with the store master key. An empty
// token yields three nil slices (cleared). A non-empty token with no master key
// is a validation error so plaintext is never written.
func (s *Store) encryptToken(token string) (cipher, nonce, salt []byte, err error) {
	if token == "" {
		return nil, nil, nil, nil
	}
	if s.masterKey == "" {
		return nil, nil, nil, fmt.Errorf("%w: a master key is required to store a member admin token", ErrValidation)
	}
	kp, err := auth.Encrypt(token, s.masterKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("frontdesk: encrypt member token: %w", err)
	}
	return kp.Ciphertext, kp.Nonce, kp.Salt, nil
}
