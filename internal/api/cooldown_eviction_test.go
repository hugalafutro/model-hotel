package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAllowQuotaNudgeEvictsStaleStamps: the nudge map is keyed by provider id,
// so without eviction it keeps a row for every provider that ever existed,
// deleted ones included. A stamp past the debounce no longer blocks a nudge and
// must not be retained.
func TestAllowQuotaNudgeEvictsStaleStamps(t *testing.T) {
	h := &Handler{quotaNudgeLast: map[uuid.UUID]time.Time{}}
	now := time.Now()

	stale := make([]uuid.UUID, 50)
	for i := range stale {
		stale[i] = uuid.New()
		h.quotaNudgeLast[stale[i]] = now.Add(-2 * quotaNudgeDebounce)
	}
	fresh := uuid.New()
	h.quotaNudgeLast[fresh] = now

	live := uuid.New()
	if !h.allowQuotaNudge(live, now) {
		t.Fatal("a provider with no stamp is due for a nudge")
	}
	if h.allowQuotaNudge(live, now) {
		t.Error("the stamp just written must debounce the next call")
	}

	if len(h.quotaNudgeLast) != 2 {
		t.Errorf("quotaNudgeLast holds %d stamps, want the fresh one and the new one", len(h.quotaNudgeLast))
	}
	if _, ok := h.quotaNudgeLast[fresh]; !ok {
		t.Error("a stamp still inside the debounce window was evicted")
	}
	if _, ok := h.quotaNudgeLast[stale[0]]; ok {
		t.Error("a stamp past the debounce window was retained")
	}

	// A nil map still initialises and stamps.
	h2 := &Handler{}
	if !h2.allowQuotaNudge(live, now) || len(h2.quotaNudgeLast) != 1 {
		t.Error("a nil map should initialise on first use")
	}
}

// TestRejectConflictEvictsStaleStamps: the conflict debounce is keyed by the
// rejected Front Desk id, which a caller controls, so a stamp past the notify
// interval must be dropped rather than kept for the process lifetime.
func TestRejectConflictEvictsStaleStamps(t *testing.T) {
	h := &FleetHandler{conflictSeen: map[string]time.Time{}}
	old := time.Now().Add(-2 * conflictNotifyInterval)
	for range 50 {
		h.conflictSeen[uuid.NewString()] = old
	}
	fresh := uuid.NewString()
	h.conflictSeen[fresh] = time.Now()

	rejected := uuid.NewString()
	h.rejectConflict("stored-id", rejected)

	h.conflictMu.Lock()
	defer h.conflictMu.Unlock()
	if len(h.conflictSeen) != 2 {
		t.Errorf("conflictSeen holds %d stamps, want the fresh one and the new one", len(h.conflictSeen))
	}
	if _, ok := h.conflictSeen[fresh]; !ok {
		t.Error("a stamp still inside the notify interval was evicted")
	}
	if _, ok := h.conflictSeen[rejected]; !ok {
		t.Error("the rejected id was not stamped")
	}
}
