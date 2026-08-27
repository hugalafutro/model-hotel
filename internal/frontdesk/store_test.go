package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testMasterKey = "test-master-key-0123456789abcdef"

// newTestStore opens a real SQLite store on a temp file.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "frontdesk.db")
	s, err := Open(path, testMasterKey, true) // allow http: tests use httptest (http://127.0.0.1) members
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontdesk.db")
	s1, err := Open(path, testMasterKey, true)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s1.CreateMember(context.Background(), "h1", "http://h1:8081", ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	_ = s1.Close()

	// Re-open the same file: migrations already applied, data preserved.
	s2, err := Open(path, testMasterKey, true)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	members, err := s2.ListMembers(context.Background())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member after reopen, got %d", len(members))
	}
}

func TestCreateMemberValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name, url string
	}{
		{"", "http://h:8081"},   // empty name
		{"  ", "http://h:8081"}, // whitespace name
		{"h", ""},               // empty url
		{"h", "ftp://h:8081"},   // bad scheme
		{"h", "://nope"},        // unparseable
		{"h", "http://"},        // no host
	}
	for _, c := range cases {
		if _, err := s.CreateMember(ctx, c.name, c.url, ""); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateMember(%q,%q): want ErrValidation, got %v", c.name, c.url, err)
		}
	}
}

func TestCreateMemberNormalizesAndDedupes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.CreateMember(ctx, "hotel-1", "HTTP://Host:8081/", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if m.URL != "http://Host:8081" {
		t.Errorf("normalized URL = %q, want http://Host:8081", m.URL)
	}
	if m.State != StateActive {
		t.Errorf("default state = %q, want active", m.State)
	}
	if m.HasToken {
		t.Error("HasToken should be false when no token given")
	}

	// Same URL (after normalization) is a duplicate.
	if _, err := s.CreateMember(ctx, "dup", "http://Host:8081", ""); !errors.Is(err, ErrDuplicateURL) {
		t.Errorf("duplicate URL: want ErrDuplicateURL, got %v", err)
	}
}

func TestMemberTokenRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.CreateMember(ctx, "h", "http://h:8081", "secret-admin-token")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if !m.HasToken {
		t.Fatal("HasToken should be true")
	}
	tok, ok, err := s.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		t.Fatalf("MemberToken: ok=%v err=%v", ok, err)
	}
	if tok != "secret-admin-token" {
		t.Errorf("decrypted token = %q", tok)
	}

	// Clearing the token removes it.
	if err := s.SetMemberToken(ctx, m.ID, ""); err != nil {
		t.Fatalf("SetMemberToken(clear): %v", err)
	}
	if _, ok, _ := s.MemberToken(ctx, m.ID); ok {
		t.Error("token should be cleared")
	}
	reloaded, _ := s.GetMember(ctx, m.ID)
	if reloaded.HasToken {
		t.Error("HasToken should be false after clear")
	}
}

// TestMemberTokenRotationIsNotServedFromCache: MemberToken decrypts through the
// shared key cache, so a rotated token must never come back as the one that was
// cached for the old ciphertext. Reading before the rotation is what makes the
// test meaningful: it populates the cache entry that a naive lookup would reuse.
func TestMemberTokenRotationIsNotServedFromCache(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.CreateMember(ctx, "h", "http://h:8081", "first-token")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if tok, ok, err := s.MemberToken(ctx, m.ID); err != nil || !ok || tok != "first-token" {
		t.Fatalf("MemberToken before rotation = %q ok=%v err=%v, want first-token", tok, ok, err)
	}

	if err := s.SetMemberToken(ctx, m.ID, "second-token"); err != nil {
		t.Fatalf("SetMemberToken(rotate): %v", err)
	}

	tok, ok, err := s.MemberToken(ctx, m.ID)
	if err != nil || !ok {
		t.Fatalf("MemberToken after rotation: ok=%v err=%v", ok, err)
	}
	if tok != "second-token" {
		t.Errorf("decrypted token = %q, want second-token: the rotated token was served from the cache", tok)
	}
}

func TestMemberTokenRequiresMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontdesk.db")
	s, err := Open(path, "", true) // no master key
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// A token with no master key must be rejected, never stored in the clear.
	if _, err := s.CreateMember(context.Background(), "h", "http://h:8081", "tok"); !errors.Is(err, ErrValidation) {
		t.Errorf("want ErrValidation, got %v", err)
	}
	// But a member without a token is fine.
	if _, err := s.CreateMember(context.Background(), "h", "http://h:8081", ""); err != nil {
		t.Errorf("tokenless member should succeed, got %v", err)
	}
}

