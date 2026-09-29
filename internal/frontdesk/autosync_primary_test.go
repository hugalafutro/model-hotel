package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPrimaryVerdictForReplacedPrimaryIsDropped: the old primary's read is in
// flight when the auto-sync is repointed. Its late refusal of the stored token
// (which marks the auto-sync idle at once) is an answer about a setup that no
// longer exists, so it neither marks the new primary's auto-sync idle nor
// counts as one of its failures.
func TestPrimaryVerdictForReplacedPrimaryIsDropped(t *testing.T) {
	srv, store := newTestServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(old.Close)
	a, _ := store.CreateMember(t.Context(), "old", old.URL, "atoken")
	b, _ := store.CreateMember(t.Context(), "new", "http://127.0.0.1:9", "btoken")
	enableAutoSync(t, store, a.ID)
	cfg, err := store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.primaryConfigHash(t.Context(), cfg)
	}()
	<-entered
	enableAutoSync(t, store, b.ID)
	srv.clearAutoSyncIdle(t.Context()) // what the repointing PUT does
	close(release)
	<-done

	if got := srv.poller.autoSyncIdle(); !got.IsZero() {
		t.Errorf("the replaced primary's late refusal marked the auto-sync idle at %v", got)
	}
	srv.idleMu.Lock()
	failures := srv.primaryReadFailures
	srv.idleMu.Unlock()
	if failures != 0 {
		t.Errorf("the replaced primary's late failure counted %d against the new one", failures)
	}
}

// TestAutoSyncIdleUnreadableRecordIsReadAgain: the persisted idle spell cannot
// be read on the first idle verdict after a start, which dates the spell to the
// start for now. The next verdict reads the record again and takes the older
// spell it holds, which was never overwritten meanwhile.
func TestAutoSyncIdleUnreadableRecordIsReadAgain(t *testing.T) {
	srv, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "")
	store.CreateMember(t.Context(), "replica", "http://127.0.0.1:9", "rtoken")
	enableAutoSync(t, store, pm.ID)
	idleSince := time.Now().Add(-4 * 24 * time.Hour).UTC()
	if err := store.SetAutoSyncIdleSince(t.Context(), idleSince); err != nil {
		t.Fatalf("SetAutoSyncIdleSince: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := store.db.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	exec(`ALTER TABLE settings RENAME COLUMN auto_sync_idle_since TO idle_gone`)
	srv.autoSyncOnce(t.Context(), "")
	exec(`ALTER TABLE settings RENAME COLUMN idle_gone TO auto_sync_idle_since`)
	if got := srv.poller.autoSyncIdle(); !got.Equal(srv.startedAt) {
		t.Fatalf("with the record unreadable the spell is dated %v, want the start %v", got, srv.startedAt)
	}

	srv.autoSyncOnce(t.Context(), "")
	if got := srv.poller.autoSyncIdle(); !got.Equal(idleSince) {
		t.Errorf("once the record reads the spell is dated %v, want the persisted %v", got, idleSince)
	}
	if got, err := store.AutoSyncIdleSince(t.Context()); err != nil || !got.Equal(idleSince) {
		t.Errorf("persisted spell %v (err %v), want the untouched %v", got, err, idleSince)
	}
}

// TestAutoSyncIdleFailedWriteIsRetried: the write that persists a new idle
// spell fails once. The next verdict, which changes nothing in memory, writes
// it, so a restart finds the spell.
func TestAutoSyncIdleFailedWriteIsRetried(t *testing.T) {
	srv, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "")
	store.CreateMember(t.Context(), "replica", "http://127.0.0.1:9", "rtoken")
	enableAutoSync(t, store, pm.ID)
	exec := func(q string) {
		t.Helper()
		if _, err := store.db.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	exec(`CREATE TRIGGER no_idle_write BEFORE UPDATE OF auto_sync_idle_since ON settings BEGIN SELECT RAISE(ABORT, 'no idle write'); END`)
	srv.autoSyncOnce(t.Context(), "")
	exec(`DROP TRIGGER no_idle_write`)
	if got, err := store.AutoSyncIdleSince(t.Context()); err != nil || !got.IsZero() {
		t.Fatalf("the refused write persisted %v (err %v)", got, err)
	}

	srv.autoSyncOnce(t.Context(), "")
	if got, err := store.AutoSyncIdleSince(t.Context()); err != nil || !got.Equal(srv.startedAt.UTC()) {
		t.Errorf("after a failed write the next verdict persisted %v (err %v), want %v", got, err, srv.startedAt)
	}
}
