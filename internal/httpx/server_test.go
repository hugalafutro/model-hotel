package httpx

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBodyReadBudget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cl   int64
		want time.Duration
	}{
		{"chunked, no declared length", -1, bodyReadBase},
		{"empty", 0, bodyReadBase},
		{"one byte", 1, bodyReadBase},
		{"just under one floor unit", bodyReadFloor - 1, bodyReadBase},
		{"one floor unit earns one second", bodyReadFloor, bodyReadBase + time.Second},
		{"control-plane JSON ceiling", MaxJSONBody, bodyReadBase + 8*time.Second},
		{"20 MiB vision request", 20 << 20, bodyReadBase + 160*time.Second},
		{"100 MiB backup restore", 100 << 20, bodyReadBase + 800*time.Second},
		{"1 GiB hostile length hits the cap", 1 << 30, bodyReadCap},
		{"MaxInt64 does not overflow", 1<<63 - 1, bodyReadCap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BodyReadBudget(tc.cl); got != tc.want {
				t.Fatalf("BodyReadBudget(%d) = %v, want %v", tc.cl, got, tc.want)
			}
		})
	}
	// The largest legitimate body must fit under the cap at the floor rate,
	// or the cap would reject a slow but honest restore.
	if BodyReadBudget(100<<20) >= bodyReadCap {
		t.Fatalf("a 100 MiB restore at the floor rate (%v) must sit below the cap (%v)", BodyReadBudget(100<<20), bodyReadCap)
	}
}

func TestNewServerPosture(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	srv := NewServer(":0", inner)
	if srv.Addr != ":0" {
		t.Fatalf("Addr = %q", srv.Addr)
	}
	if srv.ReadHeaderTimeout != ReadHeaderTimeout || srv.IdleTimeout != IdleTimeout {
		t.Fatalf("timeouts = header %v idle %v", srv.ReadHeaderTimeout, srv.IdleTimeout)
	}
	// Streams run for hours: neither whole-request timeout may ever be set.
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("ReadTimeout %v / WriteTimeout %v must stay zero", srv.ReadTimeout, srv.WriteTimeout)
	}
	// The handler is wrapped, not replaced: the inner handler still answers.
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("wrapped handler answered %d, want 418", rec.Code)
	}
}

// slowBudget is the per-request budget the listener tests run under: long
// enough for a body sent in one write to land, short enough that a trickle
// is cut within the test's patience.
const slowBudget = 200 * time.Millisecond

// startListener serves h under bodyReadDeadline with slowBudget on a real
// TCP listener; the deadline goes through the connection, so a recorder
// cannot exercise it.
func startListener(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(bodyReadDeadline(h, func(int64) time.Duration { return slowBudget }))
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// dial opens a raw connection to ts and writes raw; the caller decides how
// much of the body arrives and when.
func dial(t *testing.T, ts *httptest.Server, raw string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
	return conn
}

func TestBodyReadDeadlineCutsATrickledBody(t *testing.T) {
	t.Parallel()
	var readErr atomic.Pointer[error]
	ts := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr.Store(&err)
		if err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Headers complete, two of a declared hundred body bytes, then silence.
	conn := dial(t, ts, "POST / HTTP/1.1\r\nHost: t\r\nContent-Length: 100\r\n\r\nab")
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no response within 5s of a trickled body: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408 from the handler's failed read", resp.StatusCode)
	}
	if waited := time.Since(start); waited < slowBudget/2 || waited > 2*time.Second {
		t.Fatalf("handler released after %v, want about the %v budget", waited, slowBudget)
	}
	var ne net.Error
	if p := readErr.Load(); p == nil || *p == nil {
		t.Fatal("handler's ReadAll must fail, not hang")
	} else if !errors.As(*p, &ne) || !ne.Timeout() {
		t.Fatalf("read error = %v (%T), want a timeout", *p, *p)
	}
	// The connection is not reusable after a body left unread: a second
	// request on it must not be served.
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: t\r\n\r\n"); err == nil {
		if _, err := http.ReadResponse(bufio.NewReader(conn), nil); err == nil {
			t.Fatal("a second request on the cut connection was answered")
		}
	}
}

// bodyless requests must not get a deadline: net/http already runs its
// disconnect-detecting background read for them, and a deadline on the
// connection would surface there as a cancelled context.
func TestBodyReadDeadlineSparesABodilessRequest(t *testing.T) {
	t.Parallel()
	ts := startListener(t, http.HandlerFunc(outliveBudget))
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	assertOutlived(t, resp)
}

// A stream that starts after its body was read runs unbounded: net/http
// clears the deadline the moment the body reaches EOF.
func TestBodyReadDeadlineSparesAStreamAfterItsBody(t *testing.T) {
	t.Parallel()
	ts := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		outliveBudget(w, r)
	}))
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	assertOutlived(t, resp)
}

// A body the handler never touches keeps its deadline, so the connection is
// released once the budget passes even though nothing in the handler reads.
func TestBodyReadDeadlineBoundsAnUnreadBody(t *testing.T) {
	t.Parallel()
	ts := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Reject before reading, the shape of an auth failure on a POST.
		w.WriteHeader(http.StatusUnauthorized)
	}))
	conn := dial(t, ts, "POST / HTTP/1.1\r\nHost: t\r\nContent-Length: 100\r\n\r\nab")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	// The 401 arrives at once; the connection then has to close on its own
	// once the drain of the never-sent remainder hits the deadline. A clean
	// EOF is that close; a timeout is the connection still open.
	if _, err := io.Copy(io.Discard, conn); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("connection still open 5s after a 401 on a trickled body")
		}
		t.Fatalf("read: %v", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Fatalf("connection held %v after the 401, want about the %v budget", waited, slowBudget)
	}
}

// outliveBudget streams for three budgets, recording whether the request
// context stayed alive, then answers 200 with "alive" or "cancelled".
func outliveBudget(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	http.NewResponseController(w).Flush() //nolint:errcheck // a recorder cannot flush; irrelevant here
	deadline := time.Now().Add(3 * slowBudget)
	for time.Now().Before(deadline) {
		if r.Context().Err() != nil {
			_, _ = io.WriteString(w, "cancelled")
			return
		}
		time.Sleep(slowBudget / 4)
	}
	_, _ = io.WriteString(w, "alive")
}

func assertOutlived(t *testing.T, resp *http.Response) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "alive" {
		t.Fatalf("stream past the budget: status %d body %q, want 200 alive", resp.StatusCode, body)
	}
}
