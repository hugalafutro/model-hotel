package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// A token count written in a spelling other than the plain integer the schema
// asks for is still a count — quoted, or carrying a fraction because a relay did
// its arithmetic in floating point. Here it does not merely blank the usage: the
// counts sit inside the response object, so the whole translation fails and the
// caller loses the answer the model already produced. Since #812 it also charges
// the provider's circuit breaker for a body it in fact answered.
func TestBuildChatCompletion_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		usage string
	}{
		{"plain integers", `{"promptTokenCount":12,"candidatesTokenCount":3,"totalTokenCount":15}`},
		{"quoted", `{"promptTokenCount":"12","candidatesTokenCount":"3","totalTokenCount":"15"}`},
		{"floating point", `{"promptTokenCount":12.0,"candidatesTokenCount":3.0,"totalTokenCount":15.0}`},
		{"mixed", `{"promptTokenCount":"12","candidatesTokenCount":3.0,"totalTokenCount":15}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":` + tc.usage + `}`
			out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
			if err != nil {
				t.Fatalf("a count spelling cost the caller the whole answer: %v", err)
			}
			if !strings.Contains(string(out), "hello") {
				t.Errorf("the answer was lost: %s", out)
			}
			var got struct {
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("re-encode did not decode: %v", err)
			}
			if got.Usage.PromptTokens != 12 || got.Usage.CompletionTokens != 3 {
				t.Errorf("metered %d/%d, want 12/3", got.Usage.PromptTokens, got.Usage.CompletionTokens)
			}
		})
	}
}

// A usage member that is not a count in any spelling costs the usage and nothing
// else: the answer is what the caller came for.
func TestBuildChatCompletion_UnreadableUsageKeepsTheAnswer(t *testing.T) {
	t.Parallel()
	for _, usage := range []string{`{"promptTokenCount":"lots"}`, `{"promptTokenCount":[12]}`, `[]`, `"none"`} {
		t.Run(usage, func(t *testing.T) {
			t.Parallel()
			body := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":` + usage + `}`
			out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
			if err != nil {
				t.Fatalf("an unreadable usage member cost the caller the answer: %v", err)
			}
			if !strings.Contains(string(out), "hello") {
				t.Errorf("the answer was lost: %s", out)
			}
		})
	}
}

// An explicit null is not a usage block. The *genUsage this replaced was nil for
// absent AND null alike; a json.RawMessage for null is four non-empty bytes, so
// reading only the length let a null chunk overwrite the counts an earlier chunk
// had reported — and turned an omitted usage into a positive claim of zero.
func TestStreamTranslator_ANullUsageDoesNotWipeTheCountsAlreadyReported(t *testing.T) {
	t.Parallel()
	tr := NewStreamTranslator("id", "m", 0)
	if _, err := tr.Translate([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"totalTokenCount":15}}`)); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if _, err := tr.Translate([]byte(`{"candidates":[],"usageMetadata":null}`)); err != nil {
		t.Fatalf("null chunk: %v", err)
	}
	out, err := tr.Finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !strings.Contains(string(out), `"prompt_tokens":12`) {
		t.Errorf("a null usage wiped the counts the provider reported: %s", out)
	}
}

func TestBuildChatCompletion_ANullUsageIsNotAZeroedOne(t *testing.T) {
	t.Parallel()
	body := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":null}`
	out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if strings.Contains(string(out), `"usage"`) {
		t.Errorf("an omitted usage was reported as zero tokens: %s", out)
	}
}

// One unreadable member costs the WHOLE usage, and that is deliberate — the
// opposite of the rule proxy.Usage uses.
//
// That rule keeps whatever decoded because its members are independent. These
// figures are derived: summed across members, or falling back to a sum. A lost
// addend would leave a number that is wrong AND non-zero, which reads as
// authoritative and stops estimateMissingUsage ever firing — a cache-read count
// of 20000 lost that way bills 4. Absent is the honest report, and the estimator
// then does its job.
func TestBuildChatCompletion_AnUnreadableMemberCostsTheWholeUsage(t *testing.T) {
	t.Parallel()
	body := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":"lots","totalTokenCount":15}}`
	out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("the answer was lost with the usage: %s", out)
	}
	if strings.Contains(string(out), `"usage"`) {
		t.Errorf("a half-read usage was reported as authoritative: %s", out)
	}
}