func TestMemberStateAndRename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m, _ := s.CreateMember(ctx, "h", "http://h:8081", "")
	// A second active member so draining the first is allowed: the guard only
	// blocks draining the last active member (see TestSetMemberStateLastActiveGuard).
	if _, err := s.CreateMember(ctx, "h2", "http://h2:8081", ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	if err := s.SetMemberState(ctx, m.ID, StateDrained); err != nil {
		t.Fatalf("SetMemberState: %v", err)
	}
	if err := s.SetMemberState(ctx, m.ID, "bogus"); !errors.Is(err, ErrValidation) {
		t.Errorf("bad state: want ErrValidation, got %v", err)
	}
	if err := s.RenameMember(ctx, m.ID, "renamed"); err != nil {
		t.Fatalf("RenameMember: %v", err)
	}
	if err := s.RenameMember(ctx, m.ID, "  "); !errors.Is(err, ErrValidation) {
		t.Errorf("empty rename: want ErrValidation, got %v", err)
	}

	got, _ := s.GetMember(ctx, m.ID)
	if got.State != StateDrained || got.Name != "renamed" {
		t.Errorf("got state=%q name=%q", got.State, got.Name)
	}
}

func TestSetMemberStateLastActiveGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, _ := s.CreateMember(ctx, "a", "http://a:8081", "")
	b, _ := s.CreateMember(ctx, "b", "http://b:8081", "")

	// Draining one of two active members is allowed.
	if err := s.SetMemberState(ctx, a.ID, StateDrained); err != nil {
		t.Fatalf("drain first of two: %v", err)
	}
	// Draining the now-last active member is refused, whichever member it is.
	if err := s.SetMemberState(ctx, b.ID, StateDrained); !errors.Is(err, ErrLastActiveMember) {
		t.Fatalf("drain last active: want ErrLastActiveMember, got %v", err)
	}
	// The refused drain did not apply: the member stays active.
	if got, _ := s.GetMember(ctx, b.ID); got.State != StateActive {
		t.Errorf("last active member state = %q, want active", got.State)
	}
	// Reactivating the drained one is always allowed and restores headroom, after
	// which draining the other is allowed again.
	if err := s.SetMemberState(ctx, a.ID, StateActive); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if err := s.SetMemberState(ctx, b.ID, StateDrained); err != nil {
		t.Fatalf("drain after reactivate: %v", err)
	}
	// A drain of a nonexistent member still reports not-found, not the guard.
	if err := s.SetMemberState(ctx, "nope", StateDrained); !errors.Is(err, ErrNotFound) {
		t.Errorf("drain nonexistent: want ErrNotFound, got %v", err)
	}
}

func TestMemberNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetMember(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMember: want ErrNotFound, got %v", err)
	}
	if err := s.RenameMember(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenameMember: want ErrNotFound, got %v", err)
	}
	if err := s.SetMemberState(ctx, "nope", StateActive); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetMemberState: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteMember(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteMember: want ErrNotFound, got %v", err)
	}
	if _, _, err := s.MemberToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MemberToken: want ErrNotFound, got %v", err)
	}
}

func TestDeleteMember(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m, _ := s.CreateMember(ctx, "h", "http://h:8081", "")
	if err := s.DeleteMember(ctx, m.ID); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	members, _ := s.ListMembers(ctx)
	if len(members) != 0 {
		t.Errorf("expected 0 members, got %d", len(members))
	}
}

func TestSettingsDefaultsAndUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	def, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if def.HealthPollSecs != 5 || def.EventRetentionDays != 90 || def.RetryAttempts != 2 {
		t.Errorf("unexpected defaults: %+v", def)
	}
	if def.SessionIdleTimeoutMinutes != 60 {
		t.Errorf("session idle timeout default = %d, want 60", def.SessionIdleTimeoutMinutes)
	}
	if def.HealthFailThreshold != 3 {
		t.Errorf("health fail threshold default = %d, want 3", def.HealthFailThreshold)
	}

	updated := Settings{
		HealthPollSecs: 10, TraefikPollSecs: 7, TraefikStaleSecs: 60,
		EventRetentionDays: 30, RetryAttempts: 0, SessionIdleTimeoutMinutes: 30,
		HealthFailThreshold: 4,
	}
	if err := s.UpdateSettings(ctx, updated); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	got, _ := s.GetSettings(ctx)
	if got != updated {
		t.Errorf("got %+v, want %+v", got, updated)
	}
}

func TestSettingsValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	bad := []Settings{
		{HealthPollSecs: 0, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: 1, HealthFailThreshold: 1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 0, RetryAttempts: 1, HealthFailThreshold: 1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: -1, HealthFailThreshold: 1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: 1, HealthFailThreshold: 1, SessionIdleTimeoutMinutes: -1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: 1, HealthFailThreshold: 1, SessionIdleTimeoutMinutes: 241},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: 1, HealthFailThreshold: 0},
	}
	for i, b := range bad {
		if err := s.UpdateSettings(ctx, b); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: want ErrValidation, got %v", i, err)
		}
	}
}

func TestEventsInsertListFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, _ := s.CreateMember(ctx, "h", "http://h:8081", "")
	_, err := s.InsertEvent(ctx, Event{
		Type: "member.added", Severity: "info", Source: "frontdesk",
		Message: "added", MemberID: m.ID, Metadata: map[string]any{"name": "h"},
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	_, _ = s.InsertEvent(ctx, Event{Type: "health.up", Severity: "success", Source: "poller", Message: "up", MemberID: m.ID})
	_, _ = s.InsertEvent(ctx, Event{Type: "config.regenerated", Severity: "info", Source: "frontdesk", Message: "regen"})

	all, total, err := s.ListEvents(ctx, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("got total=%d len=%d, want 3", total, len(all))
	}
	// Newest first.
	if all[0].Type != "config.regenerated" {
		t.Errorf("ordering: first = %q", all[0].Type)
	}
	// Metadata round-trips.
	var withMeta Event
	for _, e := range all {
		if e.Type == "member.added" {
			withMeta = e
		}
	}
	if withMeta.Metadata["name"] != "h" {
		t.Errorf("metadata round-trip: %+v", withMeta.Metadata)
	}

	// Filter by member.
	byMember, total, _ := s.ListEvents(ctx, EventFilter{MemberID: m.ID})
	if total != 2 || len(byMember) != 2 {
		t.Errorf("by member: total=%d len=%d, want 2", total, len(byMember))
	}
	// Filter by severity.
	bySev, _, _ := s.ListEvents(ctx, EventFilter{Severity: "success"})
	if len(bySev) != 1 || bySev[0].Type != "health.up" {
		t.Errorf("by severity: %+v", bySev)
	}
	// Filter by type.
	byType, _, _ := s.ListEvents(ctx, EventFilter{Type: "config.regenerated"})
	if len(byType) != 1 {
		t.Errorf("by type: len=%d", len(byType))
	}
}

func TestNewestEventPerMember(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, _ := s.CreateMember(ctx, "a", "http://a:8081", "")
	b, _ := s.CreateMember(ctx, "b", "http://b:8081", "")

	base := time.Now().UTC()
	// a has two events; the newer one (health.up) must win. Explicit timestamps
	// keep the pick deterministic rather than leaning on insert-time spacing.
	_, _ = s.InsertEvent(ctx, Event{Type: "member.added", Severity: "info", Source: "frontdesk", Message: "added", MemberID: a.ID, CreatedAt: base})
	_, _ = s.InsertEvent(ctx, Event{Type: "health.up", Severity: "success", Source: "poller", Message: "up", MemberID: a.ID, CreatedAt: base.Add(time.Minute)})
	// b has one event.
	_, _ = s.InsertEvent(ctx, Event{Type: "health.down", Severity: "error", Source: "poller", Message: "down", MemberID: b.ID, CreatedAt: base})
	// A fleet-wide event (no member_id) must never appear in the map.
	_, _ = s.InsertEvent(ctx, Event{Type: "config.regenerated", Severity: "info", Source: "frontdesk", Message: "regen", CreatedAt: base})

	got, err := s.NewestEventPerMember(ctx)
	if err != nil {
		t.Fatalf("NewestEventPerMember: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d members, want 2 (fleet-wide event must be excluded): %+v", len(got), got)
	}
	if got[a.ID].Type != "health.up" {
		t.Errorf("member a newest = %q, want health.up", got[a.ID].Type)
	}
	if got[b.ID].Type != "health.down" {
		t.Errorf("member b newest = %q, want health.down", got[b.ID].Type)
	}

	// A fleet with no member-scoped events yields an empty (non-nil) map.
	empty := newTestStore(t)
	m, err := empty.NewestEventPerMember(ctx)
	if err != nil {
		t.Fatalf("NewestEventPerMember (empty): %v", err)
	}
	if len(m) != 0 {
		t.Errorf("empty store: got %d, want 0", len(m))
	}
}

func TestNewestEventOfTypes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.CreateMember(ctx, "a", "http://a:8081", "")
	if err != nil {
		t.Fatalf("CreateMember a: %v", err)
	}
	b, err := s.CreateMember(ctx, "b", "http://b:8081", "")
	if err != nil {
		t.Fatalf("CreateMember b: %v", err)
	}
	insert := func(e Event) {
		t.Helper()
		if _, err := s.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent %s: %v", e.Type, err)
		}
	}

	base := time.Now().UTC()
	insert(Event{Type: "config.sync_held", Severity: "warning", Source: "frontdesk", Message: "held", MemberID: a.ID, CreatedAt: base})
	insert(Event{Type: "config.sync_recovered", Severity: "success", Source: "frontdesk", Message: "recovered", MemberID: a.ID, CreatedAt: base.Add(time.Minute)})
	// Newer events of other types, and other members' events, must not shadow the pick.
	insert(Event{Type: "health.up", Severity: "success", Source: "poller", Message: "up", MemberID: a.ID, CreatedAt: base.Add(2 * time.Minute)})
	insert(Event{Type: "config.sync_held", Severity: "warning", Source: "frontdesk", Message: "held", MemberID: b.ID, CreatedAt: base.Add(3 * time.Minute)})

	ev, found, err := s.NewestEventOfTypes(ctx, a.ID, "config.sync_held", "config.sync_recovered")
	if err != nil {
		t.Fatalf("NewestEventOfTypes: %v", err)
	}
	if !found || ev.Type != "config.sync_recovered" {
		t.Errorf("newest of the two types = (%q, %v), want config.sync_recovered", ev.Type, found)
	}

	// Equal timestamps resolve by id DESC, the same tie-break ListEvents and
	// NewestEventPerMember serve, so a caller ranking two types agrees with the
	// surfaces that render the same rows. Explicit ids pin the winner.
	insert(Event{ID: "id-a", Type: "config.sync_recovered", Severity: "success", Source: "frontdesk", Message: "recovered", MemberID: b.ID, CreatedAt: base.Add(4 * time.Minute)})
	insert(Event{ID: "id-z", Type: "config.sync_held", Severity: "warning", Source: "frontdesk", Message: "held", MemberID: b.ID, CreatedAt: base.Add(4 * time.Minute)})
	ev, found, err = s.NewestEventOfTypes(ctx, b.ID, "config.sync_held", "config.sync_recovered")
	if err != nil {
		t.Fatalf("NewestEventOfTypes (tie): %v", err)
	}
	if !found || ev.ID != "id-z" {
		t.Errorf("timestamp tie pick = (%q, %v), want id-z (id DESC tie-break)", ev.ID, found)
	}

	// A member with no event of the given types reports found=false, whatever
	// else its log holds.
	if _, found, err := s.NewestEventOfTypes(ctx, a.ID, "config.auto_synced"); err != nil || found {
		t.Errorf("no event of type: found=%v err=%v, want found=false", found, err)
	}
	if _, found, err := s.NewestEventOfTypes(ctx, a.ID); err != nil || found {
		t.Errorf("no types given: found=%v err=%v, want found=false", found, err)
	}
}

func TestEventsPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for range 5 {
		_, _ = s.InsertEvent(ctx, Event{Type: "t", Severity: "info", Source: "x", Message: "m"})
	}
	page, total, _ := s.ListEvents(ctx, EventFilter{Limit: 2, Offset: 0})
	if total != 5 || len(page) != 2 {
		t.Errorf("page1: total=%d len=%d", total, len(page))
	}
	page2, _, _ := s.ListEvents(ctx, EventFilter{Limit: 2, Offset: 4})
	if len(page2) != 1 {
		t.Errorf("page3 should have 1, got %d", len(page2))
	}
}

func TestPruneEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := Event{Type: "t", Severity: "info", Source: "x", Message: "old", CreatedAt: time.Now().Add(-100 * 24 * time.Hour)}
	if _, err := s.InsertEvent(ctx, old); err != nil {
		t.Fatalf("InsertEvent old: %v", err)
	}
	if _, err := s.InsertEvent(ctx, Event{Type: "t", Severity: "info", Source: "x", Message: "new"}); err != nil {
		t.Fatalf("InsertEvent new: %v", err)
	}

	n, err := s.PruneEvents(ctx, 90)
	if err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	_, total, _ := s.ListEvents(ctx, EventFilter{})
	if total != 1 {
		t.Errorf("remaining %d, want 1", total)
	}
}

