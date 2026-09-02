package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// A Google AI Studio candidate takes the native route only for image output:
// an image model by its discovered output modalities, or a request naming an
// image modality for a model discovery has not marked. Plain chat stays on
// the compat layer, and the override paths never qualify.
func TestIsGeminiEgressAttempt_GoogleImage(t *testing.T) {
	plain := &requestState{bodyBytes: []byte(`{"messages":[{"role":"user","content":"hi"}]}`)}
	wantsImage := &requestState{bodyBytes: []byte(`{"messages":[{"role":"user","content":"draw"}],"modalities":["image","text"]}`)}
	cases := []struct {
		name         string
		st           *requestState
		providerType string
		outputMods   string
		want         bool
	}{
		{"image model by modalities", plain, "google", `["text","image"]`, true},
		{"request names image", wantsImage, "google", "", true},
		{"chat model, plain request", plain, "google", `["text"]`, false},
		{"chat model, empty modalities", plain, "google", "", false},
		{"image model on another compat provider", plain, "openai", `["text","image"]`, false},
		{"explicit endpoint override", &requestState{bodyBytes: plain.bodyBytes, endpointPath: "/v1/embeddings"}, "google", `["text","image"]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGeminiEgressAttempt(tc.st, tc.providerType, "gemini-2.5-flash-image", tc.outputMods); got != tc.want {
				t.Errorf("isGeminiEgressAttempt = %v, want %v", got, tc.want)
			}
		})
	}

	if got := geminiEgressEndpoint("google", "gemini-2.5-flash-image", false); got != "/models/gemini-2.5-flash-image:generateContent" {
		t.Errorf("endpoint = %q", got)
	}
	req := httptest.NewRequest("POST", "http://x", nil)
	setGeminiEgressAuth(req, "google", "studio-key")
	if req.Header.Get("x-goog-api-key") != "studio-key" || req.Header.Get("Authorization") != "" {
		t.Errorf("google egress auth headers = %v", req.Header)
	}
	if got := provider.GoogleNativeBaseURL("https://generativelanguage.googleapis.com/v1beta/openai"); got != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("native base = %q", got)
	}
}

// A chat request to a Google AI Studio image model lands on the native
// generateContent route under the /v1beta base with the x-goog-api-key
// header, and the generated image comes back as a data URL in
// message.images beside the text.
func TestChatCompletions_GoogleImageEgress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-api-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("auth headers = %v", r.Header)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/v1beta/models/gemini-2.5-flash-image:generateContent") || strings.Contains(r.URL.Path, "/openai/") {
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if _, ok := reqBody["contents"]; !ok {
			t.Errorf("upstream got untranslated body: %v", reqBody)
		}
		gc, _ := reqBody["generationConfig"].(map[string]any)
		if mods, _ := gc["responseModalities"].([]any); len(mods) != 2 {
			t.Errorf("responseModalities = %v", gc["responseModalities"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"A blue circle."},{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`))
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`UPDATE providers SET base_url = 'http://generativelanguage.googleapis.com/v1beta/openai' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("failed to update provider base URL: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET model_id = 'gemini-2.5-flash-image', output_modalities = '["text","image"]' WHERE id = $1`, env.ModelID); err != nil {
		t.Fatalf("failed to update model: %v", err)
	}
	provider.InvalidateProviderCache()
	model.InvalidateModelCache()
	target := upstream.Listener.Addr().String()
	env.Handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}

	body := fmt.Sprintf(`{"model":"%s/gemini-2.5-flash-image","messages":[{"role":"user","content":"draw a blue circle"}],"modalities":["image","text"]}`, env.ProviderName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req.WithContext(ctx))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d\n%s", w.Code, w.Body.String())
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
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	msg := resp.Choices[0].Message
	if msg.Content != "A blue circle." || len(msg.Images) != 1 || msg.Images[0].ImageURL.URL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("message = %+v", msg)
	}
}
