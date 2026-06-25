package frontdesk

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// This file holds the helpers Front Desk uses to call a member's own
// admin-authenticated API (its stats time series for the Traffic tab, and its
// admin-token-hash endpoints for the sync/reset flows). Every call goes through
// the guarded probe client (netguard.go): a dial-time block on metadata/
// link-local addresses and a refusal of cross-host redirects, so a member URL
// pointed somewhere hostile cannot bounce the request (carrying the member's
// admin Bearer token) to another host.

// maxMemberRespBody bounds a member admin-API response read into memory.
const maxMemberRespBody = 1 << 20

// callMember performs an admin-authenticated request to a member's API and
// returns the response status and body (capped). token is sent as the Bearer
// credential; body may be nil. The caller maps status to its own result.
func (s *Server) callMember(ctx context.Context, method, baseURL, path, token string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.probe.Do(req)
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
