package jsonfault

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// decodeErr returns the error encoding/json produces for the given document,
// decoded into the given target.
func decodeErr(t *testing.T, doc string, target any) error {
	t.Helper()
	err := json.Unmarshal([]byte(doc), target)
	if err == nil {
		t.Fatalf("document %q decoded without error", doc)
	}
	return err
}

func TestDescribe_SyntaxError(t *testing.T) {
	// A *json.SyntaxError quotes the offending byte, so the raw message would
	// carry a character of the document. The description reports position only.
	var v struct {
		Text string `json:"text"`
	}
	doc := `{"text":"Kohlrabi"` // truncated
	err := decodeErr(t, doc, &v)

	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("err = %T, want *json.SyntaxError", err)
	}
	got := Describe(err, len(doc))
	want := "malformed JSON at byte 18 of 18"
	if got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
}

func TestDescribe_UnmarshalTypeError(t *testing.T) {
	// A *json.UnmarshalTypeError prints the offending literal verbatim
	// ("number 8675309.42") and names the field it landed in; neither may
	// survive into the description.
	var v struct {
		Count int `json:"Kohlrabi"`
	}
	doc := `{"Kohlrabi":8675309.42}`
	err := decodeErr(t, doc, &v)

	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("err = %T, want *json.UnmarshalTypeError", err)
	}
	if !strings.Contains(err.Error(), "8675309.42") {
		t.Fatalf("precondition failed: %q no longer carries the literal", err)
	}

	got := Describe(err, len(doc))
	if !strings.HasPrefix(got, "unexpected JSON value at byte ") {
		t.Errorf("Describe = %q, want an unexpected-value description", got)
	}
	if !strings.HasSuffix(got, " of 23") {
		t.Errorf("Describe = %q, want the document length reported", got)
	}
	if strings.Contains(got, "8675309.42") || strings.Contains(got, "Kohlrabi") {
		t.Errorf("Describe leaked the document: %q", got)
	}
}

func TestDescribe_OtherErrorKinds(t *testing.T) {
	// Anything else — a custom UnmarshalJSON's own error, an opaque sentinel —
	// falls back to the size alone, since only the caller knows whether such an
	// error is safe to print.
	cases := map[string]error{
		"custom unmarshaler error": json.Unmarshal([]byte(`{}`), &pickyTarget{}),
		"opaque error":             errors.New("Kohlrabi is not a fruit"),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("precondition failed: no error to describe")
			}
			got := Describe(err, 42)
			if got != "undecodable JSON (42 bytes)" {
				t.Errorf("Describe = %q, want the generic form", got)
			}
		})
	}
}

func TestDescribe_WrappedErrorsAreMatched(t *testing.T) {
	// Callers may hand over an error some intermediate layer already wrapped;
	// errors.As must still find the concrete kind underneath.
	var v struct {
		Text string `json:"text"`
	}
	doc := `{"text":"Kohlrabi"`
	wrapped := errFmt(decodeErr(t, doc, &v))
	if got := Describe(wrapped, len(doc)); !strings.HasPrefix(got, "malformed JSON at byte ") {
		t.Errorf("Describe = %q, want the wrapped syntax error to be recognised", got)
	}
}

// errFmt wraps err the way an intermediate layer would.
func errFmt(err error) error { return errors.Join(errors.New("upstream read"), err) }

// pickyTarget rejects every document with an error of its own making, standing
// in for a type whose UnmarshalJSON fails for reasons encoding/json never sees.
type pickyTarget struct{}

func (*pickyTarget) UnmarshalJSON([]byte) error { return errors.New("Kohlrabi is not a fruit") }
