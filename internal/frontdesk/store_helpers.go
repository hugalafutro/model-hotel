package frontdesk

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func scanMember(sc scanner) (*Member, error) {
	var (
		m          Member
		state      string
		cipher     []byte
		createdAt  int64
		updatedAt  int64
		lastSyncAt sql.NullInt64
		syncReason string
	)
	if err := sc.Scan(&m.ID, &m.Name, &m.URL, &state, &cipher, &createdAt, &updatedAt, &lastSyncAt, &syncReason, &m.InstanceID); err != nil {
		return nil, err
	}
	m.State = MemberState(state)
	m.HasToken = len(cipher) > 0
	m.CreatedAt = time.Unix(0, createdAt).UTC()
	m.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if lastSyncAt.Valid {
		t := time.Unix(0, lastSyncAt.Int64).UTC()
		m.LastConfigSyncAt = &t
	}
	m.LastConfigSyncReason = syncReason
	return &m, nil
}

func scanEvent(sc scanner) (Event, error) {
	var (
		e         Event
		metaJSON  *string
		memberID  *string
		createdAt int64
	)
	if err := sc.Scan(&e.ID, &e.Type, &e.Severity, &e.Source, &e.Message, &metaJSON, &memberID, &createdAt); err != nil {
		return Event{}, err
	}
	if metaJSON != nil && *metaJSON != "" {
		if err := json.Unmarshal([]byte(*metaJSON), &e.Metadata); err != nil {
			return Event{}, fmt.Errorf("frontdesk: unmarshal event metadata: %w", err)
		}
	}
	if memberID != nil {
		e.MemberID = *memberID
	}
	e.CreatedAt = time.Unix(0, createdAt).UTC()
	return e, nil
}

// normalizeMemberURL validates and canonicalizes a member base URL. When
// allowHTTP is false, plain-http URLs are rejected so the member admin token is
// never transmitted in the clear.
func normalizeMemberURL(raw string, allowHTTP bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: url is required", ErrValidation)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: url is not valid: %w", ErrValidation, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: url must use http or https", ErrValidation)
	}
	if u.Scheme == "http" && !allowHTTP {
		return "", fmt.Errorf("%w; set FRONTDESK_ALLOW_HTTP_MEMBERS=true to allow plain http on a trusted internal network", ErrInsecureURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: url must include a host", ErrValidation)
	}
	// Reject a literal IP that is a known SSRF target (link-local, including the
	// cloud-metadata endpoint, or the unspecified address) at add time for a
	// clear error. Hostnames that resolve to such an address are caught later at
	// dial time by the poller's guarded client (see netguard.go).
	if ip := net.ParseIP(u.Hostname()); ip != nil && isProbeBlockedIP(ip) {
		return "", fmt.Errorf("%w: url host %s is not an allowed address", ErrValidation, u.Hostname())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func affectedOrNotFound(res sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("frontdesk: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite reports constraint failures in the error text.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
