package anthropicegress

import "testing"

func TestNeedsNativeRouting(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsNativeRouting([]byte(tt.body)); got != tt.want {
				t.Errorf("NeedsNativeRouting() = %v, want %v", got, tt.want)
			}
		})
	}
}
