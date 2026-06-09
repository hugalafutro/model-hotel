package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// makeJSONModelRewriter
// ---------------------------------------------------------------------------

func TestMakeJSONModelRewriter_RewritesModel(t *testing.T) {
	body := []byte(`{"model":"prov/text-embedding-3-small","input":["hello","world"],"dimensions":256}`)
	rewrite := makeJSONModelRewriter(body, "prov/text-embedding-3-small")

	out, contentType, err := rewrite("text-embedding-3-small")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contentType != "application/json" {
		t.Errorf("contentType = %q, want application/json", contentType)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("rewritten body is not valid JSON: %v", err)
	}
	if raw["model"] != "text-embedding-3-small" {
		t.Errorf("model = %v, want text-embedding-3-small", raw["model"])
	}
	if raw["dimensions"] != float64(256) {
		t.Errorf("dimensions = %v, want 256 (other fields must survive the rewrite)", raw["dimensions"])
	}
	input, ok := raw["input"].([]interface{})
	if !ok || len(input) != 2 {
		t.Errorf("input = %v, want 2-element array", raw["input"])
	}
}

func TestMakeJSONModelRewriter_SameModelForwardsVerbatim(t *testing.T) {
	body := []byte(`{"model":"text-embedding-3-small","input":"hi"}`)
	rewrite := makeJSONModelRewriter(body, "text-embedding-3-small")

	out, _, err := rewrite("text-embedding-3-small")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("body changed for identical model: got %q, want verbatim %q", out, body)
	}
}

func TestMakeJSONModelRewriter_UnparseableForwardsAsIs(t *testing.T) {
	body := []byte(`{not json`)
	rewrite := makeJSONModelRewriter(body, "prov/m")

	out, contentType, err := rewrite("m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, body) {
		t.Errorf("unparseable body must be forwarded as-is, got %q", out)
	}
	if contentType != "application/json" {
		t.Errorf("contentType = %q, want application/json", contentType)
	}
}

// ---------------------------------------------------------------------------
// parseMultipartParts / rebuildMultipartBody
// ---------------------------------------------------------------------------

// buildTestMultipart assembles a multipart body with a model field, an extra
// field, and a file part. Returns the body and its boundary.
func buildTestMultipart(t *testing.T, model string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", model); err != nil {
		t.Fatalf("WriteField(model): %v", err)
	}
	if err := mw.WriteField("language", "en"); err != nil {
		t.Fatalf("WriteField(language): %v", err)
	}
	fw, err := mw.CreateFormFile("file", "audio.mp3")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02}); err != nil {
		t.Fatalf("file write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return buf.Bytes(), mw.Boundary()
}

func TestParseMultipartParts_ExtractsModelAndParts(t *testing.T) {
	body, boundary := buildTestMultipart(t, "prov/whisper-1")

	parts, model, err := parseMultipartParts(body, boundary)
	if err != nil {
		t.Fatalf("parseMultipartParts: %v", err)
	}
	if model != "prov/whisper-1" {
		t.Errorf("model = %q, want prov/whisper-1", model)
	}
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}
	if parts[2].fieldName != "file" || parts[2].fileName != "audio.mp3" {
		t.Errorf("file part = %+v, want fieldName=file fileName=audio.mp3", parts[2])
	}
	if !bytes.Equal(parts[2].data, []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02}) {
		t.Errorf("file bytes corrupted: %v", parts[2].data)
	}
}

func TestParseMultipartParts_NoModelField(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("language", "en")
	_ = mw.Close()

	_, model, err := parseMultipartParts(buf.Bytes(), mw.Boundary())
	if err != nil {
		t.Fatalf("parseMultipartParts: %v", err)
	}
	if model != "" {
		t.Errorf("model = %q, want empty", model)
	}
}

func TestParseMultipartParts_MalformedPartHeader(t *testing.T) {
	body := []byte("--xyz\r\nno-colon-header-line\r\n\r\nhi\r\n--xyz--\r\n")
	if _, _, err := parseMultipartParts(body, "xyz"); err == nil {
		t.Error("expected error for malformed part header")
	}
}

func TestParseMultipartParts_GarbageBodyYieldsNoParts(t *testing.T) {
	// A body without the boundary reads as clean EOF: zero parts, no model.
	// The handler's model-is-required guard rejects it downstream.
	parts, model, err := parseMultipartParts([]byte("definitely not multipart"), "xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 0 || model != "" {
		t.Errorf("got parts=%d model=%q, want 0 parts and empty model", len(parts), model)
	}
}