func TestEnsureFrontdeskID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID: %v", err)
	}
	if id == "" {
		t.Fatal("EnsureFrontdeskID returned empty id")
	}

	// Idempotent: a second call returns the same value, not a fresh UUID.
	id2, err := s.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID (second call): %v", err)
	}
	if id2 != id {
		t.Errorf("second call returned %q, want stable %q", id2, id)
	}
}

func TestEnsureFrontdeskIDPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontdesk.db")
	s1, err := Open(path, testMasterKey, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	id, err := s1.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID: %v", err)
	}
	_ = s1.Close()

	// Reopen the same file: the ID survives a restart.
	s2, err := Open(path, testMasterKey, true)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	got, err := s2.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID after reopen: %v", err)
	}
	if got != id {
		t.Errorf("after reopen got %q, want persisted %q", got, id)
	}
}

func TestDeleteMemberOrDisband_TwoMemberFleet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pm, err := s.CreateMember(ctx, "primary", "https://p.example.com", "ptok")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	om, err := s.CreateMember(ctx, "other", "https://o.example.com", "")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	// Designate pm as the auto-sync primary and record a sync run, so the
	// disband has real designation state to clear.
	if _, err := s.SetAutoSyncGuarded(ctx, true, pm.ID, true); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if err := s.SetFleetSyncState(ctx, pm.ID, "primary", time.Now()); err != nil {
		t.Fatalf("seed fleet sync state: %v", err)
	}

	// The primary cannot be deleted (no token bypass exists anymore).
	if outcome, _, err := s.DeleteMemberOrDisband(ctx, pm.ID); err != nil || outcome != DeleteRefusedPrimary {
		t.Fatalf("delete primary: outcome=%v err=%v, want DeleteRefusedPrimary", outcome, err)
	}
	if _, err := s.GetMember(ctx, pm.ID); err != nil {
		t.Errorf("primary should still exist: %v", err)
	}

	// Removing the non-primary member of a two-member fleet disbands the whole
	// fleet: both rows go, auto-sync switches off, the designation and the
	// last-sync marker clear.
	outcome, removed, err := s.DeleteMemberOrDisband(ctx, om.ID)
	if err != nil || outcome != DeleteDisbanded {
		t.Fatalf("delete non-primary: outcome=%v err=%v, want DeleteDisbanded", outcome, err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed = %v, want both members", removed)
	}
	members, err := s.ListMembers(ctx)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members remain after disband: %v", members)
	}
	cfg, err := s.GetAutoSync(ctx)
	if err != nil {
		t.Fatalf("get auto-sync: %v", err)
	}
	if cfg.Enabled || cfg.PrimaryID != "" {
		t.Errorf("auto-sync survived the disband: %+v", cfg)
	}
	if _, found, err := s.GetFleetSyncState(ctx); err != nil || found {
		t.Errorf("fleet sync state survived the disband: found=%v err=%v", found, err)
	}
	// The generation must SURVIVE the disband: members keep their last-applied
	// gen as an import fence, so a re-formed fleet has to keep counting upward
	// or its first push would look stale to every surviving member.
	if cfg.Gen == 0 {
		t.Error("auto_sync_gen was reset by the disband; it must survive as the import fence")
	}
}

// TestSetAutoSyncGuarded_FleetSizeFloor pins the in-statement two-member floor:
// a NEW designation is refused while fewer than two member rows exist (closing
// the race where a disband lands between the handler's count read and the
// write), while clearing and unchanged-primary toggles (legacy one-member
// fleets included) still apply.
func TestSetAutoSyncGuarded_FleetSizeFloor(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	lm, err := s.CreateMember(ctx, "lone", "https://l.example.com", "ltok")
	if err != nil {
		t.Fatalf("create lone: %v", err)
	}

	// New designation on a one-member fleet: refused even with a valid token.
	if applied, err := s.SetAutoSyncGuarded(ctx, false, lm.ID, true); err != nil || applied {
		t.Fatalf("designate on lone fleet: applied=%v err=%v, want refused", applied, err)
	}

	// A legacy designation (seeded unguarded) can still be toggled: the write
	// leaves the primary unchanged, so the floor does not apply.
	if err := s.SetAutoSync(ctx, false, lm.ID); err != nil {
		t.Fatalf("seed legacy designation: %v", err)
	}
	if applied, err := s.SetAutoSyncGuarded(ctx, true, lm.ID, false); err != nil || !applied {
		t.Fatalf("toggle legacy lone designation: applied=%v err=%v, want applied", applied, err)
	}
	// And clearing it always works.
	if applied, err := s.SetAutoSyncGuarded(ctx, false, "", true); err != nil || !applied {
		t.Fatalf("clear lone designation: applied=%v err=%v, want applied", applied, err)
	}

	// With a second member the same designation applies.
	if _, err := s.CreateMember(ctx, "second", "https://s.example.com", ""); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if applied, err := s.SetAutoSyncGuarded(ctx, false, lm.ID, true); err != nil || !applied {
		t.Fatalf("designate on two-member fleet: applied=%v err=%v, want applied", applied, err)
	}
}

