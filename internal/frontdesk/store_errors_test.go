package frontdesk

import (
	"context"
	"testing"
)

// TestStoreMethodsErrorWhenDBClosed verifies every Store method surfaces an error
// (instead of panicking) when the database is unavailable, exercising the
// DB-error branches the happy-path tests never reach.
func TestStoreMethodsErrorWhenDBClosed(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if _, err := s.CreateMember(ctx, "n", "https://h.example", ""); err == nil {
		t.Error("CreateMember: want error")
	}
	if _, err := s.ListMembers(ctx); err == nil {
		t.Error("ListMembers: want error")
	}
	if _, err := s.GetMember(ctx, "x"); err == nil {
		t.Error("GetMember: want error")
	}
	if err := s.RenameMember(ctx, "x", "y"); err == nil {
		t.Error("RenameMember: want error")
	}
	if err := s.SetMemberToken(ctx, "x", "t"); err == nil {
		t.Error("SetMemberToken: want error")
	}
	if err := s.SetMemberState(ctx, "x", StateDrained); err == nil {
		t.Error("SetMemberState: want error")
	}
	if err := s.DeleteMember(ctx, "x"); err == nil {
		t.Error("DeleteMember: want error")
	}
	if _, err := s.DeleteMemberIfNotPrimary(ctx, "x"); err == nil {
		t.Error("DeleteMemberIfNotPrimary: want error")
	}
	if _, _, err := s.MemberToken(ctx, "x"); err == nil {
		t.Error("MemberToken: want error")
	}
	if _, err := s.GetSettings(ctx); err == nil {
		t.Error("GetSettings: want error")
	}
	if err := s.UpdateSettings(ctx, Settings{HealthPollSecs: 1, TraefikPollSecs: 1, TraefikStaleSecs: 1}); err == nil {
		t.Error("UpdateSettings: want error")
	}
	if _, err := s.InsertEvent(ctx, Event{Type: "t", Severity: "info", Source: "s"}); err == nil {
		t.Error("InsertEvent: want error")
	}
	if _, _, err := s.ListEvents(ctx, EventFilter{Limit: 10}); err == nil {
		t.Error("ListEvents: want error")
	}
	if _, err := s.NewestEventPerMember(ctx); err == nil {
		t.Error("NewestEventPerMember: want error")
	}
	if _, err := s.PruneEvents(ctx, 30); err == nil {
		t.Error("PruneEvents: want error")
	}
}
