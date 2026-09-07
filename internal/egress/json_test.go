package egress

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAsJSONString(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantOK  bool
		comment string
	}{
		{name: "absent field", raw: "", want: "", wantOK: false},
		{name: "string literal", raw: `"hello"`, want: "hello", wantOK: true},
		{name: "empty string literal", raw: `""`, want: "", wantOK: true},
		{name: "escapes are decoded", raw: `"a\nb"`, want: "a\nb", wantOK: true},
		{name: "null decodes to the empty string", raw: `null`, want: "", wantOK: true},
		{name: "array is not a string", raw: `[{"type":"text"}]`, want: "", wantOK: false},
		{name: "object is not a string", raw: `{"type":"text"}`, want: "", wantOK: false},
		{name: "number is not a string", raw: `42`, want: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AsJSONString(json.RawMessage(tc.raw))
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("AsJSONString(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDecodeStop(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "absent field", raw: "", want: nil},
		{name: "single string", raw: `"STOP"`, want: []string{"STOP"}},
		{name: "empty string is not a stop sequence", raw: `""`, want: nil},
		{name: "array", raw: `["a","b"]`, want: []string{"a", "b"}},
		{name: "empty array", raw: `[]`, want: []string{}},
		{name: "wrong type", raw: `{"a":1}`, want: nil},
		{name: "malformed", raw: `[not json`, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecodeStop(json.RawMessage(tc.raw))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DecodeStop(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestFlattenText(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{name: "absent field", raw: "", want: "", wantOK: false},
		{name: "plain string", raw: `"hi"`, want: "hi", wantOK: true},
		{name: "null is a string field", raw: `null`, want: "", wantOK: true},
		{name: "text parts join", raw: `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, want: "ab", wantOK: true},
		{name: "untyped part counts as text", raw: `[{"text":"a"}]`, want: "a", wantOK: true},
		{name: "non-text parts are dropped", raw: `[{"type":"image_url","image_url":{"url":"u"}},{"type":"text","text":"a"}]`, want: "a", wantOK: true},
		{name: "empty array", raw: `[]`, want: "", wantOK: true},
		{name: "object is neither shape", raw: `{"type":"text"}`, want: "", wantOK: false},
		{name: "malformed", raw: `[not json`, want: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FlattenText(json.RawMessage(tc.raw))
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("FlattenText(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDecodeToolChoice(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantMode string
		wantName string
		wantOK   bool
	}{
		{name: "absent field", raw: "", wantOK: false},
		{name: "auto", raw: `"auto"`, wantMode: "auto", wantOK: true},
		{name: "none", raw: `"none"`, wantMode: "none", wantOK: true},
		{name: "required", raw: `"required"`, wantMode: "required", wantOK: true},
		{name: "unknown string mode", raw: `"whatever"`, wantOK: false},
		{name: "named function", raw: `{"type":"function","function":{"name":"lookup"}}`, wantMode: "function", wantName: "lookup", wantOK: true},
		{name: "named function without a type", raw: `{"function":{"name":"lookup"}}`, wantMode: "function", wantName: "lookup", wantOK: true},
		{name: "object without a name", raw: `{"type":"function","function":{}}`, wantOK: false},
		{name: "malformed", raw: `{not json`, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, name, ok := DecodeToolChoice(json.RawMessage(tc.raw))
			if mode != tc.wantMode || name != tc.wantName || ok != tc.wantOK {
				t.Errorf("DecodeToolChoice(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.raw, mode, name, ok, tc.wantMode, tc.wantName, tc.wantOK)
			}
		})
	}
}

func TestNewChatCompletionID(t *testing.T) {
	id := NewChatCompletionID()
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("id = %q, want a chatcmpl- prefix", id)
	}
	if rest := strings.TrimPrefix(id, "chatcmpl-"); len(rest) != 32 || strings.Contains(rest, "-") {
		t.Errorf("id body = %q, want 32 hex characters with no dashes", rest)
	}
	if second := NewChatCompletionID(); second == id {
		t.Error("two ids are identical")
	}
}