// TestDeleteMemberOrDisband_LoneRow covers the bootstrap escape hatch: a single
// just-added row is removable (it is not a functioning fleet), and a stale
// primary designation pointing at it cannot wedge it in place.
func TestDeleteMemberOrDisband_LoneRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	lm, err := s.CreateMember(ctx, "lone", "https://l.example.com", "ltok")
	if err != nil {
		t.Fatalf("create lone: %v", err)
	}
	// Seed the stale designation with the unguarded setter: the guarded one now
	// enforces the two-member floor, and this legacy state predates it.
	if err := s.SetAutoSync(ctx, false, lm.ID); err != nil {
		t.Fatalf("designate lone: %v", err)
	}

	outcome, removed, err := s.DeleteMemberOrDisband(ctx, lm.ID)
	if err != nil || outcome != DeleteDisbanded {
		t.Fatalf("delete lone row: outcome=%v err=%v, want DeleteDisbanded", outcome, err)
	}
	if len(removed) != 1 || removed[0].ID != lm.ID {
		t.Fatalf("removed = %v, want just the lone row", removed)
	}
	cfg, err := s.GetAutoSync(ctx)
	if err != nil {
		t.Fatalf("get auto-sync: %v", err)
	}
	if cfg.PrimaryID != "" {
		t.Errorf("stale designation survived: %+v", cfg)
	}
}

func TestDeleteMemberOrDisband_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateMember(ctx, "only", "https://only.example.com", ""); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, _, err := s.DeleteMemberOrDisband(ctx, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete unknown id: err=%v, want ErrNotFound", err)
	}
}

