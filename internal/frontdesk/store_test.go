package frontdesk

import (
	"context"
	"errors"
	"path/filepath"
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

	updated := Settings{
		HealthPollSecs: 10, TraefikPollSecs: 7, TraefikStaleSecs: 60,
		EventRetentionDays: 30, RetryAttempts: 0,
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
		{HealthPollSecs: 0, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: 1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 0, RetryAttempts: 1},
		{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 5, EventRetentionDays: 1, RetryAttempts: -1},
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

func TestEventsPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
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
