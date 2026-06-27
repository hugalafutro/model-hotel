package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// This file implements GET /api/fleet/status, the single probe that powers the
// step-gated fleet-sync wizard. Relative to a chosen primary it reports, per
// member: reachability, whether it can decrypt the primary's provider keys (the
// MASTER_KEY match), and the config diff it would receive. The wizard gates each
// step on these fields, so it never has to call several endpoints or guess.
//
// Unlike config sync, fleetStatus NEVER returns a transport error for a single
// bad member: an unreachable or wrong-token member is marked reachable:false with
// a human note and the rest still report. Only a primary that cannot be used as a
// source short-circuits, and even then it reports inline (primary_reachable:false)
// rather than 502, so the wizard can explain the problem instead of a generic toast.

// fleetMemberStatus is one member's convergence state against the primary.
type fleetMemberStatus struct {
	MemberID  string `json:"member_id"`
	Name      string `json:"name"`
	Reachable bool   `json:"reachable"`
	HasToken  bool   `json:"has_token"`
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

	// Pull the primary's config export; it is the source the diff is computed
	// against and the canary for the MASTER_KEY check.
	export, err := s.fetchMemberExport(ctx, primary, primaryToken)
	if err != nil {
		// Surface the real reason (a status code points at a wrong stored token or
		// an out-of-date member; a dial error points at the URL) instead of a
		// generic "something went wrong".
		writeJSON(w, http.StatusOK, fleetStatusResponse{
			PrimaryID:   primary.ID,
			PrimaryNote: fmt.Sprintf("could not read this member's admin API: %v. Check its URL and that its stored admin token is current.", err),
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
			VirtualKeys []json.RawMessage `json:"virtual_keys"`
			Settings    map[string]string `json:"settings"`
		} `json:"config"`
	}
	_ = json.Unmarshal(export, &exportShape)

	// An export with no providers, virtual keys, or settings is one every member
	// will refuse (the member-side Import returns 400 rather than wipe itself
	// clean). Detect it here and report it once as a primary-level problem,
	// instead of probing every peer with a config they all reject, which would
	// otherwise paint the whole reachable fleet as "offline".
	if len(exportShape.Config.Providers) == 0 &&
		len(exportShape.Config.VirtualKeys) == 0 &&
		len(exportShape.Config.Settings) == 0 {
		writeJSON(w, http.StatusOK, fleetStatusResponse{
			PrimaryID:   primary.ID,
			PrimaryNote: "this primary has no providers, virtual keys, or settings to sync yet. Configure it first, then re-run the wizard.",
			Members:     []fleetMemberStatus{},
			LBPort:      s.lbPort,
		})
		return
	}

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
		items = append(items, s.fleetStatusForMember(ctx, m, primary.ID, export, keyless))
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
func (s *Server) fleetStatusForMember(ctx context.Context, m *Member, primaryID string, export []byte, keyless bool) fleetMemberStatus {
	item := fleetMemberStatus{MemberID: m.ID, Name: m.Name, HasToken: m.HasToken}

	// The primary is the source of truth: it matches itself by definition and is
	// never written to.
	if m.ID == primaryID {
		item.Reachable = true
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
	// The dry-run import is the single probe: it doubles as the reachability check
	// (a transport error or unexpected status means we cannot use this member as a
	// sync target) and the source of the schema/MASTER_KEY/diff fields below. A 409
	// or 422 is parsed into res, not returned as an error.
	res, status, err := s.pushMemberImport(ctx, m, token, export, true) // dry run
	if err != nil {
		// status == 0 is a real transport failure (the member never answered).
		// A non-zero status means the member answered with a code we do not treat
		// as a convergence disposition (e.g. 401/403 wrong token, 500): report the
		// real cause rather than a blanket "offline" that hides a fixable blocker.
		switch status {
		case 0:
			item.Note = "could not reach this member"
		case http.StatusUnauthorized, http.StatusForbidden:
			item.Note = fmt.Sprintf("this member rejected the stored admin token (HTTP %d); update it on the Members tab", status)
		default:
			item.Note = fmt.Sprintf("this member rejected the config request (HTTP %d)", status)
		}
		return item
	}
	item.Reachable = true
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
