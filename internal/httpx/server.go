package httpx

import (
	"net/http"
	"time"
)

// Listener posture shared by both binaries (cmd/server, cmd/frontdesk). Each
// used to build its own http.Server with ReadHeaderTimeout alone, which left
// two ways for a client to hold a goroutine and a file descriptor for free:
// send the headers promptly and then trickle the body, or send one request and
// then leave the keep-alive connection open. NewServer closes both.
//
// ReadHeaderTimeout bounds the request line and headers. IdleTimeout bounds a
// keep-alive connection between requests; when it is unset net/http falls back
// to ReadTimeout, and with that unset too it waits for the next request
// forever. 120s sits above Traefik's default 90s upstream idle so the proxy in
// front of the gateway drops an idle connection first: the other way round
// races a request the proxy has just started writing into a connection the
// gateway has just closed.
//
// There is deliberately no ReadTimeout and no WriteTimeout. A streaming chat
// completion or an /api/events stream runs for minutes to hours, and
// WriteTimeout would cut it off; the request body is bounded per request by
// BodyReadDeadline instead, which lets a large upload earn more time than a
// small JSON body without extending every request to the upload's budget.
const (
	ReadHeaderTimeout = 10 * time.Second
	IdleTimeout       = 120 * time.Second
)

// Body read budget. A request with a body must deliver all of it within
// bodyReadBase plus one second per bodyReadFloor bytes of its declared
// Content-Length, capped at bodyReadCap.
//
// The base covers every control-plane JSON body (1 MiB ceiling) and a plain
// chat request with room to spare. The floor is a deliberately poor uplink
// (128 KiB/s, about 1 Mbit/s): a 20 MiB vision request earns 160s on top of
// the base, and the largest legitimate body, the 100 MiB backup restore, earns
// 800s. The cap holds a hostile Content-Length to a bounded cost: whatever
// length is declared, the connection is released after fifteen minutes. A
// chunked body declares no length and gets the base alone; every client this
// gateway serves (OpenAI SDKs, browsers, curl -d) sends a Content-Length.
const (
	bodyReadBase  = 30 * time.Second
	bodyReadFloor = int64(128 << 10)
	bodyReadCap   = 15 * time.Minute
)

// BodyReadBudget is the time a request declaring contentLength bytes of body
// gets to deliver all of it. A missing (-1) or empty length gets the base.
func BodyReadBudget(contentLength int64) time.Duration {
	if contentLength <= 0 {
		return bodyReadBase
	}
	// Whole seconds via integer division, so a Content-Length near MaxInt64
	// cannot overflow a Duration multiplication before the cap applies.
	secs := contentLength / bodyReadFloor
	if secs >= int64((bodyReadCap-bodyReadBase)/time.Second) {
		return bodyReadCap
	}
	return bodyReadBase + time.Duration(secs)*time.Second
}

// BodyReadDeadline bounds the time a request gets to deliver its body. It
// belongs directly under the http.Server, outside every other wrapper: the
// deadline goes through http.ResponseController, which needs a path down to
// net/http's own ResponseWriter.
//
// Only a request that carries a body gets a deadline. For a bodiless request
// net/http has already started its background read (the one that turns a
// client disconnect into a cancelled context) with no deadline, and putting
// one on the connection now would cancel every /api/events stream and every
// long GET at the deadline instead. For a request with a body that read is
// deferred until the body reaches EOF, and net/http clears the connection's
// read deadline at that moment (connReader.startBackgroundRead), so a stream
// that begins after its body was read runs unbounded exactly as before. The
// deadline therefore covers the body and nothing else.
//
// A body the handler stops reading before EOF keeps the deadline, which then
// bounds net/http's post-handler drain of the remainder as well: a client that
// declares a body, gets rejected before it is read (a 401 or a 404) and then
// trickles the rest no longer holds the connection open through that drain.
func BodyReadDeadline(next http.Handler) http.Handler {
	return bodyReadDeadline(next, BodyReadBudget)
}

func bodyReadDeadline(next http.Handler, budget func(contentLength int64) time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			// The error is dropped on purpose: a writer that cannot take a
			// deadline (a recorder in a test, a wrapper without Unwrap) keeps
			// the unbounded read it had, and there is nothing to fail the
			// request over.
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(budget(r.ContentLength)))
		}
		next.ServeHTTP(w, r)
	})
}

// NewServer builds the http.Server both binaries listen on, with the posture
// described above and handler wrapped in BodyReadDeadline.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           BodyReadDeadline(handler),
		ReadHeaderTimeout: ReadHeaderTimeout,
		IdleTimeout:       IdleTimeout,
	}
}
