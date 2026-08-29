package anthropic

import "testing"

// A count is a count however the provider spelled it. A plain int field met
// neither the quoted nor the fractional form, so ParseResponseUsage returned
// nothing and the request metered at zero — the caller's quota debited for an
// answer it received.
func TestParseResponseUsage_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		usage           string
		wantIn, wantOut int
	}{
		{"plain integers", `{"input_tokens":12,"output_tokens":3}`, 12, 3},
		{"quoted", `{"input_tokens":"12","output_tokens":"3"}`, 12, 3},
		{"floating point", `{"input_tokens":12.0,"output_tokens":3.0}`, 12, 3},
		{"mixed", `{"input_tokens":"12","output_tokens":3}`, 12, 3},
		// Not a count in any spelling: nothing is invented for it.
		{"unreadable", `{"input_tokens":"lots"}`, 0, 0},
		{"absent", ``, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hi"}]}`
			if tc.usage != "" {
				body = `{"type":"message","content":[{"type":"text","text":"hi"}],"usage":` + tc.usage + `}`
			}
			got := ParseResponseUsage([]byte(body))
			if got.PromptTokens != tc.wantIn || got.CompletionTokens != tc.wantOut {
				t.Errorf("got %d/%d, want %d/%d", got.PromptTokens, got.CompletionTokens, tc.wantIn, tc.wantOut)
			}
		})
	}
}
