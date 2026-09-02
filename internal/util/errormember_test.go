package util_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

func TestValueCarries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		member string
		want   bool
	}{
		{`{"message":"rate limited"}`, true},
		{`"model not found"`, true},
		{`{"code":500}`, true},
		{`["bad","worse"]`, true},
		{`503`, true},
		{`true`, true},
		{`{"code":404,"message":"","type":"not_found"}`, true},
		{`[{"reason":"quota"}]`, true},

		{`null`, false},
		{`{}`, false},
		{`""`, false},
		{`"   "`, false},
		{`[]`, false},
		// The C convention for the absence of an error, at the top level and
		// one down: a relay's no-error stamp, arriving on every frame.
		{`false`, false},
		{`0`, false},
		{`{"code":0,"message":"","type":""}`, false},
		{`{"code":null,"message":null}`, false},
		{`{"details":[]}`, false},
		{`{"failed":false}`, false},
		{`[null,{}]`, false},
		// Not JSON at all. Every reader hands this function a member that came
		// out of a successful unmarshal, so this is the contract for a caller
		// with a raw member of its own: garbage carries nothing.
		{`{"message":"trunc`, false},
		{``, false},
	} {
		t.Run(tc.member, func(t *testing.T) {
			t.Parallel()
			if got := util.ValueCarries(json.RawMessage(tc.member)); got != tc.want {
				t.Errorf("ValueCarries(%s) = %v, want %v", tc.member, got, tc.want)
			}
		})
	}
}

func TestErrorMemberMessage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ member, want string }{
		{`{"message":"rate limited"}`, "rate limited"},
		{`"model not found"`, "model not found"},
		{`{"code":500}`, `{"code":500}`},
		{`["bad","worse"]`, `["bad","worse"]`},
		{`503`, "503"},
		{`true`, "true"},
	} {
		t.Run(tc.member, func(t *testing.T) {
			t.Parallel()
			if got := util.ErrorMemberMessage(json.RawMessage(tc.member)); got != tc.want {
				t.Errorf("ErrorMemberMessage(%s) = %q, want %q", tc.member, got, tc.want)
			}
		})
	}
}

// encoding/json caps nesting while decoding, so the recursion is bounded before
// it is ever entered. This is the depth that cap allows.
func TestValueCarries_DeepNesting(t *testing.T) {
	t.Parallel()
	const depth = 9000
	deep := strings.Repeat(`[`, depth) + `"boom"` + strings.Repeat(`]`, depth)
	if !util.ValueCarries(json.RawMessage(deep)) {
		t.Error("a value nested to the decoder's limit must still be read")
	}
	empty := strings.Repeat(`[`, depth) + strings.Repeat(`]`, depth)
	if util.ValueCarries(json.RawMessage(empty)) {
		t.Error("nesting alone is not something to read")
	}
}
