package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// This file implements GET /api/fleet/status, the single probe that powers the
// step-gated fleet-sync wizard. Relative to a chosen primary it reports, per
// member: reachability, whether its dashboard admin token already matches the
// primary's, whether it can decrypt the primary's provider keys (the MASTER_KEY
// match), and the config diff it would receive. The wizard gates each step on
// these fields, so it never has to call several endpoints or guess.
//
// Unlike the per-action endpoints (admin-token sync, config sync), fleetStatus
// NEVER returns a transport error for a single bad member: an unreachable or
// wrong-token member is marked reachable:false with a human note and the rest
// still report. Only a primary that cannot be used as a source short-circuits,
// and even then it reports inline (primary_reachable:false) rather than 502, so
// the wizard can explain the problem instead of flashing a generic toast.

// fleetMemberStatus is one member's convergence state against the primary.
type fleetMemberStatus struct {
	MemberID          string `json:"member_id"`
	Name              string `json:"name"`
	Reachable         bool   `json:"reachable"`
	HasToken          bool   `json:"has_token"`
	AdminTokenMatches bool   `json:"admin_token_matches"`
	// MasterKeyMatches is nil when MASTER_KEY was not evaluated: a keyless fleet
	// (nothing to verify) or a member that could not be probed. A non-nil false
	// means a real mismatch that blocks config sync.
	MasterKeyMatches *bool  `json:"master_key_matches"`
	SchemaOK         bool   `json:"schema_ok"`
	Added            int    `json:"added"`
	Updated          int    `json:"updated"`
	Removed          int    `json:"removed"`
	Note             string `json:"note,omitempty"`
}

// fleetStatusResponse is the body of GET /api/fleet/status.
type fleetStatusResponse struct {
	PrimaryID        string              `json:"primary_id"`
	PrimaryReachable bool                `json:"primary_reachable"`
	PrimaryNote      string              `json:"primary_note,omitempty"`
	Members          []fleetMemberStatus `json:"members"`
	// LBPort is the host port the load balancer (Traefik "web" entrypoint) is
	// published on (LB_PORT in the HA .env). The wizard's final step pairs it with
	// the browser's hostname to show the operator exactly where to send /v1
	// traffic. Empty when Front Desk was not told the port.
	LBPort string `json:"lb_port,omitempty"`
}

func (s *Server) fleetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	primaryID := r.URL.Query().Get("primary")

	primary, primaryToken, err := s.memberTokenOrErr(ctx, primaryID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, err) // an unknown/stale id is a real 404
			return
		}
		// The chosen primary has no stored admin token, so it cannot be a source.
		// Report it inline rather than failing the request.
		writeJSON(w, http.StatusOK, fleetStatusResponse{
			PrimaryID:   primaryID,
			PrimaryNote: "no stored admin token for this member; add it on the Members tab",
			Members:     []fleetMemberStatus{},
			LBPort:      s.lbPort,
		})
		return
	}

	// Probe the primary's hash and export concurrently: they are independent
	// round-trips to the same member, so serial calls double the latency on a slow
	// link and the wizard polls this on an interval.
	var (
		primaryHash string
		export      []byte
		herr, eerr  error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); primaryHash, herr = s.fetchMemberHash(ctx, primary, primaryToken) }()
	go func() { defer wg.Done(); export, eerr = s.fetchMemberExport(ctx, primary, primaryToken) }()
	wg.Wait()
	if herr != nil || eerr != nil {
		cause := herr
		if cause == nil {
			cause = eerr
		}
		// Surface the real reason (a status code points at a wrong stored token or
		// an out-of-date member; a dial error points at the URL) instead of a
		// generic "something went wrong".
		writeJSON(w, http.StatusOK, fleetStatusResponse{
			PrimaryID:   primary.ID,
			PrimaryNote: fmt.Sprintf("could not read this member's admin API: %v. Check its URL and that its stored admin token is current.", cause),
			Members:     []fleetMemberStatus{},
			LBPort:      s.lbPort,
		})
		return
	}
	// A keyless primary has nothing to verify for MASTER_KEY: the member-side
	// canary trivially passes (see canDecryptSample in internal/api/configsync.go),
	// so reporting "matches" would mislead. Parse just enough of the export to count
	// providers that actually carry an encrypted key, rather than scanning raw bytes
	// for the literal "encrypted_key" (which would misfire if any string value, such
	// as a description, ever contained that text).
	var exportShape struct {
		Config struct {
			Providers []struct {
				EncryptedKey string `json:"encrypted_key"`
			} `json:"providers"`
		} `json:"config"`
	}
	_ = json.Unmarshal(export, &exportShape)
	keyless := true
	for _, p := range exportShape.Config.Providers {
		if p.EncryptedKey != "" {
			keyless = false
			break
		}
	}

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		writeError(w, err)
		return
	}

	items := make([]fleetMemberStatus, 0, len(members))
	for _, m := range members {
		items = append(items, s.fleetStatusForMember(ctx, m, primary.ID, primaryHash, export, keyless))
	}
	writeJSON(w, http.StatusOK, fleetStatusResponse{
		PrimaryID: primary.ID, PrimaryReachable: true, Members: items, LBPort: s.lbPort,
	})
}

