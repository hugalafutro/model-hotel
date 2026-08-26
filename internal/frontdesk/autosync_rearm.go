package frontdesk

import "context"

// startRearmWatch spawns a pass's rearm watcher and returns the stop func that
// cancels it and blocks until it has returned.
//
// The pass joins the watcher rather than merely cancelling it because the watcher
// reads the store: a pass that returned while its watcher was still in flight
// would leave that read owned by nobody. Cancelling is not enough on its own,
// since a goroutine that has already entered a query does not observe the cancel
// until it comes back out. Everything that waits for a pass therefore waits for
// the watcher too: the tick loop in RunAutoSync, the enable-time kick's drain via
// Server.Wait, and a test tearing down the store its temp dir holds. Without the
// join, that store read outlives the store and races its Close and the removal of
// its directory.
//
// cancel is the pass's own context cancel and is idempotent, so the ordinary case
// where the watcher cancels on a rearm and stop then cancels again is harmless.
func (s *Server) startRearmWatch(ctx context.Context, rearmCh <-chan struct{}, gen int64, cancel context.CancelFunc) (stop func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchRearm(ctx, rearmCh, gen, cancel)
	}()
	return func() {
		cancel()
		<-done
	}
}

// watchRearm cancels a convergence pass the moment its rearm generation goes
// stale, so a rearm landing while applyMemberConfig is mid-flight aborts the HTTP
// request instead of finishing a now-stale write.
//
// rearmCh is the in-process broadcast closed by signalRearm, so the wake is
// synchronous with the generation bump rather than gated on a poll. The generation
// is re-read first to close the gap between the caller capturing gen and this
// watcher starting, where the channel close may predate the capture. The watcher
// exits as soon as ctx is done, and startRearmWatch's stop waits for that exit, so
// it never outlives the pass. A transient read error is ignored; the pass's own
// stale() gates remain.
func (s *Server) watchRearm(ctx context.Context, rearmCh <-chan struct{}, gen int64, cancel context.CancelFunc) {
	if cur, err := s.store.AutoSyncGen(ctx); err == nil && cur != gen {
		cancel()
		return
	}
	select {
	case <-ctx.Done():
	case <-rearmCh:
		// A rearm broadcast woke us: the fleet's sync inputs changed (a gen-bumping
		// rearm/repoint, or a disband that cleared the designation without touching
		// the gen), so this pass is stale either way: cancel it.
		cancel()
	}
}
