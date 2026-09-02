package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The Members tab's circuits column: which member is dark for which model. Each
// member's breaker is local runtime state, so Front Desk reads every member's
// own ledger (the ?detail=1 status the dashboard card reads) every three health
// polls, 15s at the defaults, and keeps only what the column shows. Not the
// health cadence itself: the member caches that status for 5s, so a 5s poll
// would find the cache just expired every time and recompute the ledger, two
// catalog queries and a breaker walk, on members with no dashboard open.
// Read-only; the fleet reset (server_circuits.go) is the write side.

// memberCircuitsPath is the member endpoint the poll reads: one row per
// provider with its circuits, each carrying its state and last cause.
const memberCircuitsPath = "/api/failover-groups/circuit-breaker-status?detail=1"

// MemberCircuits is one member's ledger reduced to the circuits that are not
// closed, in the member's own order (providers by id, models by name). Open
// holds at most maxOpenCircuits of them; Total is how many there are, so the
// column's count is exact even when the hover list is cut.
type MemberCircuits struct {
	CheckedAt time.Time     `json:"checked_at"`
	Open      []OpenCircuit `json:"open"`
	Total     int           `json:"total"`
}

// maxOpenCircuits bounds the ledger a member status carries. The count is the
// load-bearing number; the list is a hover aid. /api/members is refetched every
// few seconds by every open tab, so a provider-wide outage across a large
// catalog must not turn each refetch into hundreds of rows per member.
const maxOpenCircuits = 50

// maxCircuitStatusBytes bounds the member's status response. The detail
// response grows with the catalog (closed circuits ride in it too), so a
// member past the bound is reported as such rather than as unparsable.
const maxCircuitStatusBytes = 1 << 20

// OpenCircuit is one circuit the breaker holds open or owes a probe: the
// provider and model it keys on, its state, why it was last judged and when
// it is next eligible. Pinned says a quota pin governs the wait.
type OpenCircuit struct {
	ProviderID  string `json:"provider_id"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model"`
	State       string `json:"state"`
	Cause       string `json:"cause,omitempty"`
	Status      int    `json:"status,omitempty"`
	NextRetryAt string `json:"next_retry_at,omitempty"`
	QuotaPinned bool   `json:"quota_pinned,omitempty"`
	PinSource   string `json:"pin_source,omitempty"`
}