func TestRebuildMultipartBody_RewritesModelKeepsFile(t *testing.T) {
	body, boundary := buildTestMultipart(t, "prov/whisper-1")
	parts, _, err := parseMultipartParts(body, boundary)
	if err != nil {
		t.Fatalf("parseMultipartParts: %v", err)
	}

	rebuilt, contentType, err := rebuildMultipartBody(parts, "whisper-1")
	if err != nil {
		t.Fatalf("rebuildMultipartBody: %v", err)
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("contentType = %q (err %v), want multipart/form-data", contentType, err)
	}

	// Round-trip the rebuilt body and verify the rewrite.
	reparsed, model, err := parseMultipartParts(rebuilt, params["boundary"])
	if err != nil {
		t.Fatalf("re-parse rebuilt body: %v", err)
	}
	if model != "whisper-1" {
		t.Errorf("rebuilt model = %q, want whisper-1", model)
	}
	if len(reparsed) != 3 {
		t.Fatalf("len(reparsed) = %d, want 3", len(reparsed))
	}
	var filePart *multipartPart
	for i := range reparsed {
		if reparsed[i].fieldName == "file" {
			filePart = &reparsed[i]
		}
		if reparsed[i].fieldName == "language" && string(reparsed[i].data) != "en" {
			t.Errorf("language field = %q, want en", reparsed[i].data)
		}
	}
	if filePart == nil {
		t.Fatal("file part missing after rebuild")
	}
	if filePart.fileName != "audio.mp3" {
		t.Errorf("fileName = %q, want audio.mp3", filePart.fileName)
	}
	if !bytes.Equal(filePart.data, []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02}) {
		t.Errorf("file bytes corrupted after rebuild: %v", filePart.data)
	}
	if filePart.contentType != "application/octet-stream" {
		t.Errorf("file contentType = %q, want application/octet-stream (set by CreateFormFile)", filePart.contentType)
	}
}

func TestRebuildMultipartBody_EscapesQuotesInFilename(t *testing.T) {
	parts := []multipartPart{
		{fieldName: "model", data: []byte("prov/m")},
		{fieldName: "file", fileName: `we"ird\name.mp3`, contentType: "audio/mpeg", data: []byte("xx")},
	}
	rebuilt, contentType, err := rebuildMultipartBody(parts, "m")
	if err != nil {
		t.Fatalf("rebuildMultipartBody: %v", err)
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	reparsed, model, err := parseMultipartParts(rebuilt, params["boundary"])
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if model != "m" {
		t.Errorf("model = %q, want m", model)
	}
	if len(reparsed) != 2 || reparsed[1].fileName != `we"ird\name.mp3` {
		t.Errorf("filename did not survive quoting round-trip: %+v", reparsed)
	}
	if reparsed[1].contentType != "audio/mpeg" {
		t.Errorf("contentType = %q, want audio/mpeg", reparsed[1].contentType)
	}
}

// ---------------------------------------------------------------------------
// extractPassthroughUsage
// ---------------------------------------------------------------------------

func TestExtractPassthroughUsage(t *testing.T) {
	cases := []struct {
		name           string
		body           string
		wantPrompt     int
		wantCompletion int
	}{
		{
			name:       "embeddings shape (prompt/total)",
			body:       `{"object":"list","data":[],"usage":{"prompt_tokens":8,"total_tokens":8}}`,
			wantPrompt: 8,
		},
		{
			name:           "images/audio shape (input/output)",
			body:           `{"created":1,"data":[],"usage":{"input_tokens":50,"output_tokens":100,"total_tokens":150}}`,
			wantPrompt:     50,
			wantCompletion: 100,
		},
		{
			name:           "chat-style completion tokens preferred over output",
			body:           `{"usage":{"prompt_tokens":3,"completion_tokens":7,"output_tokens":99}}`,
			wantPrompt:     3,
			wantCompletion: 7,
		},
		{name: "no usage", body: `{"object":"list","data":[]}`},
		{name: "null usage", body: `{"usage":null}`},
		{name: "invalid JSON", body: `{nope`},
		{name: "empty body", body: ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, completion := extractPassthroughUsage([]byte(tc.body))
			if prompt != tc.wantPrompt || completion != tc.wantCompletion {
				t.Errorf("extractPassthroughUsage() = (%d, %d), want (%d, %d)", prompt, completion, tc.wantPrompt, tc.wantCompletion)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// flushWriter
// ---------------------------------------------------------------------------

func TestFlushWriter_WritesAndFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	fw := newFlushWriter(rec)

	n, err := fw.Write([]byte("chunk"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}
	if rec.Body.String() != "chunk" {
		t.Errorf("body = %q, want chunk", rec.Body.String())
	}
	if !rec.Flushed {
		t.Error("expected writer to flush after write")
	}
}

func TestFlushWriter_NonFlusherWriter(t *testing.T) {
	// A ResponseWriter without Flusher must not panic.
	var sink bytes.Buffer
	fw := flushWriter{w: &sink}
	if _, err := io.WriteString(fw, "data"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if sink.String() != "data" {
		t.Errorf("body = %q, want data", sink.String())
	}
}
