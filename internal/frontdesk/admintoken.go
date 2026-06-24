package frontdesk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file implements the admin-token sync and reset flows (Section 5 of the
// HA plan): converging every member's dashboard admin token onto one primary's,
// and minting a brand-new group token. Both are deliberately explicit, primary-
// /Front-Desk-driven, and destructive; the UI double-confirms before calling
// them. Only the sha256:<hex> HASH is ever pushed to a member (via its
// POST /api/admin/token-hash endpoint); a plaintext token transits to members
// only as the user-facing reveal in reset, never in logs.

const (
	memberTokenHashPath = "/api/admin/token-hash"
	// groupTokenBytes is the entropy of a generated group admin token; hex-
	// encoded it is 32 chars, matching internal/admin's token length.
	groupTokenBytes = 16
)

// dispositions for the sync preview.
const (
	dispOverwrite = "overwrite"
	dispMatches   = "matches"
	dispBlocked   = "blocked"
)

type syncPreviewItem struct {
	MemberID    string `json:"member_id"`
	Name        string `json:"name"`
	Disposition string `json:"disposition"`
}

type syncResultItem struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// memberHashResponse is the shape of a member's GET /api/admin/token-hash.
type memberHashResponse struct {
	Hash string `json:"hash"`
}

// adminTokenPreview compares every member's current admin-token hash against the
// chosen primary's, so the UI can show, by name, who will be overwritten, who
// already matches, and who is blocked (no stored token) before anything is
// written. No hashes are returned to the client, only dispositions.
func (s *Server) adminTokenPreview(w http.ResponseWriter, r *http.Request) {
	primaryID := r.URL.Query().Get("primary")
	primary, primaryToken, err := s.memberTokenOrErr(r.Context(), primaryID)
	if err != nil {
		writeError(w, err)
		return
	}
	primaryHash, err := s.fetchMemberHash(r.Context(), primary, primaryToken)
	if err != nil {
		http.Error(w, "could not read the primary's admin token hash", http.StatusBadGateway)
		return
	}

	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	items := make([]syncPreviewItem, 0, len(members))
	for _, m := range members {
		items = append(items, syncPreviewItem{
			MemberID:    m.ID,
			Name:        m.Name,
			Disposition: s.previewDisposition(r.Context(), m, primary.ID, primaryHash),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"primary_id": primary.ID, "items": items})
}

// previewDisposition classifies one member relative to the primary. A member
// whose hash can't be read is treated as "overwrite" (unknown, so the sync will
// attempt it and report the per-member outcome) rather than silently "matches".
func (s *Server) previewDisposition(ctx context.Context, m *Member, primaryID, primaryHash string) string {
	if m.ID == primaryID {
		return dispMatches // the source itself
	}
	if !m.HasToken {
		return dispBlocked
	}
	token, ok, err := s.store.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		return dispBlocked
	}
	hash, err := s.fetchMemberHash(ctx, m, token)
	if err != nil {
		return dispOverwrite
	}
	if hash == primaryHash {
		return dispMatches
	}
	return dispOverwrite
}

// adminTokenSync overwrites the admin-token hash on every non-primary member
// whose hash differs from the primary's, then updates Front Desk's own stored
// token for each synced member to the primary's so it can keep calling them. The
// primary's stored token is its plaintext, which is exactly the value members
// converge on. Each member is independent: a failed push leaves that member
// untouched.
func (s *Server) adminTokenSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrimaryID string `json:"primary_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	primary, primaryToken, err := s.memberTokenOrErr(r.Context(), req.PrimaryID)
	if err != nil {
		writeError(w, err)
		return
	}
	primaryHash, err := s.fetchMemberHash(r.Context(), primary, primaryToken)
	if err != nil {
		http.Error(w, "could not read the primary's admin token hash", http.StatusBadGateway)
		return
	}

	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	results := make([]syncResultItem, 0)
	for _, m := range members {
		if m.ID == primary.ID || !m.HasToken {
			continue // the source, and token-less members (flagged in preview), are skipped
		}
		token, ok, err := s.store.MemberToken(r.Context(), m.ID)
		if err != nil || !ok {
			continue
		}
		// Skip members already on the primary's hash.
		if cur, err := s.fetchMemberHash(r.Context(), m, token); err == nil && cur == primaryHash {
			continue
		}
		results = append(results, s.applyTokenHash(r.Context(), m, token, primaryHash, primaryToken, "admin_token.synced"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// adminTokenReset mints a new group admin token, pushes its hash to every member
// Front Desk can authenticate to, updates the stored tokens, and returns the new
// plaintext exactly once. Members with no stored token are reported as skipped
// (they keep their old token). The plaintext is never logged.
func (s *Server) adminTokenReset(w http.ResponseWriter, r *http.Request) {
	plaintext, hash, err := generateGroupToken()
	if err != nil {
		debuglog.Error("frontdesk: generate group token", "error", err)
		http.Error(w, "failed to generate a new token", http.StatusInternalServerError)
		return
	}

	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	results := make([]syncResultItem, 0, len(members))
	for _, m := range members {
		if !m.HasToken {
			results = append(results, syncResultItem{MemberID: m.ID, Name: m.Name, OK: false, Error: "no stored admin token"})
			continue
		}
		token, ok, err := s.store.MemberToken(r.Context(), m.ID)
		if err != nil || !ok {
			results = append(results, syncResultItem{MemberID: m.ID, Name: m.Name, OK: false, Error: "no stored admin token"})
			continue
		}
		results = append(results, s.applyTokenHash(r.Context(), m, token, hash, plaintext, "admin_token.reset"))
	}
	// The body carries the new plaintext token once: keep it out of any
	// intermediary or browser cache.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": plaintext, "results": results})
}

// applyTokenHash pushes newHash to one member (authenticating with its current
// token), and on success updates Front Desk's stored token for that member to
// newPlaintext and emits an audit event. The event records only the member and
// outcome, never any token material.
func (s *Server) applyTokenHash(ctx context.Context, m *Member, currentToken, newHash, newPlaintext, eventType string) syncResultItem {
	res := syncResultItem{MemberID: m.ID, Name: m.Name}
	if err := s.pushMemberHash(ctx, m, currentToken, newHash); err != nil {
		debuglog.Warn("frontdesk: push admin token hash failed", "member", m.Name, "error", err)
		res.Error = "could not update this member"
		s.emit(ctx, Event{
			Type: eventType + "_failed", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("Failed to update admin token on %s", m.Name), MemberID: m.ID,
		})
		return res
	}
	if err := s.store.SetMemberToken(ctx, m.ID, newPlaintext); err != nil {
		// The member already accepted the new token, but Front Desk could not
		// persist it: its stored token is now stale and it can no longer call
		// that member. This is an operator-visible problem, so report it as a
		// failure with a clear remedy rather than a silent success.
		debuglog.Warn("frontdesk: store synced member token failed", "member", m.Name, "error", err)
		res.Error = "token updated on the member, but Front Desk could not save it; re-enter this member's token on the Members tab"
		s.emit(ctx, Event{
			Type: eventType + "_failed", Severity: "warning", Source: "frontdesk",
			Message: fmt.Sprintf("Admin token changed on %s but Front Desk could not store it", m.Name), MemberID: m.ID,
		})
		return res
	}
	res.OK = true
	s.emit(ctx, Event{
		Type: eventType, Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Admin token updated on %s", m.Name), MemberID: m.ID,
	})
	return res
}

// fetchMemberHash reads a member's current admin-token hash (sha256:<hex>).
func (s *Server) fetchMemberHash(ctx context.Context, m *Member, token string) (string, error) {
	status, body, err := s.callMember(ctx, http.MethodGet, m.URL, memberTokenHashPath, token, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("member token-hash GET returned %d", status)
	}
	var resp memberHashResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", errors.New("frontdesk: parse member token-hash response")
	}
	return resp.Hash, nil
}

// pushMemberHash overwrites a member's admin-token hash.
func (s *Server) pushMemberHash(ctx context.Context, m *Member, token, hash string) error {
	payload, err := json.Marshal(memberHashResponse{Hash: hash})
	if err != nil {
		return err
	}
	status, _, err := s.callMember(ctx, http.MethodPost, m.URL, memberTokenHashPath, token, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("member token-hash POST returned %d", status)
	}
	return nil
}

// generateGroupToken returns a new random plaintext admin token and its
// sha256:<hex> hash, matching internal/admin's hash format so members validate
// it the same way.
func generateGroupToken() (plaintext, hash string, err error) {
	buf := make([]byte, groupTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(buf) // 32 hex chars
	sum := sha256.Sum256([]byte(plaintext))
	hash = "sha256:" + hex.EncodeToString(sum[:])
	return plaintext, hash, nil
}
