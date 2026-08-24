package frontdesk

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
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
		// Strip userinfo from a stored url on the way out: event reads are
		// monitor-readable, and a row written before normalizeMemberURL began
		// rejecting userinfo (e.g. from a restored backup) can still carry
		// credentials. Live emits are already stripped at write time.
		if raw, ok := e.Metadata["url"].(string); ok {
			e.Metadata["url"] = stripUserinfo(raw)
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
	// Reject embedded credentials (userinfo): the stored URL is re-emitted
	// unchanged by consumers with a wider audience than the admin who entered
	// it (the unauthenticated /traefik/config endpoint and the device-readable
	// event log), so it must never carry a secret. A member behind a
	// basic-authenticated proxy needs a dedicated credential field, not
	// userinfo in the base URL.
	if u.User != nil {
		return "", fmt.Errorf("%w: url must not contain credentials (userinfo)", ErrValidation)
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

// stripUserinfo removes embedded credentials (userinfo) from a URL before it
// is rendered to an unauthenticated or lower-privilege audience. It is a
// defensive backstop for member rows stored before normalizeMemberURL began
// rejecting userinfo; an unparseable string is returned unchanged.
func stripUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// urlUserinfoRE matches the userinfo component of a URL rendered inside free
// text (everything between "scheme://" and the last @ before the host), so
// error strings can be redacted without reconstructing the wrapped error
// chain. The class allows @ and matches greedily: net/http renders the
// username percent-decoded, so an email-style username carries a literal @
// inside the userinfo and only the last @ separates it from the host.
var urlUserinfoRE = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/?\s"]*@`)

// redactErrURL renders err for a monitor-readable field, removing any userinfo
// embedded in a URL inside the message. net/http already masks the password in
// a *url.Error it returns, but keeps the username, and a member row stored
// before normalizeMemberURL began rejecting userinfo can still carry both.
func redactErrURL(err error) string {
	return urlUserinfoRE.ReplaceAllString(err.Error(), "$1")
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