// fleetLastSync reports the last successful fleet-sync wizard run (timestamp +
// the primary it converged onto), so the wizard can show it has run before
// rather than looking untouched after a container rebuild. It returns 204 when
// the wizard has never recorded a successful run.
func (s *Server) fleetLastSync(w http.ResponseWriter, r *http.Request) {
	state, found, err := s.store.GetFleetSyncState(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// fleetStatusForMember probes one member relative to the primary. It is total:
// every failure path produces a populated item with a note, never an error.
func (s *Server) fleetStatusForMember(ctx context.Context, m *Member, primaryID, primaryHash string, export []byte, keyless bool) fleetMemberStatus {
	item := fleetMemberStatus{MemberID: m.ID, Name: m.Name, HasToken: m.HasToken}

	// The primary is the source of truth: it matches itself by definition and is
	// never written to.
	if m.ID == primaryID {
		item.Reachable = true
		item.AdminTokenMatches = true
		item.SchemaOK = true
		if !keyless {
			ok := true
			item.MasterKeyMatches = &ok
		}
		item.Note = "primary (source of truth)"
		return item
	}
	if !m.HasToken {
		item.Note = "no stored admin token; add it on the Members tab"
		return item
	}
	token, ok, err := s.store.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		item.Note = "no stored admin token; add it on the Members tab"
		return item
	}

	// Admin-token match is computed with the member's CURRENT token (the one Front
	// Desk has stored), so it works before any sync has run.
	hash, err := s.fetchMemberHash(ctx, m, token)
	if err != nil {
		item.Note = "could not reach this member"
		return item
	}
	item.Reachable = true
	item.AdminTokenMatches = hash == primaryHash

	res, err := s.pushMemberImport(ctx, m, token, export, true) // dry run
	if err != nil {
		// The import probe failed (5xx, a drop between the hash and import calls,
		// etc.). We could not confirm the schema is bad, so leave SchemaOK true:
		// a zero-value false would wrongly land this member in the wizard's schema
		// blockers and show a spurious "too old, upgrade it" remedy. The note
		// explains the partial probe instead.
		item.SchemaOK = true
		item.Note = "reachable, but could not read its config diff"
		return item
	}
	// Schema is checked before MASTER_KEY: a 422 short-circuits the member before
	// it runs the decrypt canary (see the member-side Import in
	// internal/api/configsync.go), leaving master_key_ok an unevaluated false. So
	// report a version skew on its own, never as a key mismatch.
	item.SchemaOK = res.SchemaVersionOK
	if !res.SchemaVersionOK {
		item.Note = "this member's app version is too old to sync with the primary"
		return item
	}
	if keyless {
		item.Note = "no provider keys to verify yet"
	} else {
		mk := res.MasterKeyOK
		item.MasterKeyMatches = &mk
		if !mk {
			item.Note = "MASTER_KEY does not match the primary"
			return item
		}
	}
	item.Added, item.Updated, item.Removed = res.Diff.counts()
	return item
}
