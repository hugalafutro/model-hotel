package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// PollTraefikOnce fetches Traefik's serverStatus and maps it onto members by URL.
func (p *Poller) PollTraefikOnce(ctx context.Context) {
	if p.traefikAPI == "" {
		return
	}
	start := p.now()
	defer func() { observePollDuration("traefik", p.now().Sub(start)) }()
	statusByURL, err := p.fetchTraefikServerStatus(ctx)
	if err != nil {
		debuglog.Debug("frontdesk: poll traefik status", "error", err)
		p.noteTraefikAPIFailure(ctx)
		return
	}
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	// Damp the UP->non-UP flip with the same consecutive-miss threshold as health:
	// Traefik briefly stops listing a member (or marks it DOWN) during a rebuild,
	// and committing that immediately flaps the badge. "UP" is applied at once
	// (recovery); a non-UP status is held back until it has been observed
	// `threshold` polls in a row.
	threshold := p.healthFailThreshold(ctx)
	p.mu.Lock()
	p.traefikAPIFails = 0
	p.traefikBlanked = false
	var changed []string
	for _, m := range members {
		cur := p.statuses[m.ID]
		// Key by the same URL BuildTraefikConfig publishes: a legacy row can
		// still carry userinfo, which the emitted config strips.
		next := statusByURL[stripUserinfo(m.URL)] // "" when Traefik does not list it
		if next == "UP" {
			delete(p.traefikNonUp, m.ID)
		} else {
			p.traefikNonUp[m.ID]++
			if p.traefikNonUp[m.ID] < threshold {
				continue // tolerate a rebuild blink; keep the last reported status
			}
		}
		if cur.TraefikStatus != next {
			cur.TraefikStatus = next
			p.statuses[m.ID] = cur
			changed = append(changed, m.ID)
		}
	}
	p.mu.Unlock()
	// Traefik's view caught up to a new/changed member (it needs to re-poll the
	// config before it lists one), so refresh the UI without a manual reload.
	for _, id := range changed {
		p.publishMemberStatus(id)
	}
}

// noteTraefikAPIFailure counts consecutive failed polls of Traefik's own API
// and, once they cross the health-fail threshold, blanks every member's
// TraefikStatus. Without this the badges freeze at their last value while the
// API is down, painting a live-looking status from a dead data source; blank
// renders as the existing faint "unknown". Damped by the same threshold as the
// UP->DOWN flip so a restart blip of Traefik does not blank the column.
func (p *Poller) noteTraefikAPIFailure(ctx context.Context) {
	threshold := p.healthFailThreshold(ctx)
	p.mu.Lock()
	if p.traefikBlanked {
		// Already blanked by an earlier poll in this outage: nothing left to blank,
		// so stop advancing the counter. A successful poll (which resets both
		// flags) re-arms this.
		p.mu.Unlock()
		return
	}
	p.traefikAPIFails++
	if p.traefikAPIFails < threshold {
		p.mu.Unlock()
		return
	}
	// Crossed the threshold: blank every live badge. Only members that were
	// actually polled hold a non-empty TraefikStatus, so iterate the in-memory
	// statuses directly rather than re-reading the store (a member absent from
	// this map already renders as "unknown"). Publishing happens after unlock.
	var changed []string
	for id, cur := range p.statuses {
		if cur.TraefikStatus != "" {
			cur.TraefikStatus = ""
			p.statuses[id] = cur
			changed = append(changed, id)
		}
		delete(p.traefikNonUp, id)
	}
	p.traefikBlanked = true
	p.mu.Unlock()
	for _, id := range changed {
		p.publishMemberStatus(id)
	}
}

func (p *Poller) fetchTraefikServerStatus(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.traefikAPI+traefikServicesAPI, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("traefik api returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseTraefikServerStatus(body)
}

// traefikServiceInfo is the subset of a Traefik API service object we read.
type traefikServiceInfo struct {
	Name         string            `json:"name"`
	ServerStatus map[string]string `json:"serverStatus"`
}

// parseTraefikServerStatus extracts the server-URL -> status map for the hotel
// service from a Traefik /api/http/services response.
func parseTraefikServerStatus(body []byte) (map[string]string, error) {
	var services []traefikServiceInfo
	if err := json.Unmarshal(body, &services); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		return nil, errors.New("frontdesk: parse traefik services response")
	}
	for _, svc := range services {
		// Traefik names HTTP-provider services like "hotel@http".
		if svc.Name == traefikServiceName || strings.HasPrefix(svc.Name, traefikServiceName+"@") {
			return svc.ServerStatus, nil
		}
	}
	return map[string]string{}, nil
}
