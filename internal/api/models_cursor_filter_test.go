package api

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}
	return ts
}

func TestBuildModelFilterConditions_Empty(t *testing.T) {
	conditions, args := buildModelFilterConditions(url.Values{})
	if len(conditions) != 0 {
		t.Errorf("Expected no conditions, got %d", len(conditions))
	}
	if len(args) != 0 {
		t.Errorf("Expected no args, got %d", len(args))
	}
}

func TestBuildModelFilterConditions_Search(t *testing.T) {
	conditions, args := buildModelFilterConditions(url.Values{"search": {"gpt"}})
	if len(conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(conditions))
	}
	if len(args) != 1 {
		t.Fatalf("Expected 1 arg, got %d", len(args))
	}
	if args[0] != "%gpt%" {
		t.Errorf("Expected %%gpt%%, got %v", args[0])
	}
	if conditions[0] == "" {
		t.Error("Expected non-empty condition")
	}
}

func TestBuildModelFilterConditions_SingleProviderID(t *testing.T) {
	pid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	conditions, args := buildModelFilterConditions(url.Values{"provider_id": {pid.String()}})
	if len(conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(conditions))
	}
	if len(args) != 1 {
		t.Fatalf("Expected 1 arg, got %d", len(args))
	}
	if args[0] != pid {
		t.Errorf("Expected %v, got %v", pid, args[0])
	}
}

func TestBuildModelFilterConditions_MultipleProviderIDs(t *testing.T) {
	pid1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	pid2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	conditions, args := buildModelFilterConditions(url.Values{"provider_id": {pid1.String() + "," + pid2.String()}})
	if len(conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(conditions))
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}
	if args[0] != pid1 || args[1] != pid2 {
		t.Errorf("Expected [pid1, pid2], got %v", args)
	}
}

func TestBuildModelFilterConditions_Capabilities(t *testing.T) {
	conditions, args := buildModelFilterConditions(url.Values{"capabilities": {"tool_call,vision"}})
	if len(conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(conditions))
	}
	if len(args) != 1 {
		t.Fatalf("Expected 1 arg, got %d", len(args))
	}
	// Should be a JSON object like {"tool_call":true,"vision":true}
	argStr, ok := args[0].(string)
	if !ok {
		t.Fatalf("Expected string arg, got %T", args[0])
	}
	if argStr == "" {
		t.Error("Expected non-empty capabilities JSON")
	}
}

func TestBuildModelFilterConditions_InvalidProviderID(t *testing.T) {
	conditions, args := buildModelFilterConditions(url.Values{"provider_id": {"not-a-uuid"}})
	if len(conditions) != 0 {
		t.Errorf("Expected no conditions for invalid UUID, got %d", len(conditions))
	}
	if len(args) != 0 {
		t.Errorf("Expected no args for invalid UUID, got %d", len(args))
	}
}

func TestBuildModelFilterConditions_Combined(t *testing.T) {
	pid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	conditions, args := buildModelFilterConditions(url.Values{
		"search":      {"gpt"},
		"provider_id": {pid.String()},
	})
	if len(conditions) != 2 {
		t.Fatalf("Expected 2 conditions, got %d", len(conditions))
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}
}

func TestBuildModelKeysetPredicate_EmptyCursor(t *testing.T) {
	argIdx := 1
	var args []interface{}
	pred := buildModelKeysetPredicate(modelCursor{}, "after", "ASC", &argIdx, &args)
	if pred != "" {
		t.Errorf("Expected empty predicate for empty cursor, got %q", pred)
	}
}

func TestBuildModelKeysetPredicate_NameSort(t *testing.T) {
	argIdx := 1
	var args []interface{}
	cursor := modelCursor{
		ID:      "test-id",
		ModelID: "gpt-4",
		SortBy:  "name",
	}
	pred := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if pred == "" {
		t.Error("Expected non-empty predicate")
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}
	if argIdx != 3 {
		t.Errorf("Expected argIdx=3, got %d", argIdx)
	}
}

func TestBuildModelKeysetPredicate_DESCAfterUsesLessThan(t *testing.T) {
	argIdx := 1
	var args []interface{}
	cursor := modelCursor{
		ID:      "test-id",
		ModelID: "gpt-4",
		SortBy:  "name",
	}
	pred := buildModelKeysetPredicate(cursor, "after", "DESC", &argIdx, &args)
	if pred == "" {
		t.Fatal("Expected non-empty predicate")
	}
	// DESC + after → "<" operator
	if !containsOp(pred, "<") {
		t.Errorf("Expected '<' operator for DESC+after, got %q", pred)
	}
}

