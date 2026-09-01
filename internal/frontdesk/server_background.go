package frontdesk

import (
	"context"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The server's background-goroutine registry: every process-lifetime loop and
// every detached kick goes through here, so Wait and Shutdown can drain them and
// a handler can ask whether it is already too late to start more. Split out of
// server.go when that file hit the size ceiling.

// Wait blocks until every background goroutine the server tracks has returned:
// the detached auto-sync kick, and every process-lifetime loop started through
// StartBackground. Use it on graceful shutdown, or in tests before tearing down
// the backing store, so a still-running goroutine can't write into a store or
// temp dir that is being removed. Shutdown is the bounded version callers should
// prefer at process exit.
func (s *Server) Wait() { s.bgWG.Wait() }

// StartBackground runs fn as a tracked background goroutine and reports whether it
// started, so Wait and Shutdown cover it. The registration happens on the caller's
// goroutine, before fn is spawned, so a shutdown racing startup still waits for fn
// rather than missing a counter that had not been incremented yet.
//
// It refuses once Shutdown has begun, and that refusal is the whole point of the
// lock: an http.Server drain returns on its own deadline without stopping the
// handlers still in flight, so a handler outliving it and registering here would
// otherwise trip the WaitGroup's "Add called concurrently with Wait" panic.
// Refusing is also the right answer on its own terms, since anything started at
// that moment is cancelled milliseconds later. The caller decides what to do
// without the goroutine; false is a normal shutdown-time answer, not an error.
//
// fn owns its exit: it is expected to return when ctx is done, which is how
// Shutdown's drain ever completes.
func (s *Server) StartBackground(ctx context.Context, fn func(context.Context)) (started bool) {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bgClosing {
		debuglog.Debug("frontdesk: background work refused, server is shutting down")
		return false
	}
	s.bgWG.Go(func() { fn(ctx) })
	return true
}

// shuttingDown reports whether Shutdown has begun and background work is being
// refused. It lets a handler answer with its own actionable message before doing
// store reads that a closing store would fail anyway, turning what the operator
// sees from a generic internal error into "try again after the restart".
// StartBackground remains the authority: a caller that passes this check can
// still lose the race and must handle a false return.
func (s *Server) shuttingDown() bool {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	return s.bgClosing
}

// StartBackgroundTimeout is StartBackground for detached work that needs its own
// deadline: it derives a time-bounded context from parent, hands it to fn, and
// releases it exactly once whichever way the registration goes. fn's own run
// releases it on the way out; a refusal releases it here, so a caller that is too
// late to start work never leaks the context it prepared.
func (s *Server) StartBackgroundTimeout(parent context.Context, d time.Duration, fn func(context.Context)) (started bool) {
	ctx, cancel := context.WithTimeout(parent, d)
	if !s.StartBackground(ctx, func(c context.Context) {
		defer cancel()
		fn(c)
	}) {
		cancel()
		return false
	}
	return true
}
