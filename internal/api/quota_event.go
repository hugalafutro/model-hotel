package api

import (
	"context"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// EventTypeQuotaStatusChanged is published when a provider's stored quota
// snapshot changes HTTP status: a reading that was there is gone (a lapsed
// subscription answers 204, a dead key 424) or one that was gone is back (200).
// The dashboard refetches its quota badges on it, so a badge appears or
// disappears when the reading does rather than on the next refresh tick.
const EventTypeQuotaStatusChanged = "quota.status_changed"

// storeQuotaSnapshot persists snap for prov and announces a status flip
// against the row it replaced. Every local write of a fresh snapshot (poll,
// manual refresh, cold fill) goes through here; the fleet import has its own
// write rule and calls publishQuotaStatusChange itself.
func (h *Handler) storeQuotaSnapshot(ctx context.Context, prov *provider.Provider, snap quota.Snapshot) error {
	prev, err := h.quotaRepo.Get(ctx, snap.ProviderID, snap.Kind)
	if err != nil {
		// The write below still proceeds; only the comparison is lost, and the
		// next refresh compares against the row written now.
		debuglog.Warn("quota: previous snapshot read failed", "provider", prov.Name, "error", err)
	}
	if err := h.quotaRepo.Upsert(ctx, snap); err != nil {
		return err
	}
	publishQuotaStatusChange(prov, snap.Kind, prev, snap.HTTPStatus)
	return nil
}

// publishQuotaStatusChange emits EventTypeQuotaStatusChanged when status
// differs from the HTTP status prev recorded. A first snapshot (prev nil) is
// not a change: the add-provider flow already reports what it found. A failed
// refresh never reaches here, so a provider that merely stopped answering
// keeps its last status and stays quiet until it answers differently.
func publishQuotaStatusChange(prov *provider.Provider, kind string, prev *quota.Snapshot, status int) {
	if prev == nil || prev.HTTPStatus == status {
		return
	}
	severity := "warning"
	message := fmt.Sprintf("%s quota reading stopped: HTTP %d (was %d)", prov.Name, status, prev.HTTPStatus)
	if status == 200 {
		severity = "success"
		message = fmt.Sprintf("%s quota reading is back (HTTP 200, was %d)", prov.Name, prev.HTTPStatus)
	}
	events.Publish(events.Event{
		Type:     EventTypeQuotaStatusChanged,
		Severity: severity,
		Source:   "quota",
		Message:  message,
		Metadata: map[string]any{
			"provider_id":          prov.ID.String(),
			"provider":             prov.Name,
			"provider_type":        provider.TypeOf(prov),
			"kind":                 kind,
			"http_status":          status,
			"previous_http_status": prev.HTTPStatus,
		},
	})
}