// TestDeleteMemberOrDisband_StatementFailures forces each statement of the
// delete transaction to fail for real (RAISE(ABORT) triggers and a dropped
// table) so the error paths are exercised without any fault-injection seam in
// production code. Each case gets a fresh store; the trigger or drop is applied
// through the store's own connection.
func TestDeleteMemberOrDisband_StatementFailures(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, n int) (*Store, []string) {
		t.Helper()
		s := newTestStore(t)
		ids := make([]string, 0, n)
		for i := range n {
			m, err := s.CreateMember(ctx, fmt.Sprintf("m%d", i), fmt.Sprintf("https://m%d.example.com", i), "")
			if err != nil {
				t.Fatalf("create member %d: %v", i, err)
			}
			ids = append(ids, m.ID)
		}
		return s, ids
	}
	breakWith := func(t *testing.T, s *Store, ddl string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("apply breakage %q: %v", ddl, err)
		}
	}
	wantErr := func(t *testing.T, s *Store, id, fragment string) {
		t.Helper()
		_, _, err := s.DeleteMemberOrDisband(ctx, id)
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Fatalf("err = %v, want it to contain %q", err, fragment)
		}
	}

	t.Run("roster read fails", func(t *testing.T) {
		s, _ := seed(t, 1)
		breakWith(t, s, `DROP TABLE members`)
		wantErr(t, s, "x", "read member roster")
	})
	t.Run("disband delete fails", func(t *testing.T) {
		s, ids := seed(t, 2)
		breakWith(t, s, `CREATE TRIGGER boom BEFORE DELETE ON members BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		wantErr(t, s, ids[0], "disband fleet")
	})
	t.Run("disband cleanup fails", func(t *testing.T) {
		s, ids := seed(t, 2)
		breakWith(t, s, `CREATE TRIGGER boom BEFORE UPDATE ON settings BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		wantErr(t, s, ids[0], "clear auto-sync on disband")
	})
	t.Run("plain delete fails", func(t *testing.T) {
		s, ids := seed(t, 3)
		breakWith(t, s, `CREATE TRIGGER boom BEFORE DELETE ON members BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		wantErr(t, s, ids[0], "delete member")
	})
	t.Run("guards refusing a non-primary maps to membership-changed", func(t *testing.T) {
		// RAISE(IGNORE) makes the disband DELETE silently skip every row: n==0
		// with the target present and not primary, which is exactly what a
		// concurrent roster change looks like from inside the transaction.
		s, ids := seed(t, 2)
		breakWith(t, s, `CREATE TRIGGER boom BEFORE DELETE ON members BEGIN SELECT RAISE(IGNORE); END`)
		if _, _, err := s.DeleteMemberOrDisband(ctx, ids[0]); !errors.Is(err, ErrMembershipChanged) {
			t.Fatalf("err = %v, want ErrMembershipChanged", err)
		}
	})
	t.Run("roster shrinking under a plain delete maps to membership-changed", func(t *testing.T) {
		// The trigger plays the concurrent operator: while the target's DELETE is
		// refused (RAISE(IGNORE)), it removes the other spare rows, so the
		// disambiguation re-read sees a two-member roster where the guarded
		// statement saw four. Retrying would DISBAND, not remove, so the caller
		// must get the look-again refusal rather than a bogus last-active error.
		s, ids := seed(t, 4)
		breakWith(t, s, fmt.Sprintf(
			`CREATE TRIGGER boom BEFORE DELETE ON members BEGIN
				DELETE FROM members WHERE id NOT IN ('%s', '%s');
				SELECT RAISE(IGNORE);
			END`, ids[0], ids[1]))
		if _, _, err := s.DeleteMemberOrDisband(ctx, ids[0]); !errors.Is(err, ErrMembershipChanged) {
			t.Fatalf("err = %v, want ErrMembershipChanged", err)
		}
	})
	t.Run("ghost cleanup fails", func(t *testing.T) {
		s, ids := seed(t, 3)
		// The ghost-state UPDATE only touches a row naming the target, so seed
		// one; the trigger then fires on that row and aborts the statement.
		if err := s.SetFleetSyncState(ctx, ids[0], "m0", time.Now()); err != nil {
			t.Fatalf("seed fleet sync state: %v", err)
		}
		breakWith(t, s, `CREATE TRIGGER boom BEFORE UPDATE ON fleet_sync_state BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		wantErr(t, s, ids[0], "clear ghost fleet state")
	})
}

// TestCommitTxWrapsError covers commitTx's failure wrap via the one commit
// error reachable without a driver seam: committing an already-finished tx.
func TestCommitTxWrapsError(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if err := commitTx(tx, "commit disband"); err == nil || !strings.Contains(err.Error(), "commit disband") {
		t.Fatalf("commitTx on finished tx: err=%v, want wrapped commit error", err)
	}
}

// TestDeleteMemberLastActiveGuard covers the delete door of the routing-pool
// invariant in a fleet big enough to keep existing (3+ members): removing an
// active member is refused when it is the last active one, so drained peers
// plus a delete of the sole active replica cannot empty the Traefik pool.
// Deleting a drained member is always allowed. (At two members the same click
// disbands the fleet instead; see TestDeleteMemberOrDisband_TwoMemberFleet.)
func TestDeleteMemberLastActiveGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pm, err := s.CreateMember(ctx, "primary", "https://p.example.com", "ptok")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	rm, err := s.CreateMember(ctx, "replica", "https://r.example.com", "rtok")
	if err != nil {
		t.Fatalf("create replica: %v", err)
	}
	dm, err := s.CreateMember(ctx, "spare", "https://s.example.com", "stok")
	if err != nil {
		t.Fatalf("create spare: %v", err)
	}
	sm2, err := s.CreateMember(ctx, "spare2", "https://s2.example.com", "")
	if err != nil {
		t.Fatalf("create spare2: %v", err)
	}
	if _, err := s.SetAutoSyncGuarded(ctx, false, pm.ID, true); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	// spare2 only pads the roster above the two-member disband threshold; drain
	// it so it never counts as routing-pool headroom below.
	if err := s.SetMemberState(ctx, sm2.ID, StateDrained); err != nil {
		t.Fatalf("drain spare2: %v", err)
	}

	// A drained member is always removable (it is not in the routing pool), even
	// though two active members remain.
	if err := s.SetMemberState(ctx, dm.ID, StateDrained); err != nil {
		t.Fatalf("drain spare: %v", err)
	}
	if outcome, _, err := s.DeleteMemberOrDisband(ctx, dm.ID); err != nil || outcome != DeleteApplied {
		t.Fatalf("delete drained spare: outcome=%v err=%v, want DeleteApplied", outcome, err)
	}

	// Drain the primary (allowed: the replica stays active), leaving the replica
	// as the only active member of the three remaining.
	if err := s.SetMemberState(ctx, pm.ID, StateDrained); err != nil {
		t.Fatalf("drain primary: %v", err)
	}
	// Deleting that sole active replica would empty the pool: refused.
	if outcome, _, err := s.DeleteMemberOrDisband(ctx, rm.ID); outcome == DeleteApplied || !errors.Is(err, ErrLastActiveMember) {
		t.Fatalf("delete last active replica: outcome=%v err=%v, want ErrLastActiveMember", outcome, err)
	}
	if _, err := s.GetMember(ctx, rm.ID); err != nil {
		t.Errorf("replica should still exist after a refused delete: %v", err)
	}

	// Reactivating the primary restores headroom; the replica then deletes.
	if err := s.SetMemberState(ctx, pm.ID, StateActive); err != nil {
		t.Fatalf("reactivate primary: %v", err)
	}
	if outcome, _, err := s.DeleteMemberOrDisband(ctx, rm.ID); err != nil || outcome != DeleteApplied {
		t.Fatalf("delete replica after reactivate: outcome=%v err=%v, want DeleteApplied", outcome, err)
	}
}

