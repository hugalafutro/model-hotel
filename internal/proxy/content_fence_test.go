package proxy

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const canary = "SUPERSECRET-PROMPT-XYZQ the crown jewels passphrase is hunter2-canary"

func chatBody(content string) []byte {
	return []byte(`{"model":"p/m","messages":[{"role":"system","content":"You are terse."},{"role":"user","content":` + jsonString(content) + `}],"stream":false}`)
}

func jsonString(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The Strix PoC shape: a 429 body quoting the prompt back. The echo goes,
// the provider's own words stay, on both stored surfaces.
func TestContentFence_MasksAnEchoedPrompt(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	rawBody := `{"error": {"message": "rate limit exceeded while processing: ` + canary + `", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}`
	detail := `rate limit exceeded while processing: ` + canary + `", "type": "rate_limit_error", "code": "rat…`
	got := f.mask([]string{rawBody, detail})
	for i, s := range got {
		if strings.Contains(s, "SUPERSECRET") || strings.Contains(s, "hunter2") {
			t.Fatalf("text %d still carries the prompt: %q", i, s)
		}
		if !strings.Contains(s, "rate limit exceeded while processing: [content]") {
			t.Fatalf("text %d lost the provider's own words or the marker: %q", i, s)
		}
	}
	if !strings.Contains(got[0], `"type": "rate_limit_error", "code": "rate_limit_exceeded"`) {
		t.Fatalf("the structured members must survive: %q", got[0])
	}
}

// A partial echo (the provider truncates what it quotes) is still an echo.
func TestContentFence_MasksAPartialEcho(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	got := f.maskOne("invalid input: " + canary[:30] + "... (truncated)")
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("partial echo survived: %q", got)
	}
	if got != "invalid input: [content]... (truncated)" {
		t.Fatalf("got %q", got)
	}
}

// error_message stores the provider's JSON as sent, so an echo inside a
// string member is JSON-escaped where the request had quotes and newlines.
func TestContentFence_MasksTheEscapedFormToo(t *testing.T) {
	t.Parallel()
	prompt := "line one says \"quoted words here\"\nline two continues the secret text"
	f := newContentFence(chatBody(prompt))
	raw := `{"error":{"message":"bad request: line one says \"quoted words here\"\nline two continues the secret text is not allowed"}}`
	got := f.maskOne(raw)
	if strings.Contains(got, "quoted words") || strings.Contains(got, "secret text") {
		t.Fatalf("escaped echo survived: %q", got)
	}
	if !strings.HasPrefix(got, `{"error":{"message":"bad request: [content] is not allowed"}}`) {
		t.Fatalf("got %q", got)
	}
}

// The provider's own sentence never appears in the request, so it is left
// alone even when it shares ordinary words with the prompt.
func TestContentFence_LeavesTheProvidersOwnWords(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody("please explain why my rate limit keeps resetting at midnight"))
	msg := "Weekly/Monthly Limit Exhausted. Your limit will reset at 2026-09-03 18:01:05 (code 1310)"
	if got := f.maskOne(msg); got != msg {
		t.Fatalf("provider text changed: %q", got)
	}
}

// An echo shorter than the window is a documented gap, not a mask.
func TestContentFence_WindowIsTheFloor(t *testing.T) {
	t.Parallel()
	short := strings.Repeat("s", contentEchoWindow-1)
	exact := strings.Repeat("e", contentEchoWindow)
	// Bracketed so the shared run is exactly the letters: a boundary space
	// present on both sides would be part of the run, and rightly so.
	f := newContentFence(chatBody("[" + short + "] and [" + exact + "]"))
	if got := f.maskOne("saw " + short + " here"); got != "saw "+short+" here" {
		t.Fatalf("a run under the window was masked: %q", got)
	}
	if got := f.maskOne("saw " + exact + " here"); got != "saw [content] here" {
		t.Fatalf("a run of exactly the window was not masked: %q", got)
	}
}

// Encoded payloads are not indexed: a data: URL or a long whitespace-free
// run is an upload, not text, and would cost megabytes to index for no
// disclosure worth the name. The text beside them is still fenced.
func TestContentFence_SkipsEncodedPayloads(t *testing.T) {
	t.Parallel()
	blob := strings.Repeat("QUJDREVGR0hJSktMTU5PUA", 400) // 8800 chars, no whitespace
	body := []byte(`{"model":"p/m","messages":[{"role":"user","content":[{"type":"text","text":"` + canary + `"},{"type":"image_url","image_url":{"url":"data:image/png;base64,` + blob + `"}},{"type":"input_audio","input_audio":{"data":"` + blob + `","format":"wav"}}]}]}`)
	f := newContentFence(body)
	if n := len(f.strings()); n != 1 {
		t.Fatalf("indexed %d strings, want only the text part", n)
	}
	if got := f.maskOne("echo: " + blob[:64]); got != "echo: "+blob[:64] {
		t.Fatalf("a blob fragment was masked: %q", got)
	}
	if got := f.maskOne("echo: " + canary); got != "echo: [content]" {
		t.Fatalf("the text part was not fenced: %q", got)
	}
}

