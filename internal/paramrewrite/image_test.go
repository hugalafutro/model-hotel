package paramrewrite

import (
	"encoding/json"
	"strings"
	"testing"
)

// xAI's image API rejects "size" outright, so a request carrying one is
// rewritten into the aspect_ratio it does accept, or has the size dropped
// when no documented ratio stands in for it.
func TestRewriteImageRequest_XAISizeBecomesAspectRatio(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		body        string
		wantRatio   string // "" means no aspect_ratio member
		wantChosen  string // what the rewrite reports it SET
		wantDropped string
	}{
		{"square", `{"model":"grok-imagine-image","prompt":"p","size":"1024x1024"}`, "1:1", "1:1", "1024x1024"},
		{"dall-e landscape", `{"model":"m","prompt":"p","size":"1792x1024"}`, "16:9", "16:9", "1792x1024"},
		{"dall-e portrait", `{"model":"m","prompt":"p","size":"1024x1792"}`, "9:16", "9:16", "1024x1792"},
		{"gpt-image landscape", `{"model":"m","prompt":"p","size":"1536x1024"}`, "3:2", "3:2", "1536x1024"},
		{"gpt-image portrait", `{"model":"m","prompt":"p","size":"1024x1536"}`, "2:3", "2:3", "1024x1536"},
		{"auto", `{"model":"m","prompt":"p","size":"auto"}`, "", "", "auto"},
		{"garbage", `{"model":"m","prompt":"p","size":"large"}`, "", "", "large"},
		{"zero height", `{"model":"m","prompt":"p","size":"1024x0"}`, "", "", "1024x0"},
		{"ratio nothing documented approximates", `{"model":"m","prompt":"p","size":"1000x1150"}`, "", "", "1000x1150"},
		{"not a string", `{"model":"m","prompt":"p","size":1024}`, "", "", "1024"},
		{"caller already chose a ratio", `{"model":"m","prompt":"p","size":"1024x1024","aspect_ratio":"16:9"}`, "16:9", "", "1024x1024"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped, chosen := RewriteImageRequest([]byte(tc.body), "xai", "Grok-Imagine-Image")
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("rewritten body is not JSON: %v: %s", err, out)
			}
			if _, has := got["size"]; has {
				t.Errorf("size survived the rewrite: %s", out)
			}
			ratio, _ := got["aspect_ratio"].(string)
			if ratio != tc.wantRatio {
				t.Errorf("aspect_ratio = %q, want %q (body %s)", ratio, tc.wantRatio, out)
			}
			if chosen != tc.wantChosen || dropped != tc.wantDropped {
				t.Errorf("reported chosen %q dropped %q, want %q %q", chosen, dropped, tc.wantChosen, tc.wantDropped)
			}
			if got["prompt"] != "p" || got["model"] == nil {
				t.Errorf("unrelated members disturbed: %s", out)
			}
		})
	}
}

// Nothing else is touched: another provider's body, a body with no size, and
// bytes that do not parse all come back exactly as they went in.
func TestRewriteImageRequest_LeavesEverythingElseAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, providerType string
	}{
		{"openai keeps size", `{"model":"dall-e-3","prompt":"p","size":"1024x1024"}`, "openai"},
		{"xai without size", `{"model":"m","prompt":"p","n":2}`, "xai"},
		{"xai unparseable", `{"model":"m","prompt":"p","size":"1024x1024"`, "xai"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped, chosen := RewriteImageRequest([]byte(tc.body), tc.providerType, "grok-imagine-image")
			if got := string(out); got != tc.body || chosen != "" || dropped != "" {
				t.Errorf("body changed (dropped %q chosen %q):\ngot  %s\nwant %s", dropped, chosen, got, tc.body)
			}
		})
	}
}

// Numbers ride through with the literal the caller wrote, the way the model
// rewriter keeps them: a rewritten body must not turn n:1 into 1.0 or lose
// precision on a seed.
func TestRewriteImageRequest_KeepsNumberLiterals(t *testing.T) {
	t.Parallel()
	out, _, _ := RewriteImageRequest([]byte(`{"model":"m","prompt":"p","size":"1024x1024","n":1,"seed":9007199254740993}`), "xai", "grok-imagine-image")
	for _, want := range []string{`"n":1`, `"seed":9007199254740993`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewritten body lost %s: %s", want, out)
		}
	}
}

// The older grok-2-image models take neither size nor aspect_ratio, so for
// them both are dropped and nothing is put in their place: injecting the ratio
// would only trade one "Argument not supported" for another, and a ratio the
// caller sent would draw the same refusal.
func TestRewriteImageRequest_Grok2ImageDropsBothMembers(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"model":"grok-2-image-1212","prompt":"p","size":"1792x1024"}`,
		`{"model":"grok-2-image-1212","prompt":"p","aspect_ratio":"16:9"}`,
		`{"model":"grok-2-image-1212","prompt":"p","size":"1792x1024","aspect_ratio":"16:9"}`,
	} {
		out, _, chosen := RewriteImageRequest([]byte(body), "xai", "grok-2-image-1212")
		if strings.Contains(string(out), "size") || strings.Contains(string(out), "aspect_ratio") || chosen != "" {
			t.Errorf("want size and aspect_ratio dropped, got %s (chosen %q)", out, chosen)
		}
	}
	out, dropped, _ := RewriteImageRequest([]byte(`{"model":"grok-2-image-1212","prompt":"p","n":1}`), "xai", "grok-2-image-1212")
	if string(out) != `{"model":"grok-2-image-1212","prompt":"p","n":1}` || dropped != "" {
		t.Errorf("a body with neither member must pass untouched, got %s", out)
	}
}