func TestBuildModelKeysetPredicate_ASCAfterUsesGreaterThan(t *testing.T) {
	argIdx := 1
	var args []interface{}
	cursor := modelCursor{
		ID:      "test-id",
		ModelID: "gpt-4",
		SortBy:  "name",
	}
	pred := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if pred == "" {
		t.Fatal("Expected non-empty predicate")
	}
	// ASC + after → ">" operator
	if !containsOp(pred, ">") {
		t.Errorf("Expected '>' operator for ASC+after, got %q", pred)
	}
}

func containsOp(s, op string) bool {
	for i := 0; i < len(s); i++ {
		if string(s[i]) == op {
			return true
		}
	}
	return false
}

func TestBuildModelKeysetPredicate_DiscoveredSort(t *testing.T) {
	argIdx := 1
	var args []interface{}
	ts := mustParseTime(t, "2024-01-01T00:00:00Z")
	cursor := modelCursor{
		ID:         "test-id",
		SortBy:     "discovered",
		LastSeenAt: ts,
	}
	pred := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if pred == "" {
		t.Error("Expected non-empty predicate")
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args (LastSeenAt, ID), got %d", len(args))
	}
}

func TestBuildModelKeysetPredicate_ContextSort(t *testing.T) {
	argIdx := 1
	var args []interface{}
	ctxLen := 128000
	cursor := modelCursor{
		ID:            "test-id",
		SortBy:        "context",
		ContextLength: &ctxLen,
	}
	pred := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if pred == "" {
		t.Error("Expected non-empty predicate")
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}
}

func TestBuildModelKeysetPredicate_StatusSort(t *testing.T) {
	argIdx := 1
	var args []interface{}
	statusSort := 0
	cursor := modelCursor{
		ID:         "test-id",
		SortBy:     "status",
		StatusSort: &statusSort,
	}
	pred := buildModelKeysetPredicate(cursor, "after", "ASC", &argIdx, &args)
	if pred == "" {
		t.Error("Expected non-empty predicate")
	}
	if len(args) != 2 {
		t.Fatalf("Expected 2 args, got %d", len(args))
	}
	// Should contain CASE expression for status
	if !containsSubstring(pred, "CASE") {
		t.Errorf("Expected CASE in status predicate, got %q", pred)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstringHelper(s, sub))
}

func containsSubstringHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSplitComma(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"", []string{}},
		{",,,", []string{}},
		{"single", []string{"single"}},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := splitComma(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("splitComma(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitComma(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestJoinAnd(t *testing.T) {
	tests := []struct {
		conditions []string
		want       string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a AND b"},
		{[]string{"a", "b", "c"}, "a AND b AND c"},
	}
	for _, tc := range tests {
		name := fmt.Sprintf("%v", tc.conditions)
		t.Run(name, func(t *testing.T) {
			if got := joinAnd(tc.conditions); got != tc.want {
				t.Errorf("joinAnd(%v) = %q, want %q", tc.conditions, got, tc.want)
			}
		})
	}
}

func TestModelSortColumn(t *testing.T) {
	tests := []struct {
		sortBy string
		want   string
	}{
		{"discovered", "COALESCE(m.last_seen_at, m.created_at)"},
		{"context", "COALESCE(m.context_length, 0)"},
		{"output", "COALESCE(m.max_output_tokens, 0)"},
		{"provider", "COALESCE(p.name, '')"},
		{"name", "COALESCE(m.name, m.model_id, '')"},
		{"", "COALESCE(m.name, m.model_id, '')"},
		{"unknown", "COALESCE(m.name, m.model_id, '')"},
	}
	for _, tc := range tests {
		t.Run(tc.sortBy, func(t *testing.T) {
			if got := modelSortColumn(tc.sortBy); got != tc.want {
				t.Errorf("modelSortColumn(%q) = %q, want %q", tc.sortBy, got, tc.want)
			}
		})
	}
}

// Verify status sort uses CASE expression
func TestModelSortColumn_Status(t *testing.T) {
	got := modelSortColumn("status")
	if !containsSubstringHelper(got, "CASE") {
		t.Errorf("Expected CASE in status sort column, got %q", got)
	}
}
