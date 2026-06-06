package failover

import (
	"testing"

	"github.com/google/uuid"
)

func TestMergePriorityOrder_EmptyInputs(t *testing.T) {
	result := mergePriorityOrder(nil, nil)
	if len(result) != 0 {
		t.Errorf("Expected empty, got %v", result)
	}

	result = mergePriorityOrder([]uuid.UUID{}, []uuid.UUID{})
	if len(result) != 0 {
		t.Errorf("Expected empty, got %v", result)
	}
}

func TestMergePriorityOrder_NoExistingOrder(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	result := mergePriorityOrder(nil, []uuid.UUID{id1, id2})
	if len(result) != 2 {
		t.Fatalf("Expected 2, got %d", len(result))
	}
	if result[0] != id1 || result[1] != id2 {
		t.Errorf("Expected [id1, id2], got %v", result)
	}
}

func TestMergePriorityOrder_PreservesExistingOrder(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	// Existing order: 2, 1 (user reordered), currentIDs: 1, 2, 3
	result := mergePriorityOrder([]uuid.UUID{id2, id1}, []uuid.UUID{id1, id2, id3})
	if len(result) != 3 {
		t.Fatalf("Expected 3, got %d", len(result))
	}
	// id2 comes first because it was first in existing order
	if result[0] != id2 || result[1] != id1 || result[2] != id3 {
		t.Errorf("Expected [id2, id1, id3], got %v", result)
	}
}

func TestMergePriorityOrder_AppendsNewEntries(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	// Existing only knows id1, currentIDs adds id2, id3
	result := mergePriorityOrder([]uuid.UUID{id1}, []uuid.UUID{id1, id2, id3})
	if len(result) != 3 {
		t.Fatalf("Expected 3, got %d", len(result))
	}
	if result[0] != id1 || result[1] != id2 || result[2] != id3 {
		t.Errorf("Expected [id1, id2, id3], got %v", result)
	}
}

func TestMergePriorityOrder_RemovesStaleEntries(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	// Existing order includes id3 which is no longer in currentIDs
	result := mergePriorityOrder([]uuid.UUID{id3, id1}, []uuid.UUID{id1, id2})
	if len(result) != 2 {
		t.Fatalf("Expected 2, got %d", len(result))
	}
	// id3 is stale so only id1 remains from existing order
	if result[0] != id1 || result[1] != id2 {
		t.Errorf("Expected [id1, id2], got %v", result)
	}
}

func TestMergePriorityOrder_DeduplicatesExistingOrder(t *testing.T) {
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	// Duplicate id1 in existingOrder — should only appear once in result
	result := mergePriorityOrder([]uuid.UUID{id1, id1, id2}, []uuid.UUID{id1, id2})
	if len(result) != 2 {
		t.Fatalf("Expected 2, got %d", len(result))
	}
	if result[0] != id1 || result[1] != id2 {
		t.Errorf("Expected [id1, id2], got %v", result)
	}
}
