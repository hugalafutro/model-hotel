package frontdesk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// A tokened member nobody answers for (a port that was listening and is
	// closed again, so the refusal is immediate): no ledger, and no event,
	// since it never had one to lose.
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	dead, _ := store.CreateMember(ctx, "dead", goneURL, "tok")
	// A member whose status response is past the size bound: no ledger, and
	// the one failure the poll reports above Debug.
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"providers":[{"provider_id":"p","circuits":[{"model":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxCircuitStatusBytes))
		_, _ = w.Write([]byte(`","state":"open"}]}]}`))
	}))
	defer huge.Close()
	oversize, _ := store.CreateMember(ctx, "oversize", huge.URL, "tok")
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
	if got == nil || !got.CheckedAt.Equal(fixed) || len(got.Open) != 2 || got.Total != 2 {
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
	if snap[oversize.ID].Circuits != nil {
		t.Errorf("oversize member has a ledger: %+v", snap[oversize.ID].Circuits)
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

	// A member whose token is removed loses its ledger with it, once.
	fail.Store(false)
	p.PollCircuitsOnce(ctx)
	if p.Snapshot()[withTok.ID].Circuits == nil {
		t.Fatal("setup: the ledger did not come back")
	}
	statusEvents()
	if err := store.SetMemberToken(ctx, withTok.ID, ""); err != nil {
		t.Fatalf("remove token: %v", err)
	}
	p.PollCircuitsOnce(ctx)
	if p.Snapshot()[withTok.ID].Circuits != nil {
		t.Error("a member without a token kept its ledger")
	}
	if n := statusEvents(); n != 1 {
		t.Errorf("status events after the token was removed = %d, want 1", n)
	}

	// A token the store holds but cannot decrypt (a master key rotated
	// underneath it) is the same as no token: the ledger goes, once.
	if err := store.SetMemberToken(ctx, withTok.ID, "tok"); err != nil {
		t.Fatalf("restore token: %v", err)
	}
	p.PollCircuitsOnce(ctx)
	if p.Snapshot()[withTok.ID].Circuits == nil {
		t.Fatal("setup: the ledger did not come back")
	}
	statusEvents()
	corruptMemberToken(t, store, withTok.ID)
	p.PollCircuitsOnce(ctx)
	if p.Snapshot()[withTok.ID].Circuits != nil {
		t.Error("a member whose token cannot be read kept its ledger")
	}
	if n := statusEvents(); n != 1 {
		t.Errorf("status events after the token became unreadable = %d, want 1", n)
	}
}

// corruptMemberToken overwrites a member's stored token with bytes that are
// not a ciphertext, so HasToken stays true while MemberToken fails.
func corruptMemberToken(t *testing.T, store *Store, id string) {
	t.Helper()
	junk := []byte("not a ciphertext")
	if _, err := store.db.ExecContext(context.Background(), `UPDATE members SET token_cipher = ?, token_nonce = ?, token_salt = ? WHERE id = ?`, junk, junk, junk, id); err != nil {
		t.Fatalf("corrupt token: %v", err)
	}
}

// The hover list is capped; the count is not. A response past the size bound
// is its own error, not a parse failure.
func TestFetchMemberCircuits_CapAndSizeBound(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"providers":[{"provider_id":"p","provider_name":"P","state":"open","circuits":[`)
	for i := range maxOpenCircuits + 7 {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"model":"m%d","state":"open","consecutive_fails":5,"last_cause":"upstream status 503"}`, i)
	}
	sb.WriteString(`]}]}`)
	many := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer many.Close()
	p, _, _ := newTestPoller(t, "")
	ledger, err := p.fetchMemberCircuits(context.Background(), many.URL, "tok")
	if err != nil || len(ledger.Open) != maxOpenCircuits || ledger.Total != maxOpenCircuits+7 || ledger.Open[maxOpenCircuits-1].Model != fmt.Sprintf("m%d", maxOpenCircuits-1) {
		t.Errorf("capped ledger = %+v (%v), want the first %d listed and all %d counted", ledger, err, maxOpenCircuits, maxOpenCircuits+7)
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"providers":[{"provider_id":"p","circuits":[{"model":"`))
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxCircuitStatusBytes))
		_, _ = w.Write([]byte(`","state":"open"}]}]}`))
	}))
	defer huge.Close()
	if _, err := p.fetchMemberCircuits(context.Background(), huge.URL, "tok"); !errors.Is(err, errCircuitStatusTooLarge) {
		t.Errorf("oversize response error = %v, want errCircuitStatusTooLarge", err)
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
	ledger, err := p.fetchMemberCircuits(context.Background(), srv.URL, "tok")
	if err != nil || ledger == nil || len(ledger.Open) != 0 || ledger.Total != 0 {
		t.Errorf("older member ledger = %+v, %v; want an empty ledger", ledger, err)
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
