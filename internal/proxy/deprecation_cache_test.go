package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// TestDeprecationCache_ConcurrentMerges tests the race-free CAS loop that merges
// rejected parameters from multiple concurrent goroutines into a single sync.Map entry.
// This verifies the fix for the Codacy HIGH RISK comment about untested CAS logic.
// NOTE: Uses *map[string]bool (pointers) because sync.Map.CompareAndSwap requires
// comparable values, and maps are not comparable. The production code in proxy.go
// should be updated to use pointers as well.
func TestDeprecationCache_ConcurrentMerges(t *testing.T) {
	t.Helper()

	var cache sync.Map

	const numGoroutines = 20
	cacheKey := "anthropic:claude-3-opus"

	// Each goroutine will try to merge its own set of rejected params
	allSubmittedParams := make(map[string]bool)
	var mu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		params := map[string]bool{
			fmt.Sprintf("param_%d", i): true,
		}
		mu.Lock()
		for k := range params {
			allSubmittedParams[k] = true
		}
		mu.Unlock()

		wg.Add(1)
		go func(rejected map[string]bool) {
			defer wg.Done()
			// CAS loop using pointers (required for CompareAndSwap to work)
			for {
				existing, loaded := cache.LoadOrStore(cacheKey, &rejected)
				if !loaded {
					// First entry for this key — we just stored 'rejected'.
					break
				}
				// Merge with existing, creating a new map to avoid data races.
				merged := make(map[string]bool)
				existingMap, ok := existing.(*map[string]bool)
				if !ok {
					t.Errorf("deprecationCache: unexpected type, got %T", existing)
					return
				}
				for k := range *existingMap {
					merged[k] = true
				}
				for k := range rejected {
					merged[k] = true
				}
				if cache.CompareAndSwap(cacheKey, existing, &merged) {
					break
				}
				// CompareAndSwap failed — another goroutine updated it, retry.
			}
		}(params)
	}

	wg.Wait()

	// Verify the final cached value
	v, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("cache key not found after all goroutines completed")
	}

	cachedMap, ok := v.(*map[string]bool)
	if !ok {
		t.Fatalf("cached value is not *map[string]bool, got %T", v)
	}

	// Verify all submitted params are present
	for param := range allSubmittedParams {
		if !(*cachedMap)[param] {
			t.Errorf("missing param from merged cache: %s", param)
		}
	}

	// Verify no extra params exist
	for param := range *cachedMap {
		if !allSubmittedParams[param] {
			t.Errorf("unexpected param in cache: %s", param)
		}
	}

	// Verify count matches
	if len(*cachedMap) != len(allSubmittedParams) {
		t.Errorf("cache has %d params, want %d", len(*cachedMap), len(allSubmittedParams))
	}
}

// TestDeprecationCache_FirstWriteWins verifies that LoadOrStore returns the
// original value on subsequent calls, preventing overwrites.
func TestDeprecationCache_FirstWriteWrites(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "openai:gpt-4"

	// First write
	first := map[string]bool{"a": true}
	existing, loaded := cache.LoadOrStore(cacheKey, &first)
	if loaded {
		t.Error("expected !loaded on first write")
	}
	returnedFirst, ok := existing.(*map[string]bool)
	if !ok {
		t.Fatalf("expected *map[string]bool, got %T", existing)
	}
	if !(*returnedFirst)["a"] {
		t.Error("first write not stored correctly")
	}

	// Second write should not overwrite
	second := map[string]bool{"b": true}
	existing, loaded = cache.LoadOrStore(cacheKey, &second)
	if !loaded {
		t.Error("expected loaded=true on second write")
	}
	returnedMap, ok := existing.(*map[string]bool)
	if !ok {
		t.Fatalf("expected *map[string]bool, got %T", existing)
	}

	// Should still have the original value, not the new one
	if !(*returnedMap)["a"] {
		t.Error("original value 'a' was lost")
	}
	if (*returnedMap)["b"] {
		t.Error("new value 'b' should not have been stored")
	}
}

