package util_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

type countBlock struct {
	Prompt     int          `json:"prompt_tokens"`
	Completion int          `json:"completion_tokens"`
	Label      string       `json:"label"`
	Details    *countDetail `json:"details"`
}

type countDetail struct {
	Cached int `json:"cached_tokens"`
}

func TestDecodeCounts_Spellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"plain integer", `{"prompt_tokens":12}`, 12},
		{"quoted", `{"prompt_tokens":"12"}`, 12},
		{"quoted with space", `{"prompt_tokens":" 12 "}`, 12},
		{"fractional", `{"prompt_tokens":12.0}`, 12},
		{"quoted fractional", `{"prompt_tokens":"12.0"}`, 12},
		{"exponent", `{"prompt_tokens":1.2e1}`, 12},
		// Rounded rather than truncated: a fractional count is floating-point
		// residue, and 11.999999 is a report of 12.
		{"rounds to nearest", `{"prompt_tokens":11.9999999}`, 12},
		{"negative", `{"prompt_tokens":"-1"}`, -1},
		{"zero", `{"prompt_tokens":"0"}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got countBlock
			if err := util.DecodeCounts([]byte(tc.raw), &got); err != nil {
				t.Fatalf("DecodeCounts(%s): %v", tc.raw, err)
			}
			if got.Prompt != tc.want {
				t.Errorf("prompt = %d, want %d", got.Prompt, tc.want)
			}
		})
	}
}

// The tolerance is for how a number is written. Anything else is a member
// holding something that is not a count, and it must still fail — with the
// decoder's own error, not one invented on the way past.
func TestDecodeCounts_RefusesWhatIsNotACount(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"prompt_tokens":"lots"}`,
		`{"prompt_tokens":""}`,
		`{"prompt_tokens":{"value":12}}`,
		`{"prompt_tokens":[12]}`,
		`{"prompt_tokens":true}`,
		// Past any context window. Rounding this into an int would report a
		// number the provider never sent.
		`{"prompt_tokens":1e30}`,
		`{"prompt_tokens":"1e30"}`,
		// Not a number at all, however it is spelled.
		`{"prompt_tokens":"NaN"}`,
		`{"prompt_tokens":"Inf"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var got countBlock
			if err := util.DecodeCounts([]byte(raw), &got); err == nil {
				t.Errorf("decoded as a count: %d", got.Prompt)
			}
		})
	}
}

// A string field that happens to hold digits is not touched. Only the member the
// decoder named, and only when the struct wants an integer there.
func TestDecodeCounts_LeavesNonCountFieldsAlone(t *testing.T) {
	t.Parallel()
	var got countBlock
	if err := util.DecodeCounts([]byte(`{"label":"12","prompt_tokens":"7"}`), &got); err != nil {
		t.Fatalf("DecodeCounts: %v", err)
	}
	if got.Label != "12" {
		t.Errorf("label = %q, want the string it arrived as", got.Label)
	}
	if got.Prompt != 7 {
		t.Errorf("prompt = %d, want 7", got.Prompt)
	}
}

// Every differently-spelled count in one object, including a nested one, and
// each fixed on its own pass.
func TestDecodeCounts_SeveralMembersAndNesting(t *testing.T) {
	t.Parallel()
	var got countBlock
	raw := `{"prompt_tokens":"12","completion_tokens":3.0,"details":{"cached_tokens":"8"}}`
	if err := util.DecodeCounts([]byte(raw), &got); err != nil {
		t.Fatalf("DecodeCounts: %v", err)
	}
	if got.Prompt != 12 || got.Completion != 3 {
		t.Errorf("got prompt=%d completion=%d, want 12/3", got.Prompt, got.Completion)
	}
	if got.Details == nil || got.Details.Cached != 8 {
		t.Errorf("nested count = %+v, want 8", got.Details)
	}
}

// The caller's bytes are its own: the rewrite is fed to the decoder and never
// handed back, because callers read those same bytes for what this struct does
// not model.
func TestDecodeCounts_DoesNotMutateTheCallersBytes(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"prompt_tokens":"12"}`)
	original := string(raw)
	var got countBlock
	if err := util.DecodeCounts(raw, &got); err != nil {
		t.Fatalf("DecodeCounts: %v", err)
	}
	if string(raw) != original {
		t.Errorf("input was rewritten in place: %q, want %q", raw, original)
	}
}

// A document that is not JSON, or not an object, never reaches the rewrite: the
// decoder's error is not an UnmarshalTypeError and comes straight back.
func TestDecodeCounts_PassesThroughOtherErrors(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"prompt_tokens":`, `[1,2]`, `"nope"`} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var got countBlock
			if err := util.DecodeCounts([]byte(raw), &got); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// The retry is bounded. A document with more differently-spelled counts than the
// bound fails rather than looping, and one within it succeeds — which is what
// makes the bound a limit rather than a number nothing ever reaches.
func TestDecodeCounts_RetryIsBounded(t *testing.T) {
	t.Parallel()
	type wide struct {
		A, B, C, D, E, F, G, H, I, J int
	}
	build := func(n int) string {
		names := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}
		parts := make([]string, 0, len(names))
		for i, name := range names {
			if i < n {
				parts = append(parts, `"`+name+`":"1"`)
			} else {
				parts = append(parts, `"`+name+`":1`)
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	var within wide
	if err := util.DecodeCounts([]byte(build(8)), &within); err != nil {
		t.Errorf("eight quoted counts must decode: %v", err)
	}
	var beyond wide
	if err := util.DecodeCounts([]byte(build(10)), &beyond); err == nil {
		t.Error("ten quoted counts must exhaust the retry bound rather than loop")
	}
}

// json.Number round-trips as the literal that was written, so a value the
// rewrite does not touch is not reformatted on its way to the decoder.
func TestDecodeCounts_LeavesOtherNumbersLiteral(t *testing.T) {
	t.Parallel()
	var got struct {
		Prompt int             `json:"prompt_tokens"`
		Cost   json.RawMessage `json:"cost"`
	}
	if err := util.DecodeCounts([]byte(`{"prompt_tokens":"12","cost":0.00000004}`), &got); err != nil {
		t.Fatalf("DecodeCounts: %v", err)
	}
	if string(got.Cost) != "0.00000004" {
		t.Errorf("cost = %s, want the literal the provider wrote", got.Cost)
	}
}
