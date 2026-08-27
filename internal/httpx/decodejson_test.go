package httpx

import (
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

func TestDecodeJSONOptional_EmptyAndMalformedBodiesContinue(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"malformed", `{"name":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var got decodeTarget
			if !DecodeJSONOptional(rec, decodeRequest(tc.body), "test", MaxJSONBody, &got) {
				t.Fatalf("DecodeJSONOptional = false, body %q", rec.Body.String())
			}
			if got.Name != "" {
				t.Errorf("name = %q, want zero value", got.Name)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want untouched 200", rec.Code)
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