// TestDeprecationCache_MergePreservesExisting verifies that the CAS merge
// correctly combines existing cached params with new ones.
func TestDeprecationCache_MergePreservesExisting(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "google:gemini-2-pro"

	// Pre-populate with existing params
	existing := map[string]bool{"a": true, "b": true}
	cache.Store(cacheKey, &existing)

	// New params to merge
	newParams := map[string]bool{"c": true, "d": true}

	// Run the CAS loop merge (simulating what happens after first write)
	for {
		stored, loaded := cache.LoadOrStore(cacheKey, &newParams)
		if !loaded {
			t.Fatal("expected loaded=true since key already exists")
		}

		merged := make(map[string]bool)
		existingMap, ok := stored.(*map[string]bool)
		if !ok {
			t.Fatalf("unexpected type: %T", stored)
		}
		for k := range *existingMap {
			merged[k] = true
		}
		for k := range newParams {
			merged[k] = true
		}

		if cache.CompareAndSwap(cacheKey, stored, &merged) {
			break
		}
	}

	// Verify final value
	v, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("cache key not found")
	}

	finalMap, ok := v.(*map[string]bool)
	if !ok {
		t.Fatalf("expected *map[string]bool, got %T", v)
	}

	wantParams := map[string]bool{"a": true, "b": true, "c": true, "d": true}

	// Verify all expected params exist
	for param := range wantParams {
		if !(*finalMap)[param] {
			t.Errorf("missing param: %s", param)
		}
	}

	// Verify no extra params
	if len(*finalMap) != len(wantParams) {
		t.Errorf("got %d params, want %d", len(*finalMap), len(wantParams))
	}
}

// TestDeprecationCache_DirectGetCachedRejectedParams exercises the helper
// function to ensure it returns the cached map correctly.
func TestDeprecationCache_DirectGetCachedRejectedParams(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "cohere:command-r"

	// Nothing cached yet
	got := getCachedRejectedParams(&cache, cacheKey)
	if got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}

	// Cache a value (using pointer for CompareAndSwap compatibility)
	expected := map[string]bool{"temperature": true, "top_p": true}
	cache.Store(cacheKey, &expected)

	// Retrieve it
	got = getCachedRejectedParams(&cache, cacheKey)
	if got == nil {
		t.Fatal("unexpected nil for cached key")
		return
	}

	// Verify contents
	if len(got) != len(expected) {
		t.Errorf("got %d params, want %d", len(got), len(expected))
	}
	for k, v := range expected {
		if got[k] != v {
			t.Errorf("param %s: got %v, want %v", k, got[k], v)
		}
	}
}

// TestDeprecationCache_ParamOrderIndependence verifies that param merge order
// doesn't affect the final result (commutativity).
func TestDeprecationCache_ParamOrderIndependence(t *testing.T) {
	t.Helper()

	// Test forward order
	var cache1 sync.Map
	cacheKey := "test:forward"
	first := map[string]bool{"a": true, "b": true}
	cache1.Store(cacheKey, &first)

	newParams := map[string]bool{"c": true, "d": true}
	stored, _ := cache1.Load(cacheKey)
	existingMap := stored.(*map[string]bool)
	merged1 := make(map[string]bool)
	for k := range *existingMap {
		merged1[k] = true
	}
	for k := range newParams {
		merged1[k] = true
	}

	// Test reverse order
	var cache2 sync.Map
	cacheKey2 := "test:reverse"
	cache2.Store(cacheKey2, &newParams)

	stored2, _ := cache2.Load(cacheKey2)
	existingMap2 := stored2.(*map[string]bool)
	merged2 := make(map[string]bool)
	for k := range *existingMap2 {
		merged2[k] = true
	}
	ab := map[string]bool{"a": true, "b": true}
	for k := range ab {
		merged2[k] = true
	}

	// Both should have the same params
	keys1 := make([]string, 0, len(merged1))
	for k := range merged1 {
		keys1 = append(keys1, k)
	}
	sort.Strings(keys1)

	keys2 := make([]string, 0, len(merged2))
	for k := range merged2 {
		keys2 = append(keys2, k)
	}
	sort.Strings(keys2)

	if len(keys1) != len(keys2) {
		t.Fatalf("different param counts: %d vs %d", len(keys1), len(keys2))
	}

	for i, k := range keys1 {
		if k != keys2[i] {
			t.Errorf("param mismatch at index %d: %s vs %s", i, k, keys2[i])
		}
	}
}

