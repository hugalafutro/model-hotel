package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// A chat request asking for image output names IMAGE among Gemini's response
// modalities; one that does not leaves the config without them.
func TestTranslateRequest_ImageModalities(t *testing.T) {
	body := []byte(`{"model":"gemini-3-pro-image","messages":[{"role":"user","content":"a blue circle"}],"modalities":["image","text"]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	var req struct {
		GenerationConfig struct {
			ResponseModalities []string `json:"responseModalities"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := strings.Join(req.GenerationConfig.ResponseModalities, ","); got != "TEXT,IMAGE" {
		t.Fatalf("responseModalities = %q, want TEXT,IMAGE", got)
	}
	plain, _, _, err := TranslateRequest([]byte(`{"model":"gemini-3-pro-image","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if strings.Contains(string(plain), "responseModalities") {
		t.Fatalf("a request without modalities set responseModalities: %s", plain)
	}
}

// A generated image part becomes an image_url data URL beside the text, in
// the non-streaming message and in a streamed delta alike.
func TestImageParts_BecomeImages(t *testing.T) {
	upstream := `{"candidates":[{"content":{"parts":[{"text":"Here it is."},{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`
	out, err := BuildChatCompletion([]byte(upstream), "chatcmpl-1", "gemini-3-pro-image", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Images  []struct {
					Type     string `json:"type"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg := resp.Choices[0].Message
	if msg.Content != "Here it is." {
		t.Fatalf("content = %q", msg.Content)
	}
	if len(msg.Images) != 1 || msg.Images[0].Type != "image_url" || msg.Images[0].ImageURL.URL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("images = %+v", msg.Images)
	}

	tr := NewStreamTranslator("chatcmpl-2", "gemini-3-pro-image", 1)
	chunk, err := tr.Translate([]byte(upstream))
	if err != nil {
		t.Fatalf("stream translate: %v", err)
	}
	if !strings.Contains(string(chunk), `"images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]`) {
		t.Fatalf("streamed delta carries no image: %s", chunk)
	}
}
