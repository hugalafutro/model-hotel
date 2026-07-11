package frontdesk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Paired devices (Bellhop phone pairing)
// ---------------------------------------------------------------------------

// DeviceRole is the server-enforced ceiling of what a paired device may do.
type DeviceRole string

const (
	// RoleMonitor devices are read-only: members, health, traffic, events,
	// alerts, fleet status, SSE. No mutations.
	RoleMonitor DeviceRole = "monitor"
	// RoleOperator devices add the whitelisted mutating actions on top of
	// Monitor: drain/activate, trigger config sync, toggle auto-sync.
	RoleOperator DeviceRole = "operator"
)

// ValidDeviceRole reports whether r is a known role.
func ValidDeviceRole(r DeviceRole) bool {
	return r == RoleMonitor || r == RoleOperator
}

// PairedDevice is one paired phone/device. The bearer token itself is never
// stored or exposed; only its SHA-256 hash lives in the row.
type PairedDevice struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Role       DeviceRole `json:"role"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// maxDeviceLabelLen bounds the device label so a hostile pairing request can't
// store unbounded text.
const maxDeviceLabelLen = 100

// CreatePairedDevice inserts a new paired device holding tokenHash. The label
// is trimmed, defaulted, and bounded; the role must be valid.
func (s *Store) CreatePairedDevice(ctx context.Context, label, tokenHash string, role DeviceRole) (*PairedDevice, error) {
	if !ValidDeviceRole(role) {
		return nil, fmt.Errorf("%w: invalid device role %q", ErrValidation, role)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Paired device"
	}
	if len(label) > maxDeviceLabelLen {
		label = label[:maxDeviceLabelLen]
	}
	if tokenHash == "" {
		return nil, fmt.Errorf("%w: token hash is required", ErrValidation)
	}

	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO paired_devices (id, label, token_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, label, tokenHash, string(role), now.UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: insert paired device: %w", err)
	}
	return &PairedDevice{ID: id, Label: label, Role: role, CreatedAt: now}, nil
}

// ListPairedDevices returns all non-revoked devices, newest first. Revoked rows
// stay in the table as an audit trail but disappear from the list (and from the
// Paired devices UI), matching the unlink semantics.
func (s *Store) ListPairedDevices(ctx context.Context) ([]*PairedDevice, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, role, created_at, last_seen_at FROM paired_devices
		 WHERE revoked_at IS NULL ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: list paired devices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var devices []*PairedDevice
	for rows.Next() {
		d, err := scanPairedDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// DeviceByTokenHash returns the non-revoked device holding tokenHash, or
// ErrNotFound. This is the auth-path lookup: a revoked device stops
// authenticating the moment revoked_at is set.
func (s *Store) DeviceByTokenHash(ctx context.Context, tokenHash string) (*PairedDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, label, role, created_at, last_seen_at FROM paired_devices
		 WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash,
	)
	d, err := scanPairedDevice(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

// RevokePairedDevice soft-deletes a device: its token stops authenticating
// immediately, the row stays for audit. Revoking an unknown or already-revoked
// device returns ErrNotFound.
func (s *Store) RevokePairedDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE paired_devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().UnixNano(), id,
	)
	return affectedOrNotFound(res, err)
}

// TouchPairedDevice stamps last_seen_at. Best-effort from the auth path; a
// failure must never fail the request, so the caller only logs errors.
func (s *Store) TouchPairedDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE paired_devices SET last_seen_at = ? WHERE id = ?`,
		time.Now().UTC().UnixNano(), id,
	)
	return err
}

func scanPairedDevice(sc scanner) (*PairedDevice, error) {
	var (
		d         PairedDevice
		role      string
		createdAt int64
		lastSeen  sql.NullInt64
	)
	if err := sc.Scan(&d.ID, &d.Label, &role, &createdAt, &lastSeen); err != nil {
		return nil, err
	}
	d.Role = DeviceRole(role)
	d.CreatedAt = time.Unix(0, createdAt).UTC()
	if lastSeen.Valid {
		t := time.Unix(0, lastSeen.Int64).UTC()
		d.LastSeenAt = &t
	}
	return &d, nil
}
