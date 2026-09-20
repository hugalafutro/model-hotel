package gemini

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// MALFORMED_FUNCTION_CALL carries no answer: the non-streaming translation
// refuses it as the provider's failure instead of handing the caller an empty
// "stop", and the streaming one ends with an error frame.
func TestMalformedFunctionCall_IsNotAStop(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":8,"totalTokenCount":8}}`)
	if _, err := BuildChatCompletion(body, "id", "gemini-2.5-flash", 1); !errors.Is(err, ErrMalformedFunctionCall) {
		t.Fatalf("BuildChatCompletion err = %v, want ErrMalformedFunctionCall", err)
	}

	tr := NewStreamTranslator("id", "m", 0)
	if _, err := tr.Translate(body); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	frames := parseFrames(t, fin)
	if len(frames) != 2 || frames[1] != "[DONE]" {
		t.Fatalf("finish frames = %v, want an error frame then [DONE]", frames)
	}
	if !strings.Contains(frames[0], `"error"`) || !strings.Contains(frames[0], "malformed function call") {
		t.Errorf("terminal frame = %s, want the malformed-call error", frames[0])
	}
}

// cachedContentTokenCount is the part of the prompt Gemini served from its
// context cache; it is spelled as prompt_tokens_details.cached_tokens so the
// metering applies the cache-hit price to it.
func TestTranslateUsage_CachedContentTokens(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":60,"candidatesTokenCount":2,"totalTokenCount":102}}`)
	out, err := BuildChatCompletion(body, "id", "gemini-2.5-flash", 1)
	if err != nil {
		t.Fatalf("BuildChatCompletion: %v", err)
	}
	var m struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Usage.PromptTokens != 100 || m.Usage.PromptTokensDetails == nil || m.Usage.PromptTokensDetails.CachedTokens != 60 {
		t.Errorf("usage = %+v, want 100 prompt tokens with 60 cached", m.Usage)
	}

	// Absent: no details member at all, not a zero one.
	out, err = BuildChatCompletion([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`), "id", "m", 1)
	if err != nil {
		t.Fatalf("BuildChatCompletion: %v", err)
	}
	if strings.Contains(string(out), "prompt_tokens_details") {
		t.Errorf("no cached tokens reported, but details were emitted: %s", out)
	}

	// Unreadable: the cached figure is dropped on its own, the prompt count
	// beside it stays.
	out, err = BuildChatCompletion([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"cachedContentTokenCount":"lots","candidatesTokenCount":2,"totalTokenCount":7}}`), "id", "m", 1)
	if err != nil {
		t.Fatalf("BuildChatCompletion: %v", err)
	}
	if strings.Contains(string(out), "prompt_tokens_details") || !strings.Contains(string(out), `"prompt_tokens":5`) {
		t.Errorf("an unreadable cached count must drop only itself: %s", out)
	}
}
