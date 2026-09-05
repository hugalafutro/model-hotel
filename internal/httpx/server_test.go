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
	// Literals first: the table below is written in terms of these constants,
	// so a constant moved by mistake (a zero base puts every POST's deadline
	// in the past and fails its first read) would otherwise agree with every
	// row that used it.
	if bodyReadBase != 30*time.Second || bodyReadFloor != 128<<10 || bodyReadCap != 15*time.Minute {
		t.Fatalf("budget constants moved: base %v floor %d cap %v, want 30s, 128 KiB, 15m", bodyReadBase, bodyReadFloor, bodyReadCap)
	}
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

// A listener's budget clamps a declared length to what it accepts and gives
// an undeclared (chunked) body the same time as that largest body.
func TestBodyBudgetForListener(t *testing.T) {
	t.Parallel()
	const maxBody = 50 << 20
	budget := bodyBudgetFor(maxBody)
	cases := []struct {
		name string
		cl   int64
		want time.Duration
	}{
		{"chunked earns the largest accepted body", -1, BodyReadBudget(maxBody)},
		{"empty", 0, bodyReadBase},
		{"within the ceiling is itself", 1 << 20, BodyReadBudget(1 << 20)},
		{"the ceiling exactly", maxBody, BodyReadBudget(maxBody)},
		{"past the ceiling earns nothing more", 10 << 30, BodyReadBudget(maxBody)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := budget(tc.cl); got != tc.want {
				t.Fatalf("budget(%d) = %v, want %v", tc.cl, got, tc.want)
			}
		})
	}
	// The clamp is what keeps a hostile length below the cap for the
	// gateway's largest configurable ceiling.
	if got := bodyBudgetFor(100 << 20)(1 << 40); got >= bodyReadCap {
		t.Fatalf("a hostile length on a 100 MiB listener budgets %v, want below the cap", got)
	}
	// A listener built with no ceiling still budgets an undeclared body as a
	// JSON body, never as the bare base.
	if got, want := bodyBudgetFor(0)(-1), BodyReadBudget(MaxJSONBody); got != want {
		t.Fatalf("chunked budget with no ceiling = %v, want the JSON ceiling's %v", got, want)
	}
}

// teapot is a comparable handler so the posture test can prove NewServer
// wrapped this exact handler rather than something that merely answers.
type teapot struct{}

func (teapot) ServeHTTP(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }

func TestNewServerPosture(t *testing.T) {
	t.Parallel()
	srv := NewServer(":0", teapot{}, 50<<20)
	if srv.Addr != ":0" {
		t.Fatalf("Addr = %q", srv.Addr)
	}
	// Literals, not the constants that set them: a constant zeroed by mistake
	// would otherwise agree with the field it zeroed.
	if srv.ReadHeaderTimeout != 10*time.Second || srv.IdleTimeout != 180*time.Second {
		t.Fatalf("timeouts = header %v idle %v, want 10s and 180s", srv.ReadHeaderTimeout, srv.IdleTimeout)
	}
	// Streams run for hours: neither whole-request timeout may ever be set.
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("ReadTimeout %v / WriteTimeout %v must stay zero", srv.ReadTimeout, srv.WriteTimeout)
	}
	// The body deadline is installed directly on the server, wrapping the
	// given handler, with the budget sized to this listener's ceiling. The
	// listener tests below exercise that same type on a real connection, so
	// together they leave no green path that drops the wrap.
	bd, ok := srv.Handler.(*bodyDeadline)
	if !ok {
		t.Fatalf("Handler is %T, want the body deadline", srv.Handler)
	}
	if bd.next != (teapot{}) {
		t.Fatalf("wrapped handler is %T, want the one given", bd.next)
	}
	if got, want := bd.budget(-1), BodyReadBudget(50<<20); got != want {
		t.Fatalf("chunked budget = %v, want the 50 MiB ceiling's %v", got, want)
	}
	// And a declared length inside the ceiling earns its own time, not the
	// ceiling's: the clamp must be a clamp, not a constant.
	if got, want := bd.budget(1<<20), BodyReadBudget(1<<20); got != want {
		t.Fatalf("1 MiB budget = %v, want %v", got, want)
	}
	// And a length past the ceiling earns the ceiling's time, not its own:
	// the upper clamp is what keeps a hostile length off the cap.
	if got, want := bd.budget(1<<40), BodyReadBudget(50<<20); got != want {
		t.Fatalf("1 TiB budget = %v, want the 50 MiB ceiling's %v", got, want)
	}
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

// startListener serves h under the body deadline with slowBudget on a real
// TCP listener; the deadline goes through the connection, so a recorder
// cannot exercise it. The length ServeHTTP hands the budget is recorded in
// seen, so a test can check the declared Content-Length is what earns time.
func startListener(t *testing.T, h http.Handler, seen *atomic.Int64) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(&bodyDeadline{next: h, budget: func(cl int64) time.Duration {
		if seen != nil {
			seen.Store(cl)
		}
		return slowBudget
	}})
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
	var seen atomic.Int64
	ts := startListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr.Store(&err)
		if err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}), &seen)

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
	// The declared length, not a constant, is what the budget was asked for.
	if got := seen.Load(); got != 100 {
		t.Fatalf("budget asked for length %d, want the declared 100", got)
	}
	// Anything under the 5s socket cap proves the release; the lower bound
	// proves it was the budget and not an immediate rejection.
	if waited := time.Since(start); waited < slowBudget/2 || waited > 4*time.Second {
		t.Fatalf("handler released after %v, want about the %v budget", waited, slowBudget)
	}
	var ne net.Error
	if p := readErr.Load(); p == nil || *p == nil {
		t.Fatal("handler's ReadAll must fail, not hang")
	} else if !errors.As(*p, &ne) || !ne.Timeout() {
		t.Fatalf("read error = %v (%T), want a timeout", *p, *p)
	}
	// The connection is not reusable after a body left unread: a second
	// request on it is not answered, and the connection is closed rather than
	// merely silent (a timeout here would mean it is still being held).
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: t\r\n\r\n"); err == nil {
		_, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err == nil {
			t.Fatal("a second request on the cut connection was answered")
		}
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("the cut connection is still open 2s later")
		}
	}
}

// bodyless requests must not get a deadline: net/http already runs its
// disconnect-detecting background read for them, and a deadline on the
// connection would surface there as a cancelled context.
func TestBodyReadDeadlineSparesABodilessRequest(t *testing.T) {
	t.Parallel()
	ts := startListener(t, http.HandlerFunc(outliveBudget), nil)
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
	}), nil)
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
	}), nil)
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
	if waited := time.Since(start); waited > 4*time.Second {
		t.Fatalf("connection held %v after the 401, want about the %v budget", waited, slowBudget)
	}
}

// outliveBudget streams for three budgets, recording whether the request
// context stayed alive, then answers 200 with "alive" or "cancelled".
func outliveBudget(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	http.NewResponseController(w).Flush()
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
