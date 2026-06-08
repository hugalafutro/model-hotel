package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

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

// ---------------------------------------------------------------------------
// GetCachedByID
// ---------------------------------------------------------------------------

func TestGetCachedByID_CacheHit(t *testing.T) {
	InvalidateProviderCache()
	id := uuid.New()
	WarmProviderCache([]*Provider{{ID: id, Name: "cached-provider"}})
	p, ok := GetCachedByID(id)
	if !ok {
		t.Fatal("GetCachedByID should return true for cached provider")
	}
	if p.Name != "cached-provider" {
		t.Errorf("expected name 'cached-provider', got %q", p.Name)
	}
}

func TestGetCachedByID_CacheMiss(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "other-provider"}})
	_, ok := GetCachedByID(uuid.New())
	if ok {
		t.Error("GetCachedByID should return false for non-cached ID")
	}
}

func TestGetCachedByID_EmptyCache(t *testing.T) {
	InvalidateProviderCache()
	_, ok := GetCachedByID(uuid.New())
	if ok {
		t.Error("GetCachedByID should return false for empty cache")
	}
}

// ---------------------------------------------------------------------------
// GetCachedByName
// ---------------------------------------------------------------------------

func TestGetCachedByName_CacheHit(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "My Provider"}})
	p, ok := GetCachedByName("My Provider")
	if !ok {
		t.Fatal("GetCachedByName should return true for cached name")
	}
	if p.Name != "My Provider" {
		t.Errorf("expected name 'My Provider', got %q", p.Name)
	}
}

func TestGetCachedByName_NormalizedNameHit(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "My Provider"}})
	p, ok := GetCachedByName("My-Provider")
	if !ok {
		t.Fatal("GetCachedByName should return true for normalized name")
	}
	if p.Name != "My Provider" {
		t.Errorf("expected name 'My Provider', got %q", p.Name)
	}
}

func TestGetCachedByName_CacheMiss(t *testing.T) {
	InvalidateProviderCache()
	WarmProviderCache([]*Provider{{ID: uuid.New(), Name: "test-provider"}})
	_, ok := GetCachedByName("nonexistent")
	if ok {
		t.Error("GetCachedByName should return false for missing name")
	}
}

// ---------------------------------------------------------------------------
// NewDiscoveryService / NewDiscoveryServiceWithHTTPClient
// ---------------------------------------------------------------------------

func TestNewDiscoveryServiceWithHTTPClient_NilClient(t *testing.T) {
	ds := NewDiscoveryServiceWithHTTPClient(nil)
	if ds == nil {
		t.Fatal("expected non-nil DiscoveryService")
	}
}

func TestNewDiscoveryServiceWithHTTPClient_CustomClient(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	ds := NewDiscoveryServiceWithHTTPClient(client)
	if ds == nil {
		t.Fatal("expected non-nil DiscoveryService")
	}
	if ds.httpClient != client {
		t.Error("expected httpClient to be the passed client")
	}
}

func TestNewDiscoveryService_NilParams(t *testing.T) {
	ds := NewDiscoveryService(nil, nil)
	if ds == nil {
		t.Fatal("expected non-nil DiscoveryService")
	}
}

func TestNewDiscoveryService_WithDialCtx(t *testing.T) {
	dialCalled := false
	dialCtx := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialCalled = true
		return nil, fmt.Errorf("test dial error")
	}
	ds := NewDiscoveryService(dialCtx, nil)
	if ds == nil {
		t.Fatal("expected non-nil DiscoveryService")
	}
	// The DialContext is set on the transport. Verify via call.
	_, err := ds.httpClient.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "localhost:80")
	if !dialCalled {
		t.Error("expected DialContext to be called")
	}
	if err == nil {
		t.Error("expected dial error from test function")
	}
}

func TestNewDiscoveryService_WithCheckRedirect(t *testing.T) {
	redirectChecked := false
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		redirectChecked = true
		return http.ErrUseLastResponse
	}
	ds := NewDiscoveryService(nil, checkRedirect)
	if ds == nil {
		t.Fatal("expected non-nil DiscoveryService")
	}
	if ds.httpClient.CheckRedirect == nil {
		t.Fatal("expected CheckRedirect to be set")
	}
	// Verify the function works by calling it directly
	err := ds.httpClient.CheckRedirect(&http.Request{}, []*http.Request{})
	if !redirectChecked {
		t.Error("expected CheckRedirect to be called")
	}
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("expected ErrUseLastResponse, got %v", err)
	}
}
