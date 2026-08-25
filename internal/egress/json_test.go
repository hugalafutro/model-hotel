package egress

import (
	"encoding/json"
	"reflect"
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
