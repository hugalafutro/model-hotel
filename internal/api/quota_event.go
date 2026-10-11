package api

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// The quota status events announce that a provider's stored quota snapshot
// crossed the line between a readable quota (HTTP 200) and none. The dashboard
// refetches its quota badges on any "quota.status_*" type, so a badge appears
// or disappears when the reading does rather than on the next refresh tick.
const (
	// EventTypeQuotaStatusLost: a reading that was there is gone. A lapsed
	// subscription answers 204, a dead key 424.
	EventTypeQuotaStatusLost = "quota.status_lost"
	// EventTypeQuotaStatusRestored: a reading that was gone is back (200).
	EventTypeQuotaStatusRestored = "quota.status_restored"
	// EventTypeQuotaStatusSynced is the same flip on a node whose reading comes
	// from the fleet: the primary's pushed snapshot, or this node's own fetch
	// replacing a row the fleet had pushed (a breaker nudge polls on every
	// member at once when a plan lapses). The node that fetched for itself
	// already alerted, so this type is deliberately not in the alert catalog;
	// a fleet of N members would otherwise send N+1 notifications for one
	// flip. It still carries the lost/restored message and severity for the
	// member's toast.
	EventTypeQuotaStatusSynced = "quota.status_synced"
)

// storeQuotaSnapshot persists snap for prov and announces a status flip
// against the row it replaced. Every local write of a fresh snapshot (poll,
// nudge, manual refresh, cold fill) goes through here; the fleet import has its
// own write rule and calls publishQuotaStatusChange itself.
func (h *Handler) storeQuotaSnapshot(ctx context.Context, prov *provider.Provider, snap quota.Snapshot) error {
	prior, err := h.quotaRepo.Replace(ctx, snap)
	if err != nil {
		return err
	}
	publishQuotaStatusChange(prov, snap.Kind, prior, snap.HTTPStatus, false)
	return nil
}

// publishQuotaStatusChange emits a quota status event when exactly one of the
// replaced row's status and the new status is 200: a reading was lost or
// restored. A prior status of 0 is not a reading (no row, or RecordFailure's
// placeholder), so the first real answer is not a flip; a move between two
// non-200 answers (204 to 424) changes nothing the badge shows and stays
// quiet. A failed refresh never reaches here: it keeps the stored status, so
// a provider that merely stopped answering stays quiet until it answers
// differently. fleet marks a write from the primary's push; a prior row the
// fleet wrote marks this node as one the primary feeds. Either way the flip
// goes out under the synced type, which the alert catalog omits.
func publishQuotaStatusChange(prov *provider.Provider, kind string, prior quota.Prior, status int, fleet bool) {
	if prior.HTTPStatus == 0 || (prior.HTTPStatus == 200) == (status == 200) {
		return
	}
	typ, severity := EventTypeQuotaStatusLost, "warning"
	message := fmt.Sprintf("%s quota reading stopped: HTTP %d (was %d)", prov.Name, status, prior.HTTPStatus)
	if status == 200 {
		typ, severity = EventTypeQuotaStatusRestored, "success"
		message = fmt.Sprintf("%s quota reading is back (HTTP 200, was %d)", prov.Name, prior.HTTPStatus)
	}
	if fleet || prior.Source == "fleet" {
		typ = EventTypeQuotaStatusSynced
	}
	events.Publish(events.Event{
		Type:     typ,
		Severity: severity,
		Source:   "quota",
		Message:  message,
		Metadata: map[string]any{
			"provider_id":          prov.ID.String(),
			"provider":             prov.Name,
			"provider_type":        provider.TypeOf(prov),
			"kind":                 kind,
			"http_status":          status,
			"previous_http_status": prior.HTTPStatus,
		},
	})
}
