package frontdesk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// maxMemberNameLen caps a member's display name, in characters.
//
// The name is not decoration: the primary's copy rides in every fleet announce,
// whose receiving handler bounds that body at 1 KiB. An unbounded name
// therefore breaks announces fleet-wide, and near-silently, since the poller
// logs a failed announce at debug level only.
//
// The budget is in bytes and the cap is in characters, so the number has to
// survive the worst ratio between them: an astral-plane character is 4 bytes of
// UTF-8, and a character JSON has to escape is 6. 128 characters is therefore
// at most 768 bytes on the wire, which still leaves the announce's other fields
// room inside the kilobyte, while staying far beyond any real hostname-shaped
// label. TestMemberNameFitsAnnounceBudget marshals the actual worst case rather
// than trusting this arithmetic.
//
// Counted in characters and not bytes because that is what the operator is told
// and what they can see. Measuring bytes would silently give a shorter name to
// anyone not writing in ASCII, which this codebase deliberately does not do to
// identifiers elsewhere either.
const maxMemberNameLen = 128

// validMemberName trims a display name and rejects one that is empty or past
// maxMemberNameLen. Rejected rather than truncated, unlike a paired device's
// label: a member's name identifies it across the fleet, so silently storing a
// different name than the operator typed would be worse than refusing.
func validMemberName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > maxMemberNameLen {
		return "", fmt.Errorf("%w: name must be at most %d characters", ErrValidation, maxMemberNameLen)
	}
	return name, nil
}

