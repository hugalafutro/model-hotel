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

// The Members tab's circuits column: which member is dark for which model,
// the question that needed four terminals on 2026-08-31. Each member's
// breaker is local runtime state, so Front Desk reads every member's own
// ledger (the ?detail=1 status the dashboard card reads) on the same cadence
// as the version poll and keeps only what the column shows. Read-only; the
// fleet reset (server_circuits.go) is the write side.

// memberCircuitsPath is the member endpoint the poll reads: one row per
// provider with its circuits, each carrying its state and last cause.
const memberCircuitsPath = "/api/failover-groups/circuit-breaker-status?detail=1"

// MemberCircuits is one member's ledger reduced to the circuits that are not
// closed, in the member's own order (providers by id, models by name).
type MemberCircuits struct {
	CheckedAt time.Time     `json:"checked_at"`
	Open      []OpenCircuit `json:"open"`
}

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
// non-closed circuits on its status. A member without a token, or one whose
// read failed, shows no ledger at all rather than the last one it had: a
// stale "1 open" would send the operator to a member that has recovered, and
// a stale "none" would hide the one that has not. The UI is refreshed only
// when the set changes, so a quiet fleet produces no events.
func (p *Poller) PollCircuitsOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	for _, m := range members {
		if !m.HasToken {
			continue
		}
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue
		}
		open, err := p.fetchMemberCircuits(ctx, m.URL, token)
		if err != nil {
			// Health polling already alerts on an unreachable member; a failed
			// ledger read is only noise beside that, so it is logged at Debug.
			debuglog.Debug("frontdesk: fetch member circuits", "member", m.Name, "error", err)
			p.mu.Lock()
			cur := p.statuses[m.ID]
			had := cur.Circuits != nil
			cur.Circuits = nil
			p.statuses[m.ID] = cur
			p.mu.Unlock()
			if had {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		p.mu.Lock()
		cur := p.statuses[m.ID]
		changed := cur.Circuits == nil || circuitsKey(cur.Circuits.Open) != circuitsKey(open)
		cur.Circuits = &MemberCircuits{CheckedAt: p.now(), Open: open}
		p.statuses[m.ID] = cur
		p.mu.Unlock()
		if changed {
			p.publishMemberStatus(m.ID)
		}
	}
}

// circuitsKey is the identity of a ledger for change detection: which circuits
// are not closed, in which state and for which cause. The retry instant and
// the check time are deliberately outside it, or every poll would publish.
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
// the circuits that are not closed. A member from before circuits[] existed
// reports rows without it and so an empty ledger, which the column shows as
// none open: on such a member the row-level state is provider-wide and
// would attribute one model's outage to every model of the provider.
func (p *Poller) fetchMemberCircuits(ctx context.Context, baseURL, token string) ([]OpenCircuit, error) {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload memberCircuitStatus
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not wrapped: the decoder error can echo a fragment of the response.
		return nil, errors.New("frontdesk: parse circuit status response")
	}
	open := []OpenCircuit{}
	for _, prov := range payload.Providers {
		for _, c := range prov.Circuits {
			if c.State == "closed" {
				continue
			}
			open = append(open, OpenCircuit{
				ProviderID: prov.ProviderID, Provider: prov.ProviderName, Model: c.Model, State: c.State,
				Cause: c.LastCause, Status: c.LastStatus, NextRetryAt: c.NextRetryAt,
				QuotaPinned: c.QuotaPinned, PinSource: c.PinSource,
			})
		}
	}
	return open, nil
}
