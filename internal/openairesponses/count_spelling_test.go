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

// A member that could not be read costs the figures it FEEDS, and nothing else.
//
// Two blunter rules were tried and both were wrong. Keeping whatever decoded
// corrupted a SUMMED figure: an Anthropic prompt count is input plus both cache
// counts, so a cache-read of 20000 lost to an unreadable sibling bills 4 — and 4
// is non-zero, so the estimator never replaces it. Dropping the whole block
// instead threw away counts read straight off one member, which are never in
// doubt — and a completion count is what tells the circuit breaker the provider
// answered at all, so losing it charges a provider for an answer it gave.
//
// Every figure here is read directly, so an unreadable details block costs
// nothing at all.
func TestTranslateResponsesToChat_AnUnreadableMemberCostsOnlyWhatItFeeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		usage   string
		want    string
		notWant string
	}{
		{"an unreadable details block", `{"input_tokens":1200,"output_tokens":340,"total_tokens":1540,"output_tokens_details":[]}`, `"prompt_tokens":1200`, ""},
		{"an unreadable completion count", `{"input_tokens":1200,"output_tokens":"lots","total_tokens":1540}`, `"prompt_tokens":1200`, `"completion_tokens":340`},
		// The fallback total is the one sum, so a lost addend takes it down.
		{"a lost addend with no stated total", `{"input_tokens":1200,"output_tokens":"lots"}`, `"prompt_tokens":1200`, `"total_tokens":1200`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":` + tc.usage + `}`
			out, err := TranslateResponsesToChat([]byte(body), "m")
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("a figure read straight off its own member was thrown away: %s", out)
			}
			if tc.notWant != "" && strings.Contains(string(out), tc.notWant) {
				t.Errorf("a figure whose input was lost was reported anyway: %s", out)
			}
		})
	}
}

// The streaming surface, which #813 left untested: the terminal usage chunk is
// built by finishChunks from the raw usage member held on the response event, so
// the count spellings have to survive the stream's own path to the client, not
// only the non-streaming one. A count read here is what the metering pipeline
// bills.
func TestStreamTranslator_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		usage                   string
		wantIn, wantOut, wantTo int
	}{
		{"plain integers", `{"input_tokens":12,"output_tokens":3,"total_tokens":15}`, 12, 3, 15},
		{"quoted", `{"input_tokens":"12","output_tokens":"3","total_tokens":"15"}`, 12, 3, 15},
		{"fractional", `{"input_tokens":12.0,"output_tokens":3.0,"total_tokens":15.0}`, 12, 3, 15},
		// A member this translator has no field for keeps the counts beside it.
		{"unmodelled sibling", `{"input_tokens":12,"output_tokens":3,"total_tokens":15,"input_tokens_details":[]}`, 12, 3, 15},
		// Not a count in any spelling: the figures read straight off their own
		// member stand, the one that could not be read is not invented.
		{"one figure unreadable", `{"input_tokens":12,"output_tokens":"lots","total_tokens":15}`, 12, 0, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := NewStreamTranslator("m")
			if _, err := tr.TranslateEvent([]byte(`{"type":"response.output_text.delta","delta":"hi"}`)); err != nil {
				t.Fatalf("delta event: %v", err)
			}
			out, err := tr.TranslateEvent([]byte(`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":` + tc.usage + `}}`))
			if err != nil {
				t.Fatalf("a usage member cost the stream its terminal event: %v", err)
			}
			got := streamUsage(t, out)
			if got.PromptTokens != tc.wantIn || got.CompletionTokens != tc.wantOut || got.TotalTokens != tc.wantTo {
				t.Errorf("metered %d/%d/%d, want %d/%d/%d",
					got.PromptTokens, got.CompletionTokens, got.TotalTokens, tc.wantIn, tc.wantOut, tc.wantTo)
			}
		})
	}
}

type streamUsageCounts struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// streamUsage reads the usage block off the last chat chunk that carries one.
// Parsing the frames rather than substring-matching the SSE bytes: "12" is a
// substring of "121", and a count is exactly the thing that must not be matched
// loosely.
func streamUsage(t *testing.T, sse []byte) streamUsageCounts {
	t.Helper()
	var last streamUsageCounts
	var seen bool
	for _, line := range strings.Split(string(sse), "\n") {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || strings.TrimSpace(payload) == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *streamUsageCounts `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("stream frame did not decode: %v", err)
		}
		if chunk.Usage != nil {
			last, seen = *chunk.Usage, true
		}
	}
	if !seen {
		t.Fatalf("no chunk carried a usage block: %s", sse)
	}
	return last
}
