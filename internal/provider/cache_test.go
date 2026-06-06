package provider

import (
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// IsCachedByID
// ---------------------------------------------------------------------------

func TestIsCachedByID_EmptyCache(t *testing.T) {
	InvalidateProviderCache()
	if IsCachedByID(uuid.New()) {
		t.Error("IsCachedByID should return false for empty cache")
	}
}

func TestIsCachedByID_Cached(t *testing.T) {
	InvalidateProviderCache()
	id := uuid.New()
	WarmProviderCache([]*Provider{{ID: id, Name: "test-provider"}})
	if !IsCachedByID(id) {
		t.Error("IsCachedByID should return true for cached provider")
	}
}

func TestIsCachedByID_Miss(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "test-provider"}})
	if IsCachedByID(uuid.New()) {
		t.Error("IsCachedByID should return false for different ID")
	}
}

// ---------------------------------------------------------------------------
// IsCachedByName
// ---------------------------------------------------------------------------

func TestIsCachedByName_EmptyCache(t *testing.T) {
	InvalidateProviderCache()
	if IsCachedByName("nonexistent") {
		t.Error("IsCachedByName should return false for empty cache")
	}
}

func TestIsCachedByName_Cached(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "My Provider"}})
	if !IsCachedByName("My Provider") {
		t.Error("IsCachedByName should return true for exact name match")
	}
}

func TestIsCachedByName_NormalizedName(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "My Provider"}})
	if !IsCachedByName("My-Provider") {
		t.Error("IsCachedByName should return true for normalized name match")
	}
}

func TestIsCachedByName_Miss(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "test-provider"}})
	if IsCachedByName("other-provider") {
		t.Error("IsCachedByName should return false for different name")
	}
}