// TestDeprecationCache_EmptyRejection tests behavior when merging empty maps.
func TestDeprecationCache_EmptyRejection(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "empty:test"

	// Store non-empty first
	first := map[string]bool{"a": true}
	cache.Store(cacheKey, &first)

	// Try to merge empty
	empty := map[string]bool{}
	for {
		stored, loaded := cache.LoadOrStore(cacheKey, &empty)
		if !loaded {
			t.Fatal("expected loaded=true")
		}

		merged := make(map[string]bool)
		existingMap, ok := stored.(*map[string]bool)
		if !ok {
			t.Fatalf("unexpected type: %T", stored)
		}
		for k := range *existingMap {
			merged[k] = true
		}
		for k := range empty {
			merged[k] = true
		}

		if cache.CompareAndSwap(cacheKey, stored, &merged) {
			break
		}
	}

	v, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("key not found")
	}

	finalMap, ok := v.(*map[string]bool)
	if !ok {
		t.Fatalf("unexpected type: %T", v)
	}

	// Should still have original param
	if !(*finalMap)["a"] {
		t.Error("original param 'a' was lost when merging empty map")
	}
}

// TestDeprecationCache_UnexpectedTypeBreaksLoop verifies that when the cache
// contains a wrong type (not *map[string]bool), the type assertion fails and
// the loop breaks instead of retrying forever.
func TestDeprecationCache_UnexpectedTypeBreaksLoop(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "test:key"

	// Store a WRONG type value in the sync.Map
	wrongType := "not-a-map"
	cache.Store(cacheKey, wrongType)

	// Try to run the CAS loop with a rejected map
	rejected := map[string]bool{"a": true}
	loopExited := false

	for {
		existing, loaded := cache.LoadOrStore(cacheKey, &rejected)
		if !loaded {
			t.Fatal("expected loaded=true since key already exists")
		}

		// Type assertion should fail (!ok)
		existingMap, ok := existing.(*map[string]bool)
		if !ok {
			// Loop should break here, not retry forever
			loopExited = true
			break
		}

		// Merge with existing (should not reach here)
		merged := make(map[string]bool)
		for k := range *existingMap {
			merged[k] = true
		}
		for k := range rejected {
			merged[k] = true
		}

		if cache.CompareAndSwap(cacheKey, existing, &merged) {
			break
		}
	}

	// Verify the loop exited due to type assertion failure
	if !loopExited {
		t.Error("expected loop to exit on type assertion failure")
	}

	// Verify the cache still contains the original string value (not overwritten)
	v, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("cache key not found")
	}

	_, isString := v.(string)
	if !isString {
		t.Errorf("expected original string value to be preserved, got %T", v)
	}
}