// PollCircuitsOnce reads each tokened member's circuit ledger and stores the
// non-closed circuits on its status. A member without a token, or one whose read
// failed, shows no ledger at all rather than the last one it had: a stale "1
// open" would send the operator to a member that has recovered, and a stale
// "none" would hide the one that has not. The UI is refreshed only when the set
// changes, so a quiet fleet produces no events.
func (p *Poller) PollCircuitsOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	for _, m := range members {
		if !m.HasToken {
			// A member whose token was removed loses its ledger with it: the
			// last read would otherwise stand in the column indefinitely.
			if p.clearCircuits(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			if p.clearCircuits(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		ledger, err := p.fetchMemberCircuits(ctx, m.URL, token)
		if err != nil {
			// Health polling already alerts on an unreachable member; a failed
			// ledger read is only noise beside that, so it is logged at Debug.
			// The one exception is a response past the size bound, which health
			// cannot see and which would otherwise blank the column for good.
			if errors.Is(err, errCircuitStatusTooLarge) {
				debuglog.Warn("frontdesk: member circuit status too large to read", "member", m.Name, "limit_bytes", maxCircuitStatusBytes)
			} else {
				debuglog.Debug("frontdesk: fetch member circuits", "member", m.Name, "error", err)
			}
			if p.clearCircuits(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		p.mu.Lock()
		cur := p.statuses[m.ID]
		changed := cur.Circuits == nil || cur.Circuits.Total != ledger.Total || circuitsKey(cur.Circuits.Open) != circuitsKey(ledger.Open)
		ledger.CheckedAt = p.now()
		cur.Circuits = ledger
		p.statuses[m.ID] = cur
		p.mu.Unlock()
		if changed {
			p.publishMemberStatus(m.ID)
		}
	}
}

// clearCircuits drops a member's ledger and reports whether there was one to
// drop, so the caller publishes a refresh only when the column changes.
func (p *Poller) clearCircuits(memberID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.statuses[memberID]
	had := cur.Circuits != nil
	cur.Circuits = nil
	p.statuses[memberID] = cur
	return had
}

// errCircuitStatusTooLarge is a member response past maxCircuitStatusBytes:
// kept apart from a parse failure because it names a different problem, and
// one that persists until the member's catalog shrinks.
var errCircuitStatusTooLarge = errors.New("frontdesk: circuit status response exceeds the size bound")

// circuitsKey is the identity of a ledger for change detection: which circuits
// are not closed, in which state and for which cause. The retry instant and
// the check time are deliberately outside it, or every poll would publish.
// The separators are a convention, not an invariant: ids are UUIDs and causes
// are the breaker's fixed phrases, none of which carry '|' or a newline, and a
// collision would only cost one missed refresh, which the next poll repairs.
func circuitsKey(open []OpenCircuit) string {
	var b strings.Builder
	for _, c := range open {
		b.WriteString(c.ProviderID)
		b.WriteByte('|')
		b.WriteString(c.Model)
		b.WriteByte('|')
		b.WriteString(c.State)
		b.WriteByte('|')
		b.WriteString(c.Cause)
		b.WriteByte('\n')
	}
	return b.String()
}

// memberCircuitStatus is the member's status response reduced to what the
// column keeps. Field names are the member's JSON.
type memberCircuitStatus struct {
	Providers []struct {
		ProviderID   string `json:"provider_id"`
		ProviderName string `json:"provider_name"`
		Circuits     []struct {
			Model       string `json:"model"`
			State       string `json:"state"`
			NextRetryAt string `json:"next_retry_at"`
			QuotaPinned bool   `json:"quota_pinned"`
			PinSource   string `json:"pin_source"`
			LastCause   string `json:"last_cause"`
			LastStatus  int    `json:"last_status"`
		} `json:"circuits"`
	} `json:"providers"`
}

// fetchMemberCircuits reads the member's detailed circuit status and returns
// the circuits that are not closed, the first maxOpenCircuits of them listed
// and all of them counted. A member too old to report circuits[] yields an
// empty ledger, which the column shows as none open: its row-level state is
// provider-wide and would attribute one model's outage to every model of the
// provider.
func (p *Poller) fetchMemberCircuits(ctx context.Context, baseURL, token string) (*MemberCircuits, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+memberCircuitsPath, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("circuit status api returned %d", resp.StatusCode)
	}
	// One byte past the bound tells a body that is too large from one that
	// fits exactly; a silently truncated body would only ever parse as garbage.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCircuitStatusBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCircuitStatusBytes {
		return nil, errCircuitStatusTooLarge
	}
	var payload memberCircuitStatus
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not wrapped: the decoder error can echo a fragment of the response.
		return nil, errors.New("frontdesk: parse circuit status response")
	}
	ledger := &MemberCircuits{Open: []OpenCircuit{}}
	for _, prov := range payload.Providers {
		for _, c := range prov.Circuits {
			if c.State == "closed" {
				continue
			}
			ledger.Total++
			if len(ledger.Open) == maxOpenCircuits {
				continue
			}
			ledger.Open = append(ledger.Open, OpenCircuit{
				ProviderID: prov.ProviderID, Provider: prov.ProviderName, Model: c.Model, State: c.State,
				Cause: c.LastCause, Status: c.LastStatus, NextRetryAt: c.NextRetryAt,
				QuotaPinned: c.QuotaPinned, PinSource: c.PinSource,
			})
		}
	}
	return ledger, nil
}