// Multipart requests fence their text fields; the upload is not text.
func TestContentFence_MultipartTextFields(t *testing.T) {
	t.Parallel()
	parts := []multipartPart{
		{fieldName: "model", data: []byte("p/m")},
		{fieldName: "prompt", data: []byte("a watercolour of " + canary)},
		{fieldName: "file", fileName: "a.wav", data: []byte(strings.Repeat("x", 100))},
	}
	f := newContentFence(nil, multipartTextFields(parts)...)
	// The prompt had a space before the canary too, so the run the two share
	// starts at that space.
	if got := f.maskOne("cannot render: " + canary); got != "cannot render:[content]" {
		t.Fatalf("got %q", got)
	}
	if got := f.maskOne("file " + strings.Repeat("x", 40)); !strings.Contains(got, strings.Repeat("x", 40)) {
		t.Fatalf("the upload's bytes were indexed: %q", got)
	}
}

// Nothing to fence, nothing changes: a nil fence, an unparsable body, an
// empty text, a text shorter than the window.
func TestContentFence_Passthroughs(t *testing.T) {
	t.Parallel()
	var nilFence *contentFence
	if got := nilFence.maskOne(canary); got != canary {
		t.Fatalf("nil fence changed text: %q", got)
	}
	if newContentFence(nil) != nil {
		t.Fatal("an empty request built a fence")
	}
	f := newContentFence([]byte("not json " + canary))
	if got := f.maskOne("echo " + canary); got != "echo "+canary {
		t.Fatalf("an unparsable body indexed anything: %q", got)
	}
	f = newContentFence(chatBody(canary))
	if got := f.mask([]string{"", "short"}); got[0] != "" || got[1] != "short" {
		t.Fatalf("got %v", got)
	}
	// Idempotent: a second pass over fenced text finds nothing new.
	once := f.maskOne("said: " + canary)
	if twice := f.maskOne(once); twice != once {
		t.Fatalf("second pass changed %q to %q", once, twice)
	}
}

// The index budget bounds the walk: content past it is not fenced, which the
// comment documents, and the walk does not blow up on a body far past it.
func TestContentFence_IndexBudget(t *testing.T) {
	t.Parallel()
	head := "HEAD " + canary
	filler := strings.Repeat("filler words to spend the budget ", contentIndexCap/32)
	tail := "TAIL past the budget " + canary
	body := []byte(`{"messages":[{"role":"user","content":` + jsonString(head) + `},{"role":"user","content":` + jsonString(filler) + `},{"role":"user","content":` + jsonString(tail) + `}]}`)
	f := newContentFence(body)
	total := 0
	for _, s := range f.strings() {
		total += len(s)
	}
	if total > 2*contentIndexCap {
		t.Fatalf("indexed %d runes, want at most the cap in each form", total)
	}
	if got := f.maskOne("echo " + canary); got != "echo[content]" {
		t.Fatalf("content inside the budget was not fenced: %q", got)
	}
}

// fenceContent runs over the row's error message and every attempt detail.
func TestContentFence_FencesTheWholeRow(t *testing.T) {
	t.Parallel()
	l := &requestLogData{content: newContentFence(chatBody(canary))}
	l.errorMessage = "upstream HTTP 429: quoted " + canary
	l.attempts = []attemptRecord{{Detail: "HTTP 429 (saturated)"}, {Detail: "quoted " + canary + " again"}}
	l.fenceContent()
	if l.errorMessage != "upstream HTTP 429: quoted [content]" {
		t.Fatalf("error message = %q", l.errorMessage)
	}
	if l.attempts[0].Detail != "HTTP 429 (saturated)" || l.attempts[1].Detail != "quoted [content] again" {
		t.Fatalf("attempts = %+v", l.attempts)
	}
	var none *requestLogData
	none.fenceContent()
	(&requestLogData{}).fenceContent()
}

func TestContentFence_MaskIsRuneSafe(t *testing.T) {
	t.Parallel()
	prompt := "日本語のテキストがここに十分に長く続いています"
	f := newContentFence(chatBody(prompt))
	got := f.maskOne("エラー: " + prompt + " は無効です")
	if !utf8.ValidString(got) || strings.Contains(got, "十分") {
		t.Fatalf("got %q", got)
	}
	if got != "エラー: [content] は無効です" {
		t.Fatalf("got %q", got)
	}
}

// The streaming path's app-log attribute is fenced too: a provider error
// frame quoting the prompt must not reach app_logs either.
func TestContentFence_StreamErrorLogAttr(t *testing.T) {
	t.Parallel()
	st := &streamState{content: newContentFence(chatBody(canary))}
	got := st.errLogAttr("upstream error frame: cannot process " + canary)
	if strings.Contains(got, "SUPERSECRET") || !strings.Contains(got, "[content]") {
		t.Fatalf("got %q", got)
	}
	none := &streamState{}
	if got := none.errLogAttr("plain"); got != "plain" {
		t.Fatalf("no fence changed text: %q", got)
	}
}