func TestDeleteMemberClearsGhostFleetState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A non-primary member that a stale fleet_sync_state still names as the last
	// primary (the ghost that used to make the badge misreport who was primary).
	gm, err := s.CreateMember(ctx, "ghost", "https://g.example.com", "")
	if err != nil {
		t.Fatalf("create ghost: %v", err)
	}
	// Two more members so deleting the ghost is a plain removal, not a
	// two-member disband (see TestDeleteMemberOrDisband_TwoMemberFleet).
	if _, err := s.CreateMember(ctx, "keep", "https://k.example.com", ""); err != nil {
		t.Fatalf("create keep: %v", err)
	}
	if _, err := s.CreateMember(ctx, "keep2", "https://k2.example.com", ""); err != nil {
		t.Fatalf("create keep2: %v", err)
	}
	if err := s.SetFleetSyncState(ctx, gm.ID, "ghost", time.Now()); err != nil {
		t.Fatalf("seed fleet sync state: %v", err)
	}

	if outcome, _, err := s.DeleteMemberOrDisband(ctx, gm.ID); err != nil || outcome != DeleteApplied {
		t.Fatalf("delete ghost: outcome=%v err=%v", outcome, err)
	}

	st, found, err := s.GetFleetSyncState(ctx)
	if err != nil {
		t.Fatalf("get fleet sync state: %v", err)
	}
	if found && st.PrimaryID == gm.ID {
		t.Errorf("fleet_sync_state still names deleted member %q", gm.ID)
	}
}

// TestMemberNameLengthCapped covers the coupling that makes an unbounded name
// dangerous rather than merely untidy: the primary's name rides in every fleet
// announce, and the receiving handler bounds that body at 1 KiB, so a long
// enough name breaks announces for the whole fleet and does it at debug-log
// volume. Both write paths are capped, and the cap rejects rather than
// truncates, since a member's name identifies it across the fleet.
func TestMemberNameLengthCapped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	long := strings.Repeat("n", maxMemberNameLen+1)
	if _, err := s.CreateMember(ctx, long, "http://member.example.com", "tok"); !errors.Is(err, ErrValidation) {
		t.Errorf("CreateMember with an over-long name err = %v, want ErrValidation", err)
	}

	m, err := s.CreateMember(ctx, strings.Repeat("n", maxMemberNameLen), "http://member.example.com", "tok")
	if err != nil {
		t.Fatalf("CreateMember at exactly the cap: %v", err)
	}

	if err := s.RenameMember(ctx, m.ID, long); !errors.Is(err, ErrValidation) {
		t.Errorf("RenameMember to an over-long name err = %v, want ErrValidation", err)
	}
	stored, err := s.GetMember(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if len(stored.Name) != maxMemberNameLen {
		t.Errorf("stored name len = %d, want the rejected rename to have changed nothing", len(stored.Name))
	}
}

// TestMemberNameFitsAnnounceBudget ties the cap to the constraint it exists for,
// so raising it without revisiting the announce body fails here rather than in
// the field. maxAnnounceBody lives in internal/api and cannot be imported, so
// its value is restated: 1 KiB.
func TestMemberNameFitsAnnounceBudget(t *testing.T) {
	const announceBodyLimit = 1 << 10
	// The announce carries the name plus is_primary, frontdesk_id and the JSON
	// scaffolding; leave that comfortably more room than it needs.
	if maxMemberNameLen > announceBodyLimit/2 {
		t.Errorf("maxMemberNameLen = %d leaves too little of the %d-byte announce budget for the rest of the payload",
			maxMemberNameLen, announceBodyLimit)
	}
}