// CreateMember validates and inserts a new member. name must be non-empty and
// rawURL must be a valid http(s) URL with a host; the URL is normalized (scheme
// lowercased, trailing slash trimmed) and deduped. token is optional; when set
// it is encrypted at rest with the store master key.
func (s *Store) CreateMember(ctx context.Context, name, rawURL, token string) (*Member, error) {
	name, err := validMemberName(name)
	if err != nil {
		return nil, err
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
	name, err := validMemberName(name)
	if err != nil {
		return err
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

// DeleteOutcome describes what DeleteMemberOrDisband did.
type DeleteOutcome int

const (
	// DeleteRefusedPrimary means the target is the designated fleet primary of
	// a fleet that keeps existing (two or more members). The primary is the
	// config source of truth and can only be changed by re-running the Fleet
	// Sync wizard (a token-gated repoint), so it is never deletable directly.
	DeleteRefusedPrimary DeleteOutcome = iota
	// DeleteApplied means one member was removed and the fleet lives on (it had
	// three or more members).
	DeleteApplied
	// DeleteDisbanded means the fleet had two members (or was a lone just-added
	// row), so removing one would have left a single-member fleet, a state that
	// is not allowed to exist. Every member row was removed, auto-sync switched
	// off, the primary designation cleared and the last-sync marker dropped.
	DeleteDisbanded
)

// RemovedMember identifies a member row a delete removed: the id for in-memory
// state cleanup, the name for the operator-facing event message.
type RemovedMember struct{ ID, Name string }

// DeleteMemberOrDisband removes a member by id, enforcing the fleet-size
// invariant that a fleet never shrinks to a single member: removal from a
// two-member fleet (or of a lone just-added row) disbands the whole fleet,
// primary included, returning Front Desk to its pristine no-fleet state. In a
// fleet of three or more it removes just the target, still refusing the
// designated primary and the last active member (the routing pool must never
// empty while a fleet exists). Every guard is re-checked inside the DELETE
// statement itself, so a concurrent add, drain or repoint cannot slip between
// the roster read and the write.
func (s *Store) DeleteMemberOrDisband(ctx context.Context, id string) (DeleteOutcome, []RemovedMember, error) {
	// The delete and its designation/sync-state cleanup run in one transaction,
	// so a crash mid-way can never leave a fleet_sync_state row or auto-sync
	// pointer naming a member that was already removed.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("frontdesk: begin delete member: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after a successful commit is a no-op

	roster, err := rosterSnapshot(ctx, tx)
	if err != nil {
		return 0, nil, err
	}
	var target RemovedMember
	found := false
	for _, m := range roster {
		if m.ID == id {
			target, found = m, true
		}
	}
	if !found {
		return 0, nil, ErrNotFound
	}

	if len(roster) <= 2 {
		// Two members: only the non-primary side may pull the plug (changing the
		// primary is the wizard's job). A lone row cannot be anyone's primary in
		// a functioning fleet, so it is always removable; if a stale designation
		// points at it anyway, disbanding clears it.
		res, err := tx.ExecContext(ctx, `
			DELETE FROM members
			WHERE (SELECT COUNT(*) FROM members) <= 2
			  AND EXISTS (SELECT 1 FROM members WHERE id = ?)
			  AND (? NOT IN (SELECT auto_sync_primary_id FROM settings WHERE id = 1)
			       OR (SELECT COUNT(*) FROM members) = 1)`,
			id, id)
		if err != nil {
			return 0, nil, fmt.Errorf("frontdesk: disband fleet: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, nil, err
		}
		if n == 0 {
			// The statement's own guards refused. The one steady-state refusal is
			// the designated primary of a two-member fleet; anything else (the
			// target vanished, or a concurrent add grew the roster past two) means
			// the roster moved under the operator's confirmed action, so make them
			// look again rather than guess.
			var isPrimary bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM settings WHERE id = 1 AND auto_sync_primary_id = ?)`,
				id).Scan(&isPrimary); err != nil {
				return 0, nil, fmt.Errorf("frontdesk: disband primary check: %w", err)
			}
			if isPrimary {
				return DeleteRefusedPrimary, nil, nil
			}
			return 0, nil, ErrMembershipChanged
		}
		// auto_sync_gen deliberately survives the disband: members keep their
		// last-applied generation as an import fence, and a future re-formed
		// fleet must continue counting upward or its first push would look stale.
		for _, step := range []txStep{
			{"clear auto-sync on disband", `UPDATE settings SET auto_sync_enabled = 0, auto_sync_primary_id = '', auto_sync_last_hash = '' WHERE id = 1`},
			{"clear fleet sync state on disband", `DELETE FROM fleet_sync_state`},
		} {
			if err := step.exec(ctx, tx); err != nil {
				return 0, nil, err
			}
		}
		if err := commitTx(tx, "commit disband"); err != nil {
			return 0, nil, err
		}
		return DeleteDisbanded, roster, nil
	}

	// Three or more members: remove just the target. Delete only if the member
	// is NOT the fleet primary, removing it would not empty the routing pool (an
	// active member must never be the last active one: the same invariant
	// SetMemberState enforces for draining, reached here via the delete door;
	// removing a drained member is always safe), and the roster is still big
	// enough that this cannot create a single-member fleet. The sub-queries make
	// the checks and the delete a single atomic statement.
	res, err := tx.ExecContext(ctx, `
		DELETE FROM members
		WHERE id = ?
		  AND (SELECT COUNT(*) FROM members) > 2
		  AND id NOT IN (SELECT auto_sync_primary_id FROM settings WHERE id = 1)
		  AND (state != ? OR EXISTS (SELECT 1 FROM members WHERE state = ? AND id != ?))`,
		id, string(StateActive), string(StateActive), id)
	if err != nil {
		return 0, nil, fmt.Errorf("frontdesk: delete member: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil, err
	}
	if n == 0 {
		// Not deleted: the member is the primary, the roster shrank to two under
		// this call (a concurrent removal, where retrying would DISBAND, a
		// materially different action than the one the operator confirmed), or it
		// is the last active member. Disambiguate so the server returns the right
		// 409. The failed DELETE already ran as a write statement, so these
		// re-reads see the roster the guards saw.
		var isPrimary, exists bool
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM settings WHERE id = 1 AND auto_sync_primary_id = ?),
			        EXISTS(SELECT 1 FROM members WHERE id = ?),
			        (SELECT COUNT(*) FROM members)`,
			id, id).Scan(&isPrimary, &exists, &count); err != nil {
			return 0, nil, fmt.Errorf("frontdesk: delete member primary check: %w", err)
		}
		if isPrimary {
			return DeleteRefusedPrimary, nil, nil
		}
		if !exists || count <= 2 {
			return 0, nil, ErrMembershipChanged
		}
		return 0, nil, ErrLastActiveMember
	}
	// A removed non-primary member must not linger as the auto-sync primary (it
	// never should, but stay defensive) nor as the stale "last run" marker.
	for _, step := range []txStep{
		{"clear auto-sync primary", `UPDATE settings SET auto_sync_primary_id = '' WHERE auto_sync_primary_id = ?`},
		{"clear ghost fleet state", `UPDATE fleet_sync_state SET primary_id = '', primary_name = '' WHERE id = 1 AND primary_id = ?`},
	} {
		if err := step.exec(ctx, tx, id); err != nil {
			return 0, nil, err
		}
	}
	if err := commitTx(tx, "commit delete member"); err != nil {
		return 0, nil, err
	}
	return DeleteApplied, []RemovedMember{target}, nil
}

// txStep is one named statement of a multi-statement transaction; the name
// keys the wrapped error so a failure says which cleanup step broke.
type txStep struct{ what, query string }

func (s txStep) exec(ctx context.Context, tx *sql.Tx, args ...any) error {
	if _, err := tx.ExecContext(ctx, s.query, args...); err != nil {
		return fmt.Errorf("frontdesk: %s: %w", s.what, err)
	}
	return nil
}

// commitTx commits and wraps the error under the caller's step name.
func commitTx(tx *sql.Tx, what string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("frontdesk: %s: %w", what, err)
	}
	return nil
}

// rosterSnapshot reads every member's id and name inside the caller's
// transaction, so disband events and state cleanup describe exactly the rows
// the delete saw.
func rosterSnapshot(ctx context.Context, tx *sql.Tx) ([]RemovedMember, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM members ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: read member roster: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RemovedMember
	for rows.Next() {
		var m RemovedMember
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, fmt.Errorf("frontdesk: scan member roster: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberToken decrypts and returns a member's stored admin token. ok is false
// when no token is stored for the member.
//
// It decrypts through the shared key cache: the background loops (auto-sync,
// health poller, announce) read every member's token every tick, and an uncached
// read is an Argon2id derivation each time. A stale token cannot be served, since
// the cache is keyed on the stored ciphertext, nonce and salt, and SetMemberToken
// re-encrypts under a fresh random salt. A cleared token returns before any
// decryption, and one that fails to decrypt is never cached.
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
	plain, err := auth.DecryptCached(cipher, nonce, salt, s.masterKey)
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
