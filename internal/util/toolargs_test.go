package util_test

import (
	"encoding/json"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// ToolArguments absorbs a provider's spelling of tool-call arguments and always
// hands the caller the spec form. A plain string field could not decode the
// object spelling at all, and because these values sit inside a struct decoded
// as a whole, that failure discarded the entire frame or response with them.
func TestToolArguments(t *testing.T) {
	for name, tc := range map[string]struct {
		raw       string
		want      string
		reEncoded string
	}{
		"spec form, a JSON string": {`"{\"city\":\"Prague\"}"`, `{"city":"Prague"}`, `"{\"city\":\"Prague\"}"`},
		"object form":              {`{"city":"Prague"}`, `{"city":"Prague"}`, `"{\"city\":\"Prague\"}"`},
		"nested object":            {`{"a":{"b":[1,2]}}`, `{"a":{"b":[1,2]}}`, `"{\"a\":{\"b\":[1,2]}}"`},
		"array form":               {`[1,2]`, `[1,2]`, `"[1,2]"`},
		"empty string":             {`""`, "", `""`},
		"empty object":             {`{}`, "{}", `"{}"`},
		// A null arguments member carries none. json.Unmarshal of null into a
		// string succeeds and leaves it empty, which is the reading we want.
		"null": {`null`, "", `""`},
		// A string with escapes must survive the round trip unchanged.
		"escaped quotes": {`"{\"q\":\"a\\\"b\"}"`, `{"q":"a\"b"}`, `"{\"q\":\"a\\\"b\"}"`},
	} {
		t.Run(name, func(t *testing.T) {
			var got util.ToolArguments
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.raw, err)
			}
			if string(got) != tc.want {
				t.Errorf("decoded %s = %q, want %q", tc.raw, string(got), tc.want)
			}
			out, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(out) != tc.reEncoded {
				t.Errorf("re-encoded %s = %s, want %s", tc.raw, out, tc.reEncoded)
			}
		})
	}
}

// Whatever the provider sent, a second decode of what we emit yields the same
// argument text: the normalisation is stable, not lossy.
func TestToolArguments_ReEncodeIsStable(t *testing.T) {
	for _, raw := range []string{
		`{"city":"Prague"}`,
		`"{\"city\":\"Prague\"}"`,
		`{"a":{"b":[1,2]}}`,
		`""`,
		`null`,
	} {
		var first util.ToolArguments
		if err := json.Unmarshal([]byte(raw), &first); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		encoded, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal %s: %v", raw, err)
		}
		var second util.ToolArguments
		if err := json.Unmarshal(encoded, &second); err != nil {
			t.Fatalf("re-unmarshal %s: %v", encoded, err)
		}
		if second != first {
			t.Errorf("%s round-tripped to %q, want %q", raw, string(second), string(first))
		}
	}
}

// The one decision the three egress translators used to make three different
// ways. It is tested here as well as through each of them, because it is the
// place they now agree — and agreement is the property, not any one output.
func TestToolArgumentsObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{"an object", `{"city":"Oslo"}`, `{"city":"Oslo"}`},
		{"an object with whitespace round it", "  {\"city\":\"Oslo\"}\n", `{"city":"Oslo"}`},
		// A call with no arguments is still a call.
		{"absent", ``, `{}`},
		// Not a tool input. This repo's existing decision is to substitute the
		// empty object rather than kill the request; what was wrong was that
		// each translator substituted something different.
		{"an array", `["Oslo"]`, `{}`},
		{"a number", `42`, `{}`},
		{"a bare word", `not an object`, `{}`},
		{"an object that does not parse", `{not json`, `{}`},
		{"a JSON null", `null`, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := string(util.ToolArgumentsObject(util.ToolArguments(tc.args))); got != tc.want {
				t.Errorf("ToolArgumentsObject(%q) = %s, want %s", tc.args, got, tc.want)
			}
		})
	}
}
