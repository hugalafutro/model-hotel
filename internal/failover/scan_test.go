package failover

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// mockFailoverRows implements pgx.Rows for testing scanFailoverGroups
// ---------------------------------------------------------------------------

type mockFailoverRows struct {
	rows      [][]any
	index     int
	scanErrOn int // row index at which Scan returns an error (-1 = never)
	rowsErr   error
}

func (m *mockFailoverRows) Close() {}

func (m *mockFailoverRows) Err() error { return m.rowsErr }

func (m *mockFailoverRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (m *mockFailoverRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (m *mockFailoverRows) Next() bool {
	return m.index < len(m.rows)
}

func (m *mockFailoverRows) Scan(dest ...any) error {
	if m.scanErrOn >= 0 && m.index == m.scanErrOn {
		return errors.New("mock scan error")
	}
	if m.index >= len(m.rows) {
		return errors.New("scan called beyond rows")
	}
	row := m.rows[m.index]
	m.index++
	for i, d := range dest {
		if i >= len(row) {
			break
		}
		switch dv := d.(type) {
		case *uuid.UUID:
			*dv = row[i].(uuid.UUID)
		case **string:
			if row[i] == nil {
				*dv = nil
			} else {
				*dv = row[i].(*string)
			}
		case *string:
			*dv = row[i].(string)
		case *[]byte:
			*dv = row[i].([]byte)
		case *bool:
			*dv = row[i].(bool)
		case *time.Time:
			*dv = row[i].(time.Time)
		}
	}
	return nil
}

func (m *mockFailoverRows) Values() ([]any, error) { return nil, nil }

func (m *mockFailoverRows) RawValues() [][]byte { return nil }

func (m *mockFailoverRows) Conn() *pgx.Conn { return nil }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

func mustUUID(s string) uuid.UUID { return uuid.MustParse(s) }

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_SingleRow
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_SingleRow(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	displayName := strPtr("Test Group")
	priority := []uuid.UUID{
		mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
	}
	priorityJSON, err := json.Marshal(priority)
	if err != nil {
		t.Fatalf("marshal priority: %v", err)
	}
	entryEnabled := map[string]bool{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa": true,
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb": false,
	}
	entryEnabledJSON, err := json.Marshal(entryEnabled)
	if err != nil {
		t.Fatalf("marshal entry_enabled: %v", err)
	}
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{
				id1,                // ID
				"gpt-4",            // DisplayModel
				displayName,        // DisplayName (*string)
				"A powerful model", // Description
				priorityJSON,       // priorityJSON
				entryEnabledJSON,   // entryEnabledJSON
				true,               // GroupEnabled
				false,              // AutoCreated
				now,                // CreatedAt
				now,                // UpdatedAt
			},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	fg := groups[0]
	if fg.ID != id1 {
		t.Errorf("ID = %v, want %v", fg.ID, id1)
	}
	if fg.DisplayModel != "gpt-4" {
		t.Errorf("DisplayModel = %q, want %q", fg.DisplayModel, "gpt-4")
	}
	if fg.DisplayName == nil {
		t.Fatal("DisplayName is nil, want non-nil")
	}
	if *fg.DisplayName != "Test Group" {
		t.Errorf("DisplayName = %q, want %q", *fg.DisplayName, "Test Group")
	}
	if fg.Description != "A powerful model" {
		t.Errorf("Description = %q, want %q", fg.Description, "A powerful model")
	}
	if len(fg.PriorityOrder) != 2 {
		t.Fatalf("PriorityOrder length = %d, want 2", len(fg.PriorityOrder))
	}
	if fg.PriorityOrder[0] != priority[0] {
		t.Errorf("PriorityOrder[0] = %v, want %v", fg.PriorityOrder[0], priority[0])
	}
	if fg.PriorityOrder[1] != priority[1] {
		t.Errorf("PriorityOrder[1] = %v, want %v", fg.PriorityOrder[1], priority[1])
	}
	if fg.EntryEnabled["aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"] != true {
		t.Errorf("EntryEnabled[aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa] = false, want true")
	}
	if fg.EntryEnabled["bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"] != false {
		t.Errorf("EntryEnabled[bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb] = true, want false")
	}
	if fg.GroupEnabled != true {
		t.Errorf("GroupEnabled = %v, want true", fg.GroupEnabled)
	}
	if fg.AutoCreated != false {
		t.Errorf("AutoCreated = %v, want false", fg.AutoCreated)
	}
	if !fg.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", fg.CreatedAt, now)
	}
	if !fg.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", fg.UpdatedAt, now)
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_MultipleRows
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_MultipleRows(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	id2 := mustUUID("22222222-2222-2222-2222-222222222222")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	emptyPriority := []byte(`[]`)
	emptyEntry := []byte(`{}`)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "model-a", strPtr("A"), "desc a", emptyPriority, emptyEntry, true, false, now, now},
			{id2, "model-b", strPtr("B"), "desc b", emptyPriority, emptyEntry, false, true, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	if groups[0].ID != id1 {
		t.Errorf("groups[0].ID = %v, want %v", groups[0].ID, id1)
	}
	if groups[1].ID != id2 {
		t.Errorf("groups[1].ID = %v, want %v", groups[1].ID, id2)
	}
	if groups[0].DisplayModel != "model-a" {
		t.Errorf("groups[0].DisplayModel = %q, want %q", groups[0].DisplayModel, "model-a")
	}
	if groups[1].DisplayModel != "model-b" {
		t.Errorf("groups[1].DisplayModel = %q, want %q", groups[1].DisplayModel, "model-b")
	}
	if groups[0].GroupEnabled != true {
		t.Errorf("groups[0].GroupEnabled = %v, want true", groups[0].GroupEnabled)
	}
	if groups[1].GroupEnabled != false {
		t.Errorf("groups[1].GroupEnabled = %v, want false", groups[1].GroupEnabled)
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_EmptyRows
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_EmptyRows(t *testing.T) {
	rows := &mockFailoverRows{
		rows:      [][]any{},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_ScanError
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_ScanError(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "model-a", strPtr("A"), "desc", []byte(`[]`), []byte(`{}`), true, false, now, now},
		},
		scanErrOn: 0,
	}

	groups, err := scanFailoverGroups(rows)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	if !strings.Contains(err.Error(), "scanFailoverGroups: row scan failed") {
		t.Errorf("error message does not contain expected prefix: %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups on error, got %d groups", len(groups))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_InvalidPriorityJSON
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_InvalidPriorityJSON(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "model-a", strPtr("A"), "desc", []byte(`{invalid`), []byte(`{}`), true, false, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	if !strings.Contains(err.Error(), "unmarshal priority") {
		t.Errorf("error message does not contain 'unmarshal priority': %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups on error, got %d groups", len(groups))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_InvalidEntryEnabledJSON
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_InvalidEntryEnabledJSON(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "model-a", strPtr("A"), "desc", []byte(`[]`), []byte(`{invalid`), true, false, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err == nil {
		t.Fatal("expected error, got nil")
		return
	}
	if !strings.Contains(err.Error(), "unmarshal entry_enabled") {
		t.Errorf("error message does not contain 'unmarshal entry_enabled': %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups on error, got %d groups", len(groups))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_NilDisplayName
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_NilDisplayName(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "model-a", (*string)(nil), "desc", []byte(`[]`), []byte(`{}`), true, false, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].DisplayName != nil {
		t.Errorf("DisplayName = %v, want nil", *groups[0].DisplayName)
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_RowsErr
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_RowsErr(t *testing.T) {
	rows := &mockFailoverRows{
		rows:      [][]any{},
		scanErrOn: -1,
		rowsErr:   errors.New("connection lost"),
	}

	groups, err := scanFailoverGroups(rows)
	if err == nil {
		t.Fatal("expected error from rows.Err(), got nil")
	}
	if !strings.Contains(err.Error(), "iteration error") {
		t.Errorf("error should contain 'iteration error', got: %v", err)
	}
	if groups != nil {
		t.Errorf("expected nil groups on rows.Err(), got %d groups", len(groups))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_EmptyPriorityAndEntryEnabled
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_EmptyPriorityAndEntryEnabled(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "empty-group", strPtr("Empty"), "", []byte(`[]`), []byte(`{}`), true, false, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].PriorityOrder) != 0 {
		t.Errorf("expected empty PriorityOrder, got %d entries", len(groups[0].PriorityOrder))
	}
	if len(groups[0].EntryEnabled) != 0 {
		t.Errorf("expected empty EntryEnabled, got %d entries", len(groups[0].EntryEnabled))
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_GroupEnabledFalse
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_GroupEnabledFalse(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "disabled-group", strPtr("Disabled"), "", []byte(`[]`), []byte(`{}`), false, true, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].GroupEnabled != false {
		t.Error("expected GroupEnabled=false")
	}
	if groups[0].AutoCreated != true {
		t.Error("expected AutoCreated=true")
	}
}

// ---------------------------------------------------------------------------
// TestScanFailoverGroups_EntryEnabledPreservesValues
// ---------------------------------------------------------------------------

func TestScanFailoverGroups_EntryEnabledPreservesValues(t *testing.T) {
	id1 := mustUUID("11111111-1111-1111-1111-111111111111")
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	modelA := mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	modelB := mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	priority := []uuid.UUID{modelA, modelB}
	priorityJSON, _ := json.Marshal(priority)
	entryEnabled := map[string]bool{
		modelA.String(): true,
		modelB.String(): false,
	}
	entryEnabledJSON, _ := json.Marshal(entryEnabled)

	rows := &mockFailoverRows{
		rows: [][]any{
			{id1, "mixed-entries", strPtr("Mixed"), "", priorityJSON, entryEnabledJSON, true, false, now, now},
		},
		scanErrOn: -1,
	}

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].EntryEnabled[modelA.String()] != true {
		t.Error("expected EntryEnabled[modelA]=true")
	}
	if groups[0].EntryEnabled[modelB.String()] != false {
		t.Error("expected EntryEnabled[modelB]=false")
	}
}
