package api

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// The quota status events announce that a provider's stored quota snapshot
// changed HTTP status. The dashboard refetches its quota badges on any
// "quota.status_*" type, so a badge appears or disappears when the reading
// does rather than on the next refresh tick.
const (
	// EventTypeQuotaStatusLost: a reading that was there is gone. A lapsed
	// subscription answers 204, a dead key 424.
	EventTypeQuotaStatusLost = "quota.status_lost"
	// EventTypeQuotaStatusRestored: a reading that was gone is back (200).
	EventTypeQuotaStatusRestored = "quota.status_restored"
	// EventTypeQuotaStatusSynced is the same flip arriving on a fleet member
	// through the primary's pushed snapshot. The primary already alerted on it,
	// so this type is deliberately not in the alert catalog: a fleet of N
	// members would otherwise send N+1 notifications for one flip. It still
	// carries the lost/restored message and severity for the member's toast.
	EventTypeQuotaStatusSynced = "quota.status_synced"
)

// storeQuotaSnapshot persists snap for prov and announces a status flip
// against the row it replaced. Every local write of a fresh snapshot (poll,
// manual refresh, cold fill) goes through here; the fleet import has its own
// write rule and calls publishQuotaStatusChange itself.
func (h *Handler) storeQuotaSnapshot(ctx context.Context, prov *provider.Provider, snap quota.Snapshot) error {
	prev, err := h.quotaRepo.Replace(ctx, snap)
	if err != nil {
		return err
	}
	publishQuotaStatusChange(prov, snap.Kind, prev, snap.HTTPStatus, false)
	return nil
}

// publishQuotaStatusChange emits a quota status event when status differs from
// prevStatus, the HTTP status of the row the write replaced. prevStatus 0 is
// not a reading: either no row existed (the add-provider flow already reports
// what it found) or only RecordFailure's placeholder did, so the first real
// answer is not a flip. A failed refresh never reaches here: it keeps the
// stored status, so a provider that merely stopped answering stays quiet until
// it answers differently. fleet marks a flip learned from the primary's push.
func publishQuotaStatusChange(prov *provider.Provider, kind string, prevStatus, status int, fleet bool) {
	if prevStatus == 0 || prevStatus == status {
		return
	}
	typ, severity := EventTypeQuotaStatusLost, "warning"
	message := fmt.Sprintf("%s quota reading stopped: HTTP %d (was %d)", prov.Name, status, prevStatus)
	if status == 200 {
		typ, severity = EventTypeQuotaStatusRestored, "success"
		message = fmt.Sprintf("%s quota reading is back (HTTP 200, was %d)", prov.Name, prevStatus)
	}
	if fleet {
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
			"previous_http_status": prevStatus,
		},
	})
}
