package frontdesk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// This file holds the helpers Front Desk uses to call a member's own
// admin-authenticated API (its stats time series for the Traffic tab, the
// config import/export relays, and the add/edit-time token check). Every call
// goes through the guarded probe client (netguard.go): a dial-time block on metadata/
// link-local addresses and a refusal of cross-host redirects, so a member URL
// pointed somewhere hostile cannot bounce the request (carrying the member's
// admin Bearer token) to another host.

// maxMemberRespBody bounds a member admin-API response read into memory.
const maxMemberRespBody = 1 << 20

// callMember performs an admin-authenticated request to a member's API and
// returns the response status and body (capped). token is sent as the Bearer
// credential; body may be nil. The caller maps status to its own result.
func (s *Server) callMember(ctx context.Context, method, baseURL, path, token string, body io.Reader) (int, []byte, error) {
	return s.callMemberWith(ctx, s.probe, method, baseURL, path, token, body)
}

// callMemberWith is callMember with an explicit client, so a heavyweight call
// (config import, which triggers member-side model discovery) can use a client
// with a longer deadline than the fast health-probe client without that probe
// timeout mislabeling a slow-but-successful import as "could not reach". Extra
// request headers (e.g. the fleet source-generation fence) may be passed as
// (name, value) pairs; an empty value is skipped.
func (s *Server) callMemberWith(ctx context.Context, client *http.Client, method, baseURL, path, token string, body io.Reader, headers ...[2]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, hd := range headers {
		if hd[1] != "" {
			req.Header.Set(hd[0], hd[1])
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMemberRespBody))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// memberProbeTimeout bounds the add/edit-time token check so a hung or slow
// member cannot stall the request; an exceeded deadline is treated as "could
// not reach" (a warning), never as a wrong token.
const memberProbeTimeout = 6 * time.Second

// memberReadTimeout bounds an interactive member admin read (currently the
// Traffic tab's stats timeseries). It is more generous than the health probe
// because a member aggregating an hour of buckets can legitimately take longer
// than a liveness check, and timing out there mislabels a slow-but-successful
// read as "could not read metrics"; it stays well under the import relay's
// deadline so a hung member still fails the Traffic card quickly.
const memberReadTimeout = 15 * time.Second

// memberSyncTimeout bounds a config import relay. Unlike a health probe, an
// import runs model discovery on the member (live upstream calls), which easily
// exceeds the 4s probe timeout, so the import client is given a far more generous
// deadline; it is still capped so a hung member cannot stall the sync forever.
const memberSyncTimeout = 120 * time.Second

// memberBackupTimeout bounds the pre-sync backup relay the auto-syncer makes
// before overwriting a member. It must exceed the member's own pg_dump budget
// (10 minutes, internal/api.backup.go) or a large member would time out every
// tick and leave an orphaned dump holding the member's backup lock. One extra
// minute covers the round trip and dump teardown.
const memberBackupTimeout = 11 * time.Minute

// tokenProbe is the outcome of checking a member's admin token at add/edit time,
// before the background poller would otherwise catch a mistake.
type tokenProbe struct {
	reached bool // the member returned an HTTP response at all
	valid   bool // the settings probe returned 200, i.e. the token was accepted
	status  int  // HTTP status when reached
}

// rejected reports a token the member positively refused (401/403): a definite
// mistake worth blocking the save for. A 404/5xx is reached-but-unverified and
// only warns (e.g. an older member with an unexpected settings response).
func (p tokenProbe) rejected() bool {
	return p.reached && (p.status == http.StatusUnauthorized || p.status == http.StatusForbidden)
}

// warning returns a human note when the token could not be confirmed but the
// save is still allowed to proceed (member offline, or reached without a 200).
// Empty when the token was accepted.
func (p tokenProbe) warning() string {
	switch {
	case p.valid:
		return ""
	case !p.reached:
		return "Saved, but Front Desk could not reach this member to verify the token yet."
	default:
		return fmt.Sprintf("Saved, but this member did not accept the token (HTTP %d); it may be an older build.", p.status)
	}
}

// probeMemberToken checks whether url accepts token right now by calling a
// lightweight admin-authenticated endpoint (settings). It never returns an
// error: a transport failure becomes reached:false so the caller can warn
// instead of blocking an add for a host that is merely offline.
func (s *Server) probeMemberToken(ctx context.Context, url, token string) tokenProbe {
	ctx, cancel := context.WithTimeout(ctx, memberProbeTimeout)
	defer cancel()
	status, _, err := s.callMember(ctx, http.MethodGet, url, memberSettingsPath, token, nil)
	if err != nil {
		return tokenProbe{}
	}
	return tokenProbe{reached: true, valid: status == http.StatusOK, status: status}
}

// memberTokenOrErr loads a member and its decrypted admin token, returning a
// typed error the caller can map: ErrNotFound when the member is gone, and a
// sentinel-wrapped ErrValidation when no token is stored.
func (s *Server) memberTokenOrErr(ctx context.Context, id string) (*Member, string, error) {
	m, err := s.store.GetMember(ctx, id)
	if err != nil {
		return nil, "", err
	}
	token, ok, err := s.store.MemberToken(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return m, "", fmt.Errorf("%w: no stored admin token for this member", ErrValidation)
	}
	return m, token, nil
}
