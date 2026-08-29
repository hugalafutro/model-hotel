package util_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// The distinction a *T field made for free: nil meant absent OR null. A
// json.RawMessage for null is four non-empty bytes, so a member held raw to be
// decoded on its own loses it — and reading only the length let an explicit null
// overwrite counts an earlier chunk had reported.
func TestJSONMemberSet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"input_tokens":1}`, true},
		{`{}`, true},
		{`[]`, true},
		{`0`, true},
		{`""`, true},
		{`false`, true},
		// Absent, and the two spellings of "explicitly nothing".
		{``, false},
		{`null`, false},
		{`  null  `, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			if got := util.JSONMemberSet(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("JSONMemberSet(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// A member the caller has no struct for is not broken bytes. The check is on the
// document being a well-formed JSON OBJECT rather than on the error naming a
// member, because an error returned by a nested custom UnmarshalJSON arrives
// with an empty Field too.
func TestShapeError(t *testing.T) {
	t.Parallel()
	named := &json.UnmarshalTypeError{Value: "string", Field: "input_tokens"}
	fieldless := &json.UnmarshalTypeError{Value: "array"}
	for _, tc := range []struct {
		name string
		data string
		err  error
		want bool
	}{
		{"a member this struct cannot type", `{"input_tokens":"12"}`, named, true},
		{"a nested decoder's error, which names nothing", `{"usage":[]}`, fieldless, true},
		// Not the document at all.
		{"a bare list", `[1,2,3]`, fieldless, false},
		{"a bare string", `"nope"`, fieldless, false},
		{"a bare number", `42`, fieldless, false},
		{"null", `null`, fieldless, false},
		// An object, but not sound bytes.
		{"truncated", `{"input_tokens":`, named, false},
		// Not a type error at all.
		{"a syntax error", `{"a":}`, &json.SyntaxError{}, false},
		{"no error", `{"a":1}`, nil, false},
		{"some other error", `{"a":1}`, errors.New("boom"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := util.ShapeError([]byte(tc.data), tc.err) != nil; got != tc.want {
				t.Errorf("ShapeError(%s) non-nil = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}
