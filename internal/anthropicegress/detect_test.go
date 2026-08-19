package anthropicegress

import "testing"

type routingCase struct {
	name string
	body string
	want bool
}

// routingCases is shared by TestNeedsNativeRouting and the pre-filter test, so
// the cheap byte scan is exercised against every routing decision the detector
// makes rather than a separate list that can drift from it.
func routingCases() []routingCase {
	return []routingCase{
		{
			name: "file part",
			body: `{"messages":[{"role":"user","content":[
				{"type":"text","text":"summarise"},
				{"type":"file","file":{"file_data":"data:application/pdf;base64,JVBERi0="}}]}]}`,
			want: true,
		},
		{
			name: "file part by url",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		{
			name: "pdf smuggled through the image slot",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:application/pdf;base64,JVBERi0="}}]}]}`,
			want: true,
		},
		{
			name: "data uri with no media type",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:;base64,JVBERi0="}}]}]}`,
			want: true,
		},
		{
			name: "file part in a later message",
			body: `{"messages":[
				{"role":"user","content":"hello"},
				{"role":"assistant","content":"hi"},
				{"role":"user","content":[{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		// An unrecognised role is translated as a user turn, so its document
		// arrives intact.
		{
			name: "file part on an unrecognised role",
			body: `{"messages":[{"role":"human","content":[
				{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		// A part type spelled with \uXXXX escapes carries none of the literal
		// hint bytes, so the pre-filter has to fall through to the full decode
		// on the escape alone.
		{
			name: "file part spelled with unicode escapes",
			body: `{"messages":[{"role":"user","content":[
				{"type":"\u0066ile","\u0066ile":{"\u0066ile_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		// document is not in Anthropic's assistant content-block schema, so an
		// assistant document is a guaranteed 400 on the native route.
		{
			name: "file part on an assistant turn",
			body: `{"messages":[{"role":"assistant","content":[
				{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: false,
		},
		// The roles below drop their documents in translation (system and
		// developer content is flattened into the system prompt, a tool message
		// into a text tool_result). Routing native for one would ask the model
		// about a file that never arrived; the compat endpoint 400s instead.
		{
			name: "file part on a system turn",
			body: `{"messages":[
				{"role":"system","content":[{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]},
				{"role":"user","content":"summarise it"}]}`,
			want: false,
		},
		{
			name: "file part on a developer turn",
			body: `{"messages":[
				{"role":"developer","content":[{"type":"file","file":{"file_data":"data:application/pdf;base64,JVBERi0="}}]},
				{"role":"user","content":"summarise it"}]}`,
			want: false,
		},
		{
			name: "file part on a tool turn",
			body: `{"messages":[
				{"role":"tool","tool_call_id":"call_1","content":[{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: false,
		},
		{
			name: "smuggled pdf on a system turn",
			body: `{"messages":[
				{"role":"system","content":[{"type":"image_url","image_url":{"url":"data:application/pdf;base64,JVBERi0="}}]}]}`,
			want: false,
		},
		{
			name: "system document beside a user document still routes native",
			body: `{"messages":[
				{"role":"system","content":[{"type":"file","file":{"file_url":"https://example.com/ignored.pdf"}}]},
				{"role":"user","content":[{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		{
			name: "base64 image data uri",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0="}}]}]}`,
			want: false,
		},
		{
			name: "image media type is matched case-insensitively",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:IMAGE/JPEG;base64,/9j/4AAQ"}}]}]}`,
			want: false,
		},
		{
			name: "image data uri with extra parameters",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:image/webp;charset=binary;base64,UklGRg=="}}]}]}`,
			want: false,
		},
		{
			name: "http image url",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}]}`,
			want: false,
		},
		{
			name: "truncated data uri stays on the compat path",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:application/pdf;base64"}}]}]}`,
			want: false,
		},
		{
			name: "image_url part with no image_url object",
			body: `{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			want: false,
		},
		// Every shape below is one TestTranslateRequest_UnusableFileAndImagePartsAreDropped
		// pins as DROPPED by translateBlocks. Routing native on one would send a
		// request whose document never arrives and answer it with a confident
		// 200; the compat endpoint 400s instead, so they stay on it. These cases
		// exist to keep the gate and the translation from drifting apart.
		{
			name: "file part carrying only a file_id",
			body: `{"messages":[{"role":"user","content":[
				{"type":"text","text":"summarise"},
				{"type":"file","file":{"file_id":"file-abc"}}]}]}`,
			want: false,
		},
		{
			name: "file part with no file object",
			body: `{"messages":[{"role":"user","content":[{"type":"file"}]}]}`,
			want: false,
		},
		{
			name: "file part with an empty file object",
			body: `{"messages":[{"role":"user","content":[{"type":"file","file":{}}]}]}`,
			want: false,
		},
		{
			name: "file_data that is not a data uri",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_data":"JVBERi0="}}]}]}`,
			want: false,
		},
		{
			name: "file_data data uri with no payload",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_data":"data:application/pdf;base64,"}}]}]}`,
			want: false,
		},
		{
			name: "undecodable base64 text payload",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_data":"data:text/plain;base64,!!not-base64!!"}}]}]}`,
			want: false,
		},
		{
			name: "malformed percent-encoded pdf file part",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_data":"data:application/pdf,%PDF-1.4"}}]}]}`,
			want: false,
		},
		{
			name: "malformed percent-encoded pdf in the image slot",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"data:application/pdf,%PDF-1.4"}}]}]}`,
			want: false,
		},
		{
			name: "empty image url",
			body: `{"messages":[{"role":"user","content":[
				{"type":"image_url","image_url":{"url":""}}]}]}`,
			want: false,
		},
		// A dropped file part beside a translatable one still routes native:
		// the surviving document is what the request is about.
		{
			name: "dropped file part beside a usable one",
			body: `{"messages":[{"role":"user","content":[
				{"type":"file","file":{"file_id":"file-abc"}},
				{"type":"file","file":{"file_url":"https://example.com/a.pdf"}}]}]}`,
			want: true,
		},
		{
			name: "text parts only",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			want: false,
		},
		{
			name: "string content",
			body: `{"messages":[{"role":"user","content":"hello"}]}`,
			want: false,
		},
		{
			name: "null content",
			body: `{"messages":[{"role":"assistant","content":null}]}`,
			want: false,
		},
		{
			name: "no messages",
			body: `{"model":"claude-sonnet-4-5"}`,
			want: false,
		},
		{
			name: "unparseable body",
			body: `{"messages":[`,
			want: false,
		},
		{
			name: "empty body",
			body: ``,
			want: false,
		},
	}
}

func TestNeedsNativeRouting(t *testing.T) {
	for _, tt := range routingCases() {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsNativeRouting([]byte(tt.body)); got != tt.want {
				t.Errorf("NeedsNativeRouting() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The pre-filter is one-sided: it may let a body through that does not route
// native, but it must never rule out one that does, or the adapter silently
// stops working for the requests it exists to serve.
func TestMayNeedNativeRouting_NeverRulesOutANativeRequest(t *testing.T) {
	for _, tt := range routingCases() {
		if !tt.want {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			if !mayNeedNativeRouting([]byte(tt.body)) {
				t.Errorf("mayNeedNativeRouting() = false for a body that routes native")
			}
		})
	}
}

// An image/* media type outside the four Anthropic accepts is a document as far
// as the translation is concerned, so the pre-filter must not mistake the
// "image/" prefix for proof that a payload is an image.
func TestMayNeedNativeRouting_UnacceptedImageTypeFallsThrough(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[
		{"type":"image_url","image_url":{"url":"data:image/svg+xml;base64,PHN2Zz4="}}]}]}`)
	if !mayNeedNativeRouting(body) {
		t.Fatal("mayNeedNativeRouting() = false for an image/svg+xml data uri")
	}
	if !NeedsNativeRouting(body) {
		t.Error("NeedsNativeRouting() = false, want true (svg is not an Anthropic image type)")
	}
}

// The bodies the pre-filter is for: the text and vision traffic that makes up
// almost every request to an Anthropic provider, ruled out on a byte scan
// without materialising a single base64 payload.
func TestMayNeedNativeRouting_SkipsTheCommonPath(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"plain text", `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`},
		{"text parts", `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`},
		{"http image url", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}]}]}`},
		// The payload the pre-filter is worth having for: a base64 image never
		// routes native, and the full decode would copy the whole thing.
		{"base64 png", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0="}}]}]}`},
		{"base64 jpeg, upper case media type", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:IMAGE/JPEG;base64,/9j/4AAQ"}}]}]}`},
		{"base64 webp with extra parameters", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/webp;charset=binary;base64,UklGRg=="}}]}]}`},
		{"percent-encoded gif", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/gif,GIF89a"}}]}]}`},
		{"two base64 images", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0="}},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,/9j/4AAQ"}}]}]}`},
		{"tool result", `{"messages":[{"role":"tool","tool_call_id":"call_1","content":"42"}]}`},
		{"empty body", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if mayNeedNativeRouting([]byte(tt.body)) {
				t.Errorf("mayNeedNativeRouting() = true, want false (no full decode needed)")
			}
		})
	}
}
