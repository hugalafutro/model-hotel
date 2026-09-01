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
// forever. The server side of an idle race should be the longer one, or a
// client that reuses a pooled connection at the last moment writes a request
// into a connection the gateway has just closed. 180s sits above the pools
// under this project's control: Traefik's default 90s upstream idle, Go's
// default 90s transport (Front Desk's member clients pool at that value), and
// the gateway's own 120s outbound pool (one Model Hotel is a valid provider
// for another). Bellhop's OkHttp pool keeps a connection for five minutes and
// is left above it on purpose: OkHttp checks a pooled socket's health before
// reuse and retries a connection failure, and a listener idle timeout has to
// stay bounded rather than chase every client's pool.
//
// There is deliberately no ReadTimeout and no WriteTimeout. A streaming chat
// completion or an /api/events stream runs for minutes to hours, and
// WriteTimeout would cut it off; the request body is bounded per request by
// BodyReadDeadline instead, which lets a large upload earn more time than a
// small JSON body without extending every request to the upload's budget.
const (
	ReadHeaderTimeout = 10 * time.Second
	IdleTimeout       = 180 * time.Second
)

// Body read budget. A request with a body must deliver all of it within
// bodyReadBase plus one second per bodyReadFloor bytes of its length, capped
// at bodyReadCap.
//
// The base covers every control-plane JSON body (1 MiB ceiling) and a plain
// chat request with room to spare. The floor is a deliberately poor uplink
// (128 KiB/s, about 1 Mbit/s): a 20 MiB vision request earns 160s on top of
// the base, and the largest legitimate body, the 100 MiB backup restore, earns
// 800s. The cap is the last line against a hostile Content-Length: whatever
// is declared, the connection is released after fifteen minutes.
//
// The length that earns time is the declared Content-Length clamped to the
// largest body the listener accepts (NewServer's maxBody), and a body that
// declares no length at all (Transfer-Encoding: chunked, which any Go client
// streaming a file or pipe, a browser fetch with a stream body, and curl
// uploading from a pipe with -T - all send) is budgeted as that largest body.
// A chunked 25 MiB transcription upload therefore gets the same time as one
// that declared its size, while a declared or undeclared length past what the
// listener would accept anyway earns nothing beyond it.
const (
	bodyReadBase  = 30 * time.Second
	bodyReadFloor = int64(128 << 10)
	bodyReadCap   = 15 * time.Minute
)

// BodyReadBudget is the time a body of length bytes gets to arrive. An
// empty length gets the base; the clamping of a declared length and the
// treatment of an undeclared one happen in bodyBudgetFor.
func BodyReadBudget(length int64) time.Duration {
	if length <= 0 {
		return bodyReadBase
	}
	// Whole seconds via integer division, so a length near MaxInt64 cannot
	// overflow a Duration multiplication before the cap applies.
	secs := length / bodyReadFloor
	if secs >= int64((bodyReadCap-bodyReadBase)/time.Second) {
		return bodyReadCap
	}
	return bodyReadBase + time.Duration(secs)*time.Second
}

// bodyBudgetFor returns the budget function for a listener that accepts at
// most maxBody bytes of body: a declared length is clamped to maxBody and an
// undeclared one (ContentLength -1) is taken as maxBody. A maxBody of zero or
// less would hand every undeclared body the bare base, so it is floored at the
// control-plane JSON ceiling, the lower of the two listeners' ceilings.
func bodyBudgetFor(maxBody int64) func(contentLength int64) time.Duration {
	if maxBody <= 0 {
		maxBody = MaxJSONBody
	}
	return func(contentLength int64) time.Duration {
		if contentLength < 0 || contentLength > maxBody {
			contentLength = maxBody
		}
		return BodyReadBudget(contentLength)
	}
}

// bodyDeadline bounds the time a request gets to deliver its body. It sits
// directly under the http.Server, outside every other wrapper: the deadline
// goes through http.ResponseController, which needs a path down to net/http's
// own ResponseWriter.
//
// Only a request that carries a body gets a deadline. For a bodiless request
// net/http has already started its background read (the one that turns a
// client disconnect into a cancelled context) with no deadline, and putting
// one on the connection now would cancel every /api/events stream and every
// long GET at the deadline instead. For a request with a body that read is
// deferred until the body reaches EOF, and net/http clears the connection's
// read deadline when it starts it (connReader.startBackgroundRead), so a
// stream that begins after its body was read runs unbounded. The one path
// that skips the clear is a pipelined connection whose next request's first
// byte already arrived (hasByte), and there nothing reads the connection
// during the stream, so the stream is untouched and only keep-alive reuse is
// lost. The deadline therefore covers the body and nothing else.
//
// The budget starts when the handler chain is entered, not at the first body
// byte, so the time routing and auth take before the handler reads counts
// against it; today that is milliseconds. The same holds for a client that
// sends Expect: 100-continue and waits for the handler's first read.
//
// A body the handler stops reading before EOF keeps the deadline, which then
// bounds net/http's post-handler drain of the remainder as well: a client that
// declares a body, gets rejected before it is read (a 401 or a 404) and then
// trickles the rest no longer holds the connection open through that drain.
type bodyDeadline struct {
	next   http.Handler
	budget func(contentLength int64) time.Duration
}

func (h *bodyDeadline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil && r.Body != http.NoBody {
		// The error is dropped on purpose: a writer that cannot take a
		// deadline (a recorder in a test, a wrapper without Unwrap) keeps the
		// unbounded read it had, and there is nothing to fail the request
		// over.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(h.budget(r.ContentLength)))
	}
	h.next.ServeHTTP(w, r)
}

// NewServer builds the http.Server both binaries listen on, with the posture
// described above. maxBody is the largest request body the listener's routes
// accept (the gateway's MAX_REQUEST_SIZE, Front Desk's JSON ceiling); it sizes
// the body budget for a request that declares no length or more than that,
// which also means raising the gateway's ceiling raises the longest hold a
// hostile connection can buy (430s at the 50 MiB default, 830s at 100 MiB).
func NewServer(addr string, handler http.Handler, maxBody int64) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           &bodyDeadline{next: handler, budget: bodyBudgetFor(maxBody)},
		ReadHeaderTimeout: ReadHeaderTimeout,
		IdleTimeout:       IdleTimeout,
	}
}
