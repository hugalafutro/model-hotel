package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeTarget struct {
	Name string `json:"name"`
}

func decodeRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/thing", strings.NewReader(body))
}

func TestDecodeJSON_ValidBody(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	if !DecodeJSON(rec, decodeRequest(`{"name":"ok"}`), "test", MaxJSONBody, &got) {
		t.Fatalf("DecodeJSON = false, body %q", rec.Body.String())
	}
	if got.Name != "ok" {
		t.Errorf("name = %q, want ok", got.Name)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want untouched 200", rec.Code)
	}
}

func TestDecodeJSON_MalformedBody_400(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	if DecodeJSON(rec, decodeRequest(`{"name":`), "test", MaxJSONBody, &got) {
		t.Fatal("DecodeJSON = true for malformed body")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid request body") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// TestDecodeJSON_OversizedBody_413 pins the whole point of the helper: a body
// past the limit is refused with 413 rather than read, and the distinct status
// is what lets an operator tell "the client sent junk" from "the client sent
// too much".
func TestDecodeJSON_OversizedBody_413(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	huge := `{"name":"` + strings.Repeat("a", 4096) + `"}`
	if DecodeJSON(rec, decodeRequest(huge), "test", 64, &got) {
		t.Fatal("DecodeJSON = true for oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// TestDecodeJSON_ExactlyAtLimit_Accepted pins the boundary as inclusive: a body
// of exactly the limit is legal, so a limit chosen to fit a known payload does
// not reject that payload by one byte.
func TestDecodeJSON_ExactlyAtLimit_Accepted(t *testing.T) {
	body := `{"name":"abc"}`
	rec := httptest.NewRecorder()
	var got decodeTarget
	if !DecodeJSON(rec, decodeRequest(body), "test", int64(len(body)), &got) {
		t.Fatalf("DecodeJSON = false at exactly the limit, body %q", rec.Body.String())
	}
	if got.Name != "abc" {
		t.Errorf("name = %q, want abc", got.Name)
	}
}

func TestDecodeJSONOptional_EmptyBodyContinues(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	if !DecodeJSONOptional(rec, decodeRequest(""), "test", MaxJSONBody, &got) {
		t.Fatalf("DecodeJSONOptional = false, body %q", rec.Body.String())
	}
	if got.Name != "" {
		t.Errorf("name = %q, want zero value", got.Name)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want untouched 200", rec.Code)
	}
}

// TestDecodeJSONOptional_PresentButBrokenBody_400 is the line between the two
// situations the tolerance must not blur. A body that never arrived means "use
// the default"; a body that arrived and could not be read means the caller
// asked for something specific, and answering it with the default silently does
// something other than what was asked. On the purge endpoint that is the
// difference between clearing an hour of logs and clearing all of them.
func TestDecodeJSONOptional_PresentButBrokenBody_400(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"truncated", `{"name":"a"`},
		{"wrong type", `{"name":5}`},
		{"not json", `nonsense`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var got decodeTarget
			if DecodeJSONOptional(rec, decodeRequest(tc.body), "test", MaxJSONBody, &got) {
				t.Fatal("DecodeJSONOptional = true for a body that was sent and could not be read")
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestDecodeJSONOptional_OversizedBody_413 is the half of the tolerance that is
// not tolerant: ignoring a malformed body must not turn into ignoring an
// unbounded read.
func TestDecodeJSONOptional_OversizedBody_413(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	huge := `{"name":"` + strings.Repeat("a", 4096) + `"}`
	if DecodeJSONOptional(rec, decodeRequest(huge), "test", 64, &got) {
		t.Fatal("DecodeJSONOptional = true for oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// TestDecodeJSON_ValidBodyUnderLimitAfterHugeField guards the read itself, not
// just the outcome: MaxBytesReader must cut the stream off, so a body whose
// declared JSON is fine but whose length is not never reaches the decoder.
func TestDecodeJSON_LimitAppliesToStreamNotContentLength(t *testing.T) {
	rec := httptest.NewRecorder()
	req := decodeRequest(`{"name":"` + strings.Repeat("a", 4096) + `"}`)
	// A client that under-declares its length gets no extra budget: the reader
	// counts bytes, it does not trust the header.
	req.ContentLength = 8
	var got decodeTarget
	if DecodeJSON(rec, req, "test", 64, &got) {
		t.Fatal("DecodeJSON = true for oversized body with an understated Content-Length")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// TestDecodeJSON_TrailingDataAfterValue_Rejected covers what makes the ceiling
// real rather than nominal. json.Decoder stops the moment it has one complete
// value, so without draining what follows, a tiny object with a huge tail would
// decode fine and never trip the limit: the endpoint would answer 200 to a body
// of any size, and 413 would stop meaning "oversized". A tail under the limit is
// a smuggled second payload and earns a 400; a tail that runs past the limit is
// the 413 the naive version never produced.
func TestDecodeJSON_TrailingDataAfterValue_Rejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"second value", `{"name":"a"}{"name":"b"}`, http.StatusBadRequest},
		{"junk", `{"name":"a"}garbage`, http.StatusBadRequest},
		{"tail past the limit", `{"name":"a"}` + strings.Repeat(" ", 4096), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var got decodeTarget
			if DecodeJSON(rec, decodeRequest(tc.body), "test", 64, &got) {
				t.Fatal("DecodeJSON = true for a body with trailing data")
			}
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body=%q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestDecodeJSON_TrailingNewlineAccepted keeps the drain from breaking clients
// that end a request body with a newline, which is ordinary and not a tail.
func TestDecodeJSON_TrailingNewlineAccepted(t *testing.T) {
	rec := httptest.NewRecorder()
	var got decodeTarget
	if !DecodeJSON(rec, decodeRequest("{\"name\":\"ok\"}\n"), "test", MaxJSONBody, &got) {
		t.Fatalf("DecodeJSON = false for a trailing newline, body %q", rec.Body.String())
	}
	if got.Name != "ok" {
		t.Errorf("name = %q, want ok", got.Name)
	}
}

// TestDecodeFailure_NamesTheFailureWithoutQuotingTheBody pins the logging
// invariant: this gateway never logs request content, and the
// pre-authentication routes decode through here, so the byte a json.SyntaxError
// quotes must not reach the log. Every other failure is normalised to the same
// fixed vocabulary so no decoder message reaches a log line by accident. The
// errors here are the real ones the decoder produces, not hand-built ones, so
// the test fails if a future Go release changes what they carry.
func TestDecodeFailure_NamesTheFailureWithoutQuotingTheBody(t *testing.T) {
	var target decodeTarget
	syntaxErr := json.Unmarshal([]byte(`{qqqq}`), &target)
	typeErr := json.Unmarshal([]byte(`{"name":31337}`), &target)
	if syntaxErr == nil || typeErr == nil {
		t.Fatal("expected the decoder to reject both probe bodies")
	}

	for _, tc := range []struct {
		name   string
		err    error
		want   string
		secret string // the caller-controlled fragment the raw error carries
	}{
		{"syntax", syntaxErr, "malformed JSON", "q"},
		{"wrong type", typeErr, "wrong JSON type for field", ""},
		{"truncated", io.ErrUnexpectedEOF, "truncated JSON value", ""},
		{"empty", io.EOF, "empty body", ""},
		{"trailing", nil, "trailing data after JSON value", ""},
		{"other", errors.New("boom"), "unreadable body", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, _ := decodeFailure(tc.err)
			if kind != tc.want {
				t.Errorf("kind = %q, want %q", kind, tc.want)
			}
			if tc.secret != "" {
				if !strings.Contains(tc.err.Error(), tc.secret) {
					t.Fatalf("probe is stale: %q no longer carries %q", tc.err, tc.secret)
				}
				if strings.Contains(kind, tc.secret) {
					t.Errorf("kind %q leaks the body fragment %q", kind, tc.secret)
				}
			}
		})
	}
}

// TestDecodeFailure_OffsetIsAPositionNotAValue keeps the one number the log does
// carry honest: it locates the failure, it is not read out of the body.
func TestDecodeFailure_OffsetIsAPositionNotAValue(t *testing.T) {
	var target decodeTarget
	err := json.Unmarshal([]byte(`{"name":"ok","x":}`), &target)
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	kind, offset := decodeFailure(err)
	if kind != "malformed JSON" {
		t.Fatalf("kind = %q", kind)
	}
	if offset <= 0 {
		t.Errorf("offset = %d, want the position of the bad byte", offset)
	}
}
