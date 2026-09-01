package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const memberCircuitsBody = `{"enabled":true,"closed":1,"half_open":1,"open":1,"providers":[
 {"provider_id":"p-zai","provider_name":"Z.ai","state":"open","circuits":[
   {"model":"glm-5.3","state":"open","consecutive_fails":5,"next_retry_at":"2026-08-31T18:41:00Z","quota_pinned":true,"pin_source":"advisor","last_cause":"upstream status 429 (exhausted)","last_status":429},
   {"model":"glm-4.6","state":"closed","consecutive_fails":0,"last_cause":"success","last_status":200}]},
 {"provider_id":"p-nw","provider_name":"Neuralwatt","state":"half-open","circuits":[
   {"model":"glm-5.3","state":"half-open","consecutive_fails":5,"last_cause":"upstream status 503","last_status":503}]}]}`

// The circuits poll keeps a member's non-closed circuits with their causes,
// leaves a tokenless member without a ledger, drops the ledger of a member
// whose read failed rather than showing a stale one, and refreshes the UI
// only when the set changes.
func TestPollCircuitsOnce(t *testing.T) {
	var fail atomic.Bool
	var gotPath, gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.RequestURI())
		gotAuth.Store(r.Header.Get("Authorization"))
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(memberCircuitsBody))
	}))
	defer srv.Close()

	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	fixed := time.Date(2026, 8, 31, 14, 47, 0, 0, time.UTC)
	p.now = func() time.Time { return fixed }
	withTok, _ := store.CreateMember(ctx, "wt", srv.URL, "tok")
	noTok, _ := store.CreateMember(ctx, "nt", "http://127.0.0.1:9", "")
	// A tokened member nobody answers for: no ledger, and no event, since it
	// never had one to lose.
	dead, _ := store.CreateMember(ctx, "dead", "http://127.0.0.1:2", "tok")
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	statusEvents := func() int {
		n := 0
		for {
			select {
			case ev := <-ch:
				if ev.Type == "member.status" {
					n++
				}
			default:
				return n
			}
		}
	}

	p.PollCircuitsOnce(ctx)
	if gotPath.Load() != memberCircuitsPath || gotAuth.Load() != "Bearer tok" {
		t.Errorf("member asked %v with %v, want the detail status with the member's token", gotPath.Load(), gotAuth.Load())
	}
	snap := p.Snapshot()
	got := snap[withTok.ID].Circuits
	if got == nil || !got.CheckedAt.Equal(fixed) || len(got.Open) != 2 {
		t.Fatalf("ledger = %+v, want two non-closed circuits stamped at the poll", got)
	}
	if c := got.Open[0]; c.Provider != "Z.ai" || c.Model != "glm-5.3" || c.State != "open" || c.Cause != "upstream status 429 (exhausted)" || c.Status != 429 || !c.QuotaPinned || c.PinSource != "advisor" || c.NextRetryAt != "2026-08-31T18:41:00Z" {
		t.Errorf("open circuit = %+v", c)
	}
	if c := got.Open[1]; c.Provider != "Neuralwatt" || c.State != "half-open" || c.Cause != "upstream status 503" {
		t.Errorf("half-open circuit = %+v", c)
	}
	if snap[noTok.ID].Circuits != nil {
		t.Errorf("tokenless member has a ledger: %+v", snap[noTok.ID].Circuits)
	}
	if snap[dead.ID].Circuits != nil {
		t.Errorf("unreachable member has a ledger: %+v", snap[dead.ID].Circuits)
	}
	if n := statusEvents(); n != 1 {
		t.Errorf("status events after the first read = %d, want 1", n)
	}

	// The same ledger again: nothing to tell the UI.
	p.PollCircuitsOnce(ctx)
	if n := statusEvents(); n != 0 {
		t.Errorf("status events after an unchanged read = %d, want 0", n)
	}

	// A failed read drops the ledger (fail closed) and says so once.
	fail.Store(true)
	p.PollCircuitsOnce(ctx)
	if p.Snapshot()[withTok.ID].Circuits != nil {
		t.Error("a failed read kept the previous ledger")
	}
	if n := statusEvents(); n != 1 {
		t.Errorf("status events after the read failed = %d, want 1", n)
	}
	p.PollCircuitsOnce(ctx)
	if n := statusEvents(); n != 0 {
		t.Errorf("status events after a second failed read = %d, want 0", n)
	}
}

// A member whose rows carry no circuits[] (from before the field existed) is
// an empty ledger, not an error and not a provider-wide guess.
func TestFetchMemberCircuits_OlderMember(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"providers":[{"provider_id":"p","state":"open","open_models":["m"]}]}`))
	}))
	defer srv.Close()
	p, _, _ := newTestPoller(t, "")
	open, err := p.fetchMemberCircuits(context.Background(), srv.URL, "tok")
	if err != nil || open == nil || len(open) != 0 {
		t.Errorf("older member ledger = %v, %v; want an empty, non-nil ledger", open, err)
	}

	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer garbled.Close()
	if _, err := p.fetchMemberCircuits(context.Background(), garbled.URL, "tok"); err == nil {
		t.Error("a garbled body was accepted")
	}
	if _, err := p.fetchMemberCircuits(context.Background(), "http://bad host\x7f", "tok"); err == nil {
		t.Error("an unbuildable member URL was accepted")
	}
}
