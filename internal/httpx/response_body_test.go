package httpx

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestReadCappedBody(t *testing.T) {
	body, err := ReadCappedBody(strings.NewReader("hello"), 16)
	if err != nil {
		t.Fatalf("under the cap: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}

	// Exactly at the cap is still a whole body.
	if body, err = ReadCappedBody(strings.NewReader("hello"), 5); err != nil || string(body) != "hello" {
		t.Errorf("at the cap: body=%q err=%v", body, err)
	}

	// One byte past it is oversized, not truncated: a caller must never
	// re-parse a mutilated payload as a whole one.
	if _, err = ReadCappedBody(strings.NewReader("hello!"), 5); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("over the cap: err = %v, want ErrBodyTooLarge", err)
	}

	if _, err = ReadCappedBody(errReader{}, 16); err == nil || errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("read failure should surface as itself, got %v", err)
	}
}

func TestDecodeCappedJSON(t *testing.T) {
	var out struct {
		A int `json:"a"`
	}
	if err := DecodeCappedJSON(strings.NewReader(`{"a":7}`), 64, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.A != 7 {
		t.Errorf("a = %d, want 7", out.A)
	}

	if err := DecodeCappedJSON(strings.NewReader(`{"a":7}`), 3, &out); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("oversized decode: err = %v, want ErrBodyTooLarge", err)
	}

	if err := DecodeCappedJSON(strings.NewReader(`not json`), 64, &out); err == nil {
		t.Error("malformed JSON should be an error")
	}

	// An empty body reads clean and fails at unmarshal, not at the cap.
	if err := DecodeCappedJSON(strings.NewReader(""), 64, &out); err == nil || errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("empty body: err = %v", err)
	}

	var discard any
	if err := DecodeCappedJSON(io.LimitReader(strings.NewReader(`{}`), 2), 8, &discard); err != nil {
		t.Errorf("limited reader: %v", err)
	}
}

// TestDecodeCappedJSON_TrailingContent pins the upstream-response leniency this
// helper inherits from json.NewDecoder: a provider that appends a newline or a
// second document after the payload still decodes, so migrating a fetcher onto
// the capped read cannot start rejecting an upstream that always worked.
func TestDecodeCappedJSON_TrailingContent(t *testing.T) {
	for _, body := range []string{
		`{"a":1}` + "\n",
		`{"a":1}{"a":2}`,
		`{"a":1} trailing junk`,
	} {
		var out struct {
			A int `json:"a"`
		}
		if err := DecodeCappedJSON(strings.NewReader(body), 1024, &out); err != nil {
			t.Fatalf("DecodeCappedJSON(%q) = %v, want the first value decoded", body, err)
		}
		if out.A != 1 {
			t.Errorf("DecodeCappedJSON(%q) decoded a = %d, want 1", body, out.A)
		}
	}
}

func TestDecodeCappedJSON_OverLimit(t *testing.T) {
	var out map[string]any
	if err := DecodeCappedJSON(strings.NewReader(`{"a":"aaaaaaaaaa"}`), 4, &out); !errors.Is(err, ErrBodyTooLarge) {
		t.Errorf("err = %v, want ErrBodyTooLarge", err)
	}
}
