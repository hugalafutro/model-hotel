package api

import (
	"context"
	"fmt"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
	"github.com/hugalafutro/model-hotel/internal/util"
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
	// EventTypeQuotaStatusSynced is the same flip on a fleet member for a
	// provider the primary is feeding, whether it arrived in the primary's
	// pushed snapshot or in the member's own fetch over a fresh pushed row (a
	// breaker nudge polls on every member at once when a plan lapses). The
	// primary alerts for the fleet, so this type is deliberately not in the
	// alert catalog; N members would otherwise send N more notifications for
	// one flip. It still carries the lost/restored message and severity for
	// the member's toast.
	EventTypeQuotaStatusSynced = "quota.status_synced"
)

// storeQuotaSnapshot persists snap for prov and announces a status flip
// against the row it replaced, under quotaWriteMu so the events this process
// publishes follow the order its writes committed in. Every local write of a
// fresh snapshot (poll, nudge, manual refresh, cold fill) goes through here;
// the fleet import has its own write rule and calls publishQuotaStatusChange
// itself, under the same mutex.
func (h *Handler) storeQuotaSnapshot(ctx context.Context, prov *provider.Provider, snap quota.Snapshot) error {
	h.quotaWriteMu.Lock()
	defer h.quotaWriteMu.Unlock()
	prior, err := h.quotaRepo.Replace(ctx, snap)
	if err != nil {
		return err
	}
	publishQuotaStatusChange(prov, snap.Kind, prior, snap.HTTPStatus, h.quotaFedByFleet(ctx, prior))
	return nil
}

// quotaFedByFleet reports whether this node leaves alerting on this flip to the
// primary: it is a managed member and the row it just replaced was pushed by
// the fleet less than one refresh interval ago. That is the same test the poll
// pass uses to skip a provider the primary is feeding, so a write that reaches
// here through the poll pass never qualifies (the row was already stale, which
// is exactly the primary not feeding this provider: down, unreachable, or
// failing on this one upstream) and the member alerts for itself. Only a
// breaker nudge or a manual refresh over a fresh pushed row defers, and the
// primary's next poll reads the same upstream and alerts within the interval.
// Duplicate alerts during an outage beat silence; a read of the role that
// fails alerts too, for the same reason.
func (h *Handler) quotaFedByFleet(ctx context.Context, prior quota.Prior) bool {
	if prior.Source != "fleet" || !isManagedMember(ctx, h.settingsRepo) {
		return false
	}
	interval := quotaRefreshInterval(ctx, h.settingsRepo)
	age, ok := util.TrustedAge(time.Now(), prior.FetchedAt)
	return interval > 0 && ok && age < interval
}

// publishQuotaStatusChange emits a quota status event when exactly one of the
// replaced row's status and the new status is 200: a reading was lost or
// restored. A prior status of 0 is not a reading (no row, or RecordFailure's
// placeholder), so the first real answer is not a flip; a move between two
// non-200 answers (204 to 424) changes nothing the badge shows and stays
// quiet. A failed refresh never reaches here: it keeps the stored status, so
// a provider that merely stopped answering stays quiet until it answers
// differently. fed says this node leaves alerting to the primary (the import
// path when the push replaces an earlier push, a local fetch when
// quotaFedByFleet says so), so its flip goes out under the synced type the
// alert catalog omits.
func publishQuotaStatusChange(prov *provider.Provider, kind string, prior quota.Prior, status int, fed bool) {
	if prior.HTTPStatus == 0 || (prior.HTTPStatus == 200) == (status == 200) {
		return
	}
	typ, severity := EventTypeQuotaStatusLost, "warning"
	message := fmt.Sprintf("%s quota reading stopped: HTTP %d (was %d)", prov.Name, status, prior.HTTPStatus)
	if status == 200 {
		typ, severity = EventTypeQuotaStatusRestored, "success"
		message = fmt.Sprintf("%s quota reading is back (HTTP 200, was %d)", prov.Name, prior.HTTPStatus)
	}
	if fed {
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
