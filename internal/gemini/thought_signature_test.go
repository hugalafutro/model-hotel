package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

const signedCall = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_temperature","args":{"city":"Prague"}},"thoughtSignature":"sig-abc"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}`

// Gemini 3 signs each function call and refuses the follow-up turn without
// the signature, so it travels out on the tool call as
// extra_content.google.thought_signature, in the completion and in the
// stream alike, and an unsigned call carries no extra_content at all.
func TestToolCall_CarriesThoughtSignatureOut(t *testing.T) {
	out, err := BuildChatCompletion([]byte(signedCall), "chatcmpl-1", "gemini-3.1-pro", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(string(out), `"extra_content":{"google":{"thought_signature":"sig-abc"}}`) {
		t.Fatalf("completion lost the signature: %s", out)
	}
	tr := NewStreamTranslator("chatcmpl-2", "gemini-3.1-pro", 1)
	chunk, err := tr.Translate([]byte(signedCall))
	if err != nil {
		t.Fatalf("stream translate: %v", err)
	}
	if !strings.Contains(string(chunk), `"extra_content":{"google":{"thought_signature":"sig-abc"}}`) {
		t.Fatalf("chunk lost the signature: %s", chunk)
	}
	unsigned := strings.Replace(signedCall, `,"thoughtSignature":"sig-abc"`, "", 1)
	out, err = BuildChatCompletion([]byte(unsigned), "chatcmpl-3", "gemini-3.1-pro", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(string(out), "extra_content") {
		t.Fatalf("an unsigned call grew an extra_content member: %s", out)
	}
}

// The follow-up turn puts the signature back beside the functionCall part.
func TestTranslateRequest_ThoughtSignatureBackOnTheCall(t *testing.T) {
	body := `{"model":"gemini-3.1-pro","messages":[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_temperature","arguments":"{\"city\":\"Prague\"}"},"extra_content":{"google":{"thought_signature":"sig-abc"}}}]},
		{"role":"tool","tool_call_id":"call_1","content":"-7"}]}`
	out, _, _, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	var req struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				FunctionCall     *json.RawMessage `json:"functionCall"`
				ThoughtSignature string           `json:"thoughtSignature"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, c := range req.Contents {
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				found = true
				if p.ThoughtSignature != "sig-abc" {
					t.Fatalf("functionCall part signature = %q, want sig-abc", p.ThoughtSignature)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no functionCall part in %s", out)
	}
	plain := strings.Replace(body, `,"extra_content":{"google":{"thought_signature":"sig-abc"}}`, "", 1)
	out, _, _, err = TranslateRequest([]byte(plain))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if strings.Contains(string(out), "thoughtSignature") {
		t.Fatalf("an unsigned call sent a thoughtSignature: %s", out)
	}
}
