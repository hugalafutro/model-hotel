package gemini

import (
	"encoding/json"
	"io"
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

// An image model drafts interim pictures as thought parts; only the final
// image is the answer. A blob with no bytes or a non-image type is nothing.
func TestImageParts_ThoughtAndEmptyBlobsAreSkipped(t *testing.T) {
	upstream := `{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"ZHJhZnQ="}},{"inlineData":{"mimeType":"image/png","data":""}},{"inlineData":{"mimeType":"audio/L16","data":"AAAA"}},{"inlineData":{"mimeType":"image/png","data":"ZmluYWw="}}]},"finishReason":"STOP"}]}`
	out, err := BuildChatCompletion([]byte(upstream), "chatcmpl-3", "gemini-3-pro-image", 1)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if n := strings.Count(string(out), `"image_url":{"url":"data:image/png;base64,`); n != 1 || !strings.Contains(string(out), "base64,ZmluYWw=") {
		t.Fatalf("images in completion = %d, want the final image only: %s", n, out)
	}
	tr := NewStreamTranslator("chatcmpl-4", "gemini-3-pro-image", 1)
	chunk, err := tr.Translate([]byte(upstream))
	if err != nil {
		t.Fatalf("stream translate: %v", err)
	}
	if n := strings.Count(string(chunk), `"image_url":{"url":"data:image/png;base64,`); n != 1 || strings.Contains(string(chunk), "ZHJhZnQ=") {
		t.Fatalf("images in chunk = %d, want the final image only: %s", n, chunk)
	}
	thoughtOnly, err := tr.Translate([]byte(`{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"ZHJhZnQ="}}]}}]}`))
	if err != nil || thoughtOnly != nil {
		t.Fatalf("a thought-only chunk emitted %q, %v", thoughtOnly, err)
	}
}

// An image-only modalities list asks Gemini for the picture alone.
func TestTranslateRequest_ImageOnlyModalities(t *testing.T) {
	out, _, _, err := TranslateRequest([]byte(`{"model":"gemini-2.5-flash-image","messages":[{"role":"user","content":"a circle"}],"modalities":["image"]}`))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if !strings.Contains(string(out), `"responseModalities":["IMAGE"]`) {
		t.Fatalf("image-only request sent %s", out)
	}
}

// A streamed picture arrives as one SSE event of several MiB; the gemini
// adapter's cap admits it where the text dialects' 4 MiB would fail the
// stream.
func TestStreamAdapter_LargeImageEventPasses(t *testing.T) {
	big := strings.Repeat("A", 5<<20)
	event := "data: " + `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + big + `"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1290,"totalTokenCount":1293}}` + "\n\n"
	ad := NewStreamAdapter(io.NopCloser(strings.NewReader(event)), "gemini-3-pro-image")
	out, err := io.ReadAll(ad)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(out), "base64,"+big[:64]) || !strings.Contains(string(out), "[DONE]") {
		t.Fatalf("large image event did not pass: %d bytes, done=%v", len(out), strings.Contains(string(out), "[DONE]"))
	}
}
