package egress

import (
	"encoding/json"
	"testing"
)

func TestThoughtSignatureIn(t *testing.T) {
	for raw, want := range map[string]string{
		`{"google":{"thought_signature":"sig"}}`: "sig",
		`{"google":{}}`:                          "",
		`{"other":{"thought_signature":"sig"}}`:  "",
		`null`:                                   "",
		`"junk"`:                                 "",
		`{"google":"junk"}`:                      "",
		``:                                       "",
	} {
		if got := ThoughtSignatureIn(json.RawMessage(raw)); got != want {
			t.Errorf("ThoughtSignatureIn(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestExtraContentFor(t *testing.T) {
	if ExtraContentFor("") != nil {
		t.Error("an unsigned call must carry no member")
	}
	out, _ := json.Marshal(ExtraContentFor("sig"))
	if string(out) != `{"google":{"thought_signature":"sig"}}` {
		t.Errorf("wire shape = %s", out)
	}
	if got := ThoughtSignatureIn(out); got != "sig" {
		t.Errorf("round trip = %q", got)
	}
}
