package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"
)

// A token count in a spelling other than the plain integer the schema asks for
// is still a count. Decoded as part of the response object, one of them failed
// the whole translation and cost the caller the answer the model had already
// produced — and since #812 charged the provider's circuit breaker for a body it
// in fact answered.
func TestTranslateResponsesToChat_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		usage string
	}{
		{"plain integers", `{"input_tokens":12,"output_tokens":3,"total_tokens":15}`},
		{"quoted", `{"input_tokens":"12","output_tokens":"3","total_tokens":"15"}`},
		{"floating point", `{"input_tokens":12.0,"output_tokens":3.0,"total_tokens":15.0}`},
		{"a nested count quoted", `{"input_tokens":12,"output_tokens":3,"output_tokens_details":{"reasoning_tokens":"7"}}`},
		// Not a count in any spelling: the usage is lost, the answer is not.
		{"unreadable", `{"input_tokens":"lots"}`},
		{"not an object", `[]`},
		{"null", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":` + tc.usage + `}`
			out, err := TranslateResponsesToChat([]byte(body), "m")
			if err != nil {
				t.Fatalf("a usage member cost the caller the whole answer: %v", err)
			}
			if !strings.Contains(string(out), "hello") {
				t.Errorf("the answer was lost: %s", out)
			}
		})
	}
}

// And when it IS readable, whatever the spelling, it is metered.
func TestTranslateResponsesToChat_MetersASpelledCount(t *testing.T) {
	t.Parallel()
	body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":"12","output_tokens":3.0,"total_tokens":"15"}}`
	out, err := TranslateResponsesToChat([]byte(body), "m")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	var got struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-encode did not decode: %v", err)
	}
	if got.Usage.PromptTokens != 12 || got.Usage.CompletionTokens != 3 || got.Usage.TotalTokens != 15 {
		t.Errorf("metered %d/%d/%d, want 12/3/15", got.Usage.PromptTokens, got.Usage.CompletionTokens, got.Usage.TotalTokens)
	}
}

// The nested count is read, not merely tolerated. Asserting "no error" alone
// would pass just as well if the whole details object were silently dropped.
func TestTranslateResponsesToChat_ReadsANestedCount(t *testing.T) {
	t.Parallel()
	body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":12,"output_tokens":3,"output_tokens_details":{"reasoning_tokens":"7"}}}`
	out, err := TranslateResponsesToChat([]byte(body), "m")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !strings.Contains(string(out), `"reasoning_tokens":7`) {
		t.Errorf("the nested count was dropped rather than read: %s", out)
	}
}

// An omitted or unreadable usage is reported as absent, not as a positive claim
// of zero tokens. The Responses API itself emits "usage": null on a non-terminal
// snapshot, and the *Usage this replaced was nil for that.
func TestTranslateResponsesToChat_AnAbsentUsageIsNotAZeroedOne(t *testing.T) {
	t.Parallel()
	for _, usage := range []string{`null`, `[]`, `"none"`} {
		t.Run(usage, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":` + usage + `}`
			out, err := TranslateResponsesToChat([]byte(body), "m")
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if strings.Contains(string(out), `"usage"`) {
				t.Errorf("an unreadable usage was reported as zero tokens: %s", out)
			}
		})
	}
}

// And one unreadable member must not cost the counts beside it.
func TestTranslateResponsesToChat_KeepsTheCountsItCouldRead(t *testing.T) {
	t.Parallel()
	body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":12,"output_tokens":3,"output_tokens_details":[]}}`
	out, err := TranslateResponsesToChat([]byte(body), "m")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !strings.Contains(string(out), `"prompt_tokens":12`) {
		t.Errorf("readable counts were thrown away with the unreadable member beside them: %s", out)
	}
}
