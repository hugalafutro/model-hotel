package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// A drain or activation sent with reason=maintenance (the fleet rebuild tool's
// planned flips) is recorded as health.maintenance, the type that pages nobody
// by default, instead of member.state_changed; a reason the server does not
// know is refused; a plain flip still records member.state_changed.
func TestServerMemberStateMaintenanceReason(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	first, err := store.CreateMember(ctx, "hotel-1", "http://hotel-1:8081", "")
	if err != nil {
		t.Fatalf("first member: %v", err)
	}
	if _, err := store.CreateMember(ctx, "hotel-2", "http://hotel-2:8081", ""); err != nil {
		t.Fatalf("second member: %v", err)
	}
	events := func(typ string) []Event {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/events?type="+typ, "", true)
		var out struct {
			Events []Event `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode events: %v; body=%s", err, rec.Body.String())
		}
		return out.Events
	}

	rec := do(t, srv, http.MethodPost, "/api/members/"+first.ID+"/state", `{"state":"drained","reason":"upgrade"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown reason = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var coded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &coded); err != nil || coded["code"] != "invalid_reason" {
		t.Fatalf("400 code = %q (err %v), want invalid_reason; body=%s", coded["code"], err, rec.Body.String())
	}
	if m, err := store.GetMember(ctx, first.ID); err != nil || m.State != StateActive {
		t.Fatalf("a refused reason must not change state: %+v %v", m, err)
	}
	if n := len(events("member.state_changed")) + len(events("health.maintenance")); n != 0 {
		t.Fatalf("a refused reason must record nothing, got %d events", n)
	}

	for _, state := range []string{"drained", "active"} {
		if rec := do(t, srv, http.MethodPost, "/api/members/"+first.ID+"/state", `{"state":"`+state+`","reason":"maintenance"}`, true); rec.Code != http.StatusOK {
			t.Fatalf("%s for maintenance = %d, want 200; body=%s", state, rec.Code, rec.Body.String())
		}
	}
	got := events("health.maintenance")
	if len(got) != 2 {
		t.Fatalf("health.maintenance events = %d, want 2 (drain + activate)", len(got))
	}
	for _, ev := range got {
		if ev.Severity != "info" || ev.Metadata["reason"] != "maintenance" || ev.MemberID != first.ID {
			t.Errorf("maintenance event = %+v", ev)
		}
	}
	if paged := events("member.state_changed"); len(paged) != 0 {
		t.Fatalf("a planned flip must not record member.state_changed, got %+v", paged)
	}

	if rec := do(t, srv, http.MethodPost, "/api/members/"+first.ID+"/state", `{"state":"drained"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("plain drain = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if paged := events("member.state_changed"); len(paged) != 1 || paged[0].Severity != "warning" {
		t.Fatalf("a hand-made drain still records member.state_changed at warning, got %+v", paged)
	}
	if rec := do(t, srv, http.MethodPost, "/api/members/"+first.ID+"/state", `{"state":"active"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("plain activate = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if paged := events("member.state_changed"); len(paged) != 2 || paged[0].Severity != "info" && paged[1].Severity != "info" {
		t.Fatalf("a hand-made activation records member.state_changed at info, got %+v", paged)
	}

	// The reason buys no way around the routing-pool guard: draining the last
	// active member for maintenance is refused like any other drain.
	if rec := do(t, srv, http.MethodPost, "/api/members/"+first.ID+"/state", `{"state":"drained","reason":"maintenance"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("drain first for maintenance = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	second, err := store.ListMembers(ctx)
	if err != nil || len(second) != 2 {
		t.Fatalf("list members: %v (%d)", err, len(second))
	}
	other := second[0].ID
	if other == first.ID {
		other = second[1].ID
	}
	if rec := do(t, srv, http.MethodPost, "/api/members/"+other+"/state", `{"state":"drained","reason":"maintenance"}`, true); rec.Code != http.StatusConflict {
		t.Fatalf("drain last active for maintenance = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}