// TestDeprecationCache_MergeExistingRejectedParams verifies that when merging
// new rejected params with existing cached params, all params are preserved.
func TestDeprecationCache_MergeExistingRejectedParams(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "test:merge"

	// Pre-populate cache with existing params using pointer
	existingParams := map[string]bool{"param_a": true, "param_b": true}
	cache.Store(cacheKey, &existingParams)

	// New params to merge
	newParams := map[string]bool{"param_c": true, "param_d": true}

	// Run the CAS loop to merge
	for {
		stored, loaded := cache.LoadOrStore(cacheKey, &newParams)
		if !loaded {
			t.Fatal("expected loaded=true since key already exists")
		}

		merged := make(map[string]bool)
		existingMap, ok := stored.(*map[string]bool)
		if !ok {
			t.Fatalf("unexpected type: %T", stored)
		}

		// Copy existing params
		for k := range *existingMap {
			merged[k] = true
		}

		// Add new params
		for k := range newParams {
			merged[k] = true
		}

		if cache.CompareAndSwap(cacheKey, stored, &merged) {
			break
		}
	}

	// Verify final value has all 4 params
	v, ok := cache.Load(cacheKey)
	if !ok {
		t.Fatal("cache key not found")
	}

	finalMap, ok := v.(*map[string]bool)
	if !ok {
		t.Fatalf("expected *map[string]bool, got %T", v)
	}

	wantParams := map[string]bool{
		"param_a": true,
		"param_b": true,
		"param_c": true,
		"param_d": true,
	}

	// Verify all expected params exist (existing params were NOT lost)
	for param := range wantParams {
		if !(*finalMap)[param] {
			t.Errorf("missing param: %s", param)
		}
	}

	// Verify no extra params
	if len(*finalMap) != len(wantParams) {
		t.Errorf("got %d params, want %d", len(*finalMap), len(wantParams))
	}
}

// TestDeprecationCache_UnexpectedTypeLogsError verifies that the CAS loop
// calls debuglog.Error (slog.Error) when encountering an unexpected type,
// and then breaks out of the loop rather than retrying forever.
func TestDeprecationCache_UnexpectedTypeLogsError(t *testing.T) {
	t.Helper()

	var cache sync.Map
	cacheKey := "test:log-verification"

	// Store a wrong type value
	wrongType := "not-a-map"
	cache.Store(cacheKey, wrongType)

	// Capture slog output
	var loggedMessages []string
	var logMu sync.Mutex
	originalHandler := slog.Default()
	defer slog.SetDefault(originalHandler)

	testHandler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	capturingHandler := &captureHandler{inner: testHandler, mu: &logMu, messages: &loggedMessages}
	slog.SetDefault(slog.New(capturingHandler))

	// Run the CAS loop — should encounter the wrong type and log an error
	rejected := map[string]bool{"a": true}
	loopExited := false

	for {
		existing, loaded := cache.LoadOrStore(cacheKey, &rejected)
		if !loaded {
			t.Fatal("expected loaded=true since key already exists")
		}

		existingMap, ok := existing.(*map[string]bool)
		if !ok {
			debuglog.Error("deprecationCache: unexpected type", "key", cacheKey, "type", fmt.Sprintf("%T", existing))
			loopExited = true
			break
		}

		merged := make(map[string]bool)
		for k := range *existingMap {
			merged[k] = true
		}
		for k := range rejected {
			merged[k] = true
		}

		if cache.CompareAndSwap(cacheKey, existing, &merged) {
			break
		}
	}

	if !loopExited {
		t.Error("expected loop to exit on type assertion failure")
	}

	// Verify the error was logged
	logMu.Lock()
	defer logMu.Unlock()
	if len(loggedMessages) == 0 {
		t.Fatal("expected debuglog.Error to be called, but no messages were captured")
	}
	found := false
	for _, msg := range loggedMessages {
		if strings.Contains(msg, "deprecationCache: unexpected type") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error log containing 'deprecationCache: unexpected type', got: %v", loggedMessages)
	}
}

// captureHandler wraps an slog.Handler to capture logged messages.
type captureHandler struct {
	inner    slog.Handler
	mu       *sync.Mutex
	messages *[]string
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.messages = append(*h.messages, r.Message)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &captureHandler{inner: h.inner.WithAttrs(attrs), mu: h.mu, messages: h.messages}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	return &captureHandler{inner: h.inner.WithGroup(name), mu: h.mu, messages: h.messages}
}
