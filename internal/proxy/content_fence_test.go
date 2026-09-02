package proxy

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hugalafutro/model-hotel/internal/provider"
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
		{fieldName: "model", data: []byte("prov/very-long-model-name-here")},
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
	if got := f.maskOne("model prov/very-long-model-name-here not found"); got != "model prov/very-long-model-name-here not found" {
		t.Fatalf("the model field was indexed: %q", got)
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
	if total > len(contentForms)*contentIndexCap {
		t.Fatalf("indexed %d runes, want at most the cap in each of %d forms", total, len(contentForms))
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

// The trail's detail is whitespace-collapsed by attemptDetail before the
// fence sees it, so a prompt with indented code or double spaces has to
// match in its collapsed form too.
func TestContentFence_CollapsedTrailDetail(t *testing.T) {
	t.Parallel()
	prompt := "CANARY  merger  target  is  Aurora  Bio  Ltd\n    def leak():\n        print(SECRET_TOKEN_VALUE)\n"
	f := newContentFence(chatBody(prompt))
	raw := `{"error": {"message": "rate limit exceeded while processing: ` + strings.ReplaceAll(prompt, "\n", `\n`) + `"}}`
	detail := attemptDetail(credentialMasker{}, raw)
	got := f.maskOne(detail)
	for _, leak := range []string{"CANARY", "Aurora", "leak()", "SECRET_TOKEN"} {
		if strings.Contains(got, leak) {
			t.Fatalf("collapsed detail still carries %q: %q", leak, got)
		}
	}
	if !strings.HasPrefix(got, `{"error": {"message": "rate limit exceeded while processing:`) {
		t.Fatalf("provider words lost: %q", got)
	}
	// And the same prompt echoed with its spacing intact, as error_message
	// stores it.
	if got := f.maskOne(raw); strings.Contains(got, "CANARY") || strings.Contains(got, "SECRET_TOKEN") {
		t.Fatalf("raw echo survived: %q", got)
	}
}

// Text with no spaces is still text: a page of Chinese, a minified JSON
// document or a CSV must be indexed, and the blob rule must count runes,
// not bytes, when it probes.
func TestContentFence_DenseTextIsContent(t *testing.T) {
	t.Parallel()
	zh := strings.Repeat("我们公司的机密并购计划是收购北京晨光科技，出价四亿五千万。", 120) // ~3,600 runes, ~10,800 bytes
	csv := strings.Repeat("id,name,ssn;", 500)
	minified := `{"k":` + strings.Repeat(`{"a":1,"b":"x"},`, 400) + `1}`
	f := newContentFence(chatBody(zh + "\n" + csv + "\n" + minified))
	if n := len(f.strings()); n == 0 {
		t.Fatal("dense text was not indexed")
	}
	for name, echo := range map[string]string{"zh": zh[:90], "csv": csv[:40], "json": minified[:40]} {
		if got := f.maskOne("请求过长，无法处理：" + echo + " …"); strings.Contains(got, echo[:20]) {
			t.Fatalf("%s echo survived: %q", name, got)
		}
	}
	// The probe is in runes: a CJK string past 4096 bytes but under the
	// probe in runes is plainly text and must not be judged as a blob, and
	// neither is one past the probe in runes: the rule is the alphabet, not
	// the absence of whitespace.
	if isEncodedPayload(strings.Repeat("日本語の文章", 300)) {
		t.Fatal("1800 runes of CJK judged a blob")
	}
	long := strings.Repeat("我们公司的机密并购计划是收购北京晨光科技。", 300) // 6,000 runes, no whitespace
	if isEncodedPayload(long) {
		t.Fatal("6000 runes of CJK judged a blob")
	}
	if got := newContentFence(chatBody(long)).maskOne("无法处理：" + long[:120]); strings.Contains(got, "晨光科技") {
		t.Fatalf("a long CJK prompt was not fenced: %q", got)
	}
	if !isEncodedPayload(strings.Repeat("QUJDREVGR0hJSktMTU5PUA", 400)) {
		t.Fatal("base64 not judged a blob")
	}
}

// Over the index budget the walk is deterministic: the content-bearing
// members are indexed first, whatever order the client wrote the JSON in, so
// the same request fences the same way every time.
func TestContentFence_DeterministicOverTheBudget(t *testing.T) {
	t.Parallel()
	// A filler whose four forms all differ (double spaces, newlines, quotes)
	// and outweighs every per-form budget, so that only the walk order
	// decides what is indexed. "context" sorts before "messages", so only the
	// content-first ranking keeps the prompt inside the budget; a rerank's
	// "documents" would starve its "query" the same way.
	filler := strings.Repeat("tool  description \"filler\" words that spend the whole budget\n", 2*contentIndexCap/60+1)
	body := []byte(`{"context":` + jsonString(filler) + `,"tools":[{"type":"function","function":{"name":"t","description":` + jsonString(filler) + `}}],"model":"p/m","messages":[{"role":"user","content":` + jsonString(canary) + `}]}`)
	for i := 0; i < 5; i++ {
		f := newContentFence(body)
		if n := len(f.strings()); n < len(contentForms) {
			t.Fatalf("run %d: %d forms indexed, want every budget exhausted for the test to bite", i, n)
		}
		if got := f.maskOne("echo " + canary); strings.Contains(got, "SUPERSECRET") {
			t.Fatalf("run %d: messages lost to the budget: %q", i, got)
		}
	}
	rerank := []byte(`{"model":"p/m","documents":[` + jsonString(filler) + `],"query":` + jsonString(canary) + `}`)
	if got := newContentFence(rerank).maskOne("cannot rerank: " + canary); strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("a rerank query was starved by its documents: %q", got)
	}
}

// Each form has its own budget: a single string past the cap is still
// indexed in the collapsed form the trail's detail needs, not only as
// written.
func TestContentFence_PerFormBudget(t *testing.T) {
	t.Parallel()
	huge := "SECRET  double  spaced  dossier  header  line  here\n" + strings.Repeat("filler text to pass the cap ", contentIndexCap/28+1)
	f := newContentFence(chatBody(huge))
	if n := len(f.strings()); n < 3 {
		t.Fatalf("indexed %d forms of a string past the cap, want the raw, collapsed and escaped ones at least", n)
	}
	raw := `{"error":{"message":"cannot process: SECRET  double  spaced  dossier  header  line  here"}}`
	if got := f.maskOne(attemptDetail(credentialMasker{}, raw)); strings.Contains(got, "dossier") {
		t.Fatalf("the collapsed form was starved: %q", got)
	}
}

// The client's own identifiers are content: an e-mail in the OpenAI user
// field or a message name quoted back by a provider is fenced.
func TestContentFence_IdentifiersAreContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"p/m","user":"alice.mcgregor@acquisitions-corp.example","messages":[{"role":"user","name":"participant-long-name-value","content":"hello there friend"}]}`)
	f := newContentFence(body)
	if got := f.maskOne("user alice.mcgregor@acquisitions-corp.example is not permitted"); strings.Contains(got, "mcgregor") {
		t.Fatalf("user field survived: %q", got)
	}
	if got := f.maskOne("name participant-long-name-value rejected"); strings.Contains(got, "long-name") {
		t.Fatalf("message name survived: %q", got)
	}
}

// A hedged probe runs against a throwaway log entry; it must carry the
// fence, or the probe's failure line logs the provider's frame unfenced.
func TestContentFence_HedgeProbeLogCarriesTheFence(t *testing.T) {
	t.Parallel()
	entry := &requestLogData{modelID: "hotel/g", endpointType: "chat", content: newContentFence(chatBody(canary))}
	snap := hedgeProbeLog(entry, modelCandidate{provider: &provider.Provider{Name: "p"}})
	if snap.content != entry.content || snap.modelID != "hotel/g" || snap.providerName != "p" || snap.endpointType != "chat" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if got := snap.content.maskOne("cannot process: " + canary); strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("snapshot fence inert: %q", got)
	}
}

// Routing fields are not content: the model name appears verbatim in the
// gateway's own messages, and a role or a participant name is metadata.
func TestContentFence_RoutingFieldsAreNotContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"hotel/claude-sonnet-4-5-long","messages":[{"role":"user-with-a-long-role","name":"participant-name-here","content":` + jsonString(canary) + `}],"tool_choice":"auto-with-long-value","response_format":{"type":"json_schema","json_schema":{"schema":{"description":"schema text long enough to index"}}}}`)
	f := newContentFence(body)
	for _, keep := range []string{
		"no available provider for hotel/claude-sonnet-4-5-long; earliest retry in 30s",
		"invalid model format: hotel/claude-sonnet-4-5-long",
		"role user-with-a-long-role not accepted",
		"tool_choice auto-with-long-value rejected",
	} {
		if got := f.maskOne(keep); got != keep {
			t.Fatalf("routing text was fenced: %q -> %q", keep, got)
		}
	}
	if got := f.maskOne("echo " + canary); strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("content beside the routing fields was not fenced: %q", got)
	}
	// An object under a routing key is still walked: only a bare string is
	// skipped.
	if got := f.maskOne("schema text long enough to index"); got == "schema text long enough to index" {
		t.Fatal("a description under response_format was not indexed")
	}
}
