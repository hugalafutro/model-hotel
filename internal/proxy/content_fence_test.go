package proxy

import (
	"strings"
	"testing"

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

// The Strix PoC shape: a 429 body quoting the prompt back. The whole fragment
// goes, on both stored surfaces, rather than the echo alone.
func TestContentFence_WithholdsAnEchoedPrompt(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	rawBody := `{"error": {"message": "rate limit exceeded while processing: ` + canary + `", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}`
	detail := `rate limit exceeded while processing: ` + canary + `", "type": "rate_limit_error", "code": "rat…`
	for i, s := range []string{rawBody, detail} {
		if got := f.fenceUpstream(s); got != contentWithheld {
			t.Fatalf("text %d = %q, want the whole fragment withheld", i, got)
		}
	}
}

// A partial echo (the provider truncates what it quotes) is still an echo.
func TestContentFence_WithholdsAPartialEcho(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	if got := f.fenceUpstream("invalid input: " + canary[:30] + "... (truncated)"); got != contentWithheld {
		t.Fatalf("partial echo survived: %q", got)
	}
}

// error_message stores the provider's JSON as sent, so an echo inside a
// string member is JSON-escaped where the request had quotes and newlines.
func TestContentFence_CatchesTheEscapedFormToo(t *testing.T) {
	t.Parallel()
	prompt := "line one says \"quoted words here\"\nline two continues the secret text"
	f := newContentFence(chatBody(prompt))
	raw := `{"error":{"message":"bad request: line one says \"quoted words here\"\nline two continues the secret text is not allowed"}}`
	if got := f.fenceUpstream(raw); got != contentWithheld {
		t.Fatalf("escaped echo survived: %q", got)
	}
}

// The provider's own sentence never appears in the request, so it is left
// alone even when it shares ordinary words with the prompt.
func TestContentFence_LeavesTheProvidersOwnWords(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody("please explain why my rate limit keeps resetting at midnight"))
	msg := "Weekly/Monthly Limit Exhausted. Your limit will reset at 2026-09-03 18:01:05 (code 1310)"
	if got := f.fenceUpstream(msg); got != msg {
		t.Fatalf("provider text changed: %q", got)
	}
}

// An echo shorter than the window is a documented gap, not a withholding.
func TestContentFence_WindowIsTheFloor(t *testing.T) {
	t.Parallel()
	short := strings.Repeat("s", contentEchoWindow-1)
	exact := strings.Repeat("e", contentEchoWindow)
	// Bracketed so the shared run is exactly the letters: a boundary space
	// present on both sides would be part of the run, and rightly so.
	f := newContentFence(chatBody("[" + short + "] and [" + exact + "]"))
	if got := f.fenceUpstream("saw " + short + " here"); got != "saw "+short+" here" {
		t.Fatalf("a run under the window was withheld: %q", got)
	}
	if got := f.fenceUpstream("saw " + exact + " here"); got != contentWithheld {
		t.Fatalf("a run of exactly the window survived: %q", got)
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
	if got := f.fenceUpstream("echo: " + blob[:64]); got != "echo: "+blob[:64] {
		t.Fatalf("a blob fragment was withheld: %q", got)
	}
	if got := f.fenceUpstream("echo: " + canary); got != contentWithheld {
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
	if got := f.fenceUpstream("cannot render: " + canary); got != contentWithheld {
		t.Fatalf("got %q", got)
	}
	if got := f.fenceUpstream("file " + strings.Repeat("x", 40)); !strings.Contains(got, strings.Repeat("x", 40)) {
		t.Fatalf("the upload's bytes were indexed: %q", got)
	}
	if got := f.fenceUpstream("model prov/very-long-model-name-here not found"); got != "model prov/very-long-model-name-here not found" {
		t.Fatalf("the model field was indexed: %q", got)
	}
}

// Nothing to fence, nothing changes: a nil fence, an unparsable body, an
// empty text, a text shorter than the window.
func TestContentFence_Passthroughs(t *testing.T) {
	t.Parallel()
	var nilFence *contentFence
	if got := nilFence.fenceUpstream(canary); got != canary {
		t.Fatalf("nil fence changed text: %q", got)
	}
	if newContentFence(nil) != nil {
		t.Fatal("an empty request built a fence")
	}
	f := newContentFence([]byte("not json " + canary))
	if got := f.fenceUpstream("echo " + canary); got != "echo "+canary {
		t.Fatalf("an unparsable body indexed anything: %q", got)
	}
	f = newContentFence(chatBody(canary))
	if got := f.fenceUpstream(""); got != "" {
		t.Fatalf("empty text = %q", got)
	}
	if got := f.fenceUpstream("short"); got != "short" {
		t.Fatalf("short text = %q", got)
	}
	// Idempotent: the replacement itself is not request content, so a second
	// pass leaves it alone.
	if got := f.fenceUpstream(contentWithheld); got != contentWithheld {
		t.Fatalf("second pass changed the replacement to %q", got)
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
	if got := f.fenceUpstream("echo " + canary); got != contentWithheld {
		t.Fatalf("content inside the budget was not fenced: %q", got)
	}
}

// Gateway-authored text bypasses the fence. A prompt asking about a gateway
// error message shares a long run with the gateway's own account of the
// failure; fencing that sentence would blank the diagnosis the operator needs,
// and it discloses nothing, since the gateway wrote every word of it. The same
// words arriving from a provider are still withheld.
func TestContentFence_GatewayProseIsNotFenced(t *testing.T) {
	t.Parallel()
	prompt := "why do I keep seeing client disconnected during attempt 1 to provider \"Ollama\" in my logs"
	st := &requestState{logData: &requestLogData{content: newContentFence(chatBody(prompt))}}
	st.setReqErr(reqError{Kind: KindClientDisconnect, Attempt: 0, Provider: "Ollama"})
	want := `client disconnected during attempt 1 to provider "Ollama"`
	if got := st.lastErr; got != want {
		t.Fatalf("gateway message = %q, want %q", got, want)
	}
	if got := st.lastReqErr.terminalLogMessage(true, 3); got != want {
		t.Fatalf("terminal message = %q, want %q", got, want)
	}
	// The provider saying the same thing is upstream text, and goes.
	if got := st.logData.content.fenceUpstream(want); got != contentWithheld {
		t.Fatalf("upstream copy of the same words survived: %q", got)
	}
}

// The one fragment of provider text a reqError carries is fenced as it is
// recorded, so the exhaustion path's wrapper survives with nothing after it.
func TestContentFence_ReqErrUnderlyingIsFenced(t *testing.T) {
	t.Parallel()
	st := &requestState{logData: &requestLogData{content: newContentFence(chatBody(canary))}}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 0, Provider: "p", Underlying: "cannot process: " + canary})
	if strings.Contains(st.lastErr, "SUPERSECRET") {
		t.Fatalf("the echo reached the row: %q", st.lastErr)
	}
	msg := st.lastReqErr.terminalLogMessage(true, 3)
	if !strings.HasPrefix(msg, "all 3 providers failed; last error: ") {
		t.Fatalf("the gateway's wrapper was lost: %q", msg)
	}
	if !strings.HasSuffix(msg, contentWithheld) {
		t.Fatalf("want the fixed replacement at the tail: %q", msg)
	}
	// The client is told the same thing either way: terminalClientMessage
	// names the model and the class of failure and never renders Underlying,
	// which is what lets the fence rewrite it in place.
	if got := st.lastReqErr.terminalClientMessage("hotel/g", true); got != "all providers failed for model hotel/g" {
		t.Fatalf("the client message changed with the fence: %q", got)
	}
	// A nil log entry (a bare attempt path) has no fence: it must not panic,
	// and it must leave the provider's words alone.
	bare := &requestState{}
	bare.setReqErr(reqError{Kind: KindInternal, Underlying: "cannot process: " + canary})
	if !strings.Contains(bare.lastReqErr.Underlying, canary) {
		t.Fatalf("a fenceless state dropped the error: %q", bare.lastReqErr.Underlying)
	}
}

// The trail drops a detail it cannot show rather than storing a stub, and
// keeps one the fence had no quarrel with.
func TestContentFence_TrailDetailIsDroppedWhole(t *testing.T) {
	t.Parallel()
	f := newContentFence(chatBody(canary))
	if got := attemptDetail(credentialMasker{}, f, "cannot process: "+canary); got != "" {
		t.Fatalf("detail = %q, want it dropped", got)
	}
	// Already withheld upstream: the trail says nothing rather than repeating
	// the replacement the error message carries.
	if got := attemptDetail(credentialMasker{}, f, contentWithheld); got != "" {
		t.Fatalf("detail = %q, want it dropped", got)
	}
	clean := "Weekly/Monthly Limit Exhausted, resets 2026-09-03"
	if got := attemptDetail(credentialMasker{}, f, clean); got != clean {
		t.Fatalf("detail = %q, want the provider's own words", got)
	}
}

// The streaming path's app-log attribute is fenced too: a provider error
// frame quoting the prompt must not reach app_logs either.
func TestContentFence_StreamErrorLogAttr(t *testing.T) {
	t.Parallel()
	st := &streamState{content: newContentFence(chatBody(canary))}
	if got := st.errLogAttr("upstream error frame: cannot process " + canary); got != contentWithheld {
		t.Fatalf("got %q", got)
	}
	none := &streamState{}
	if got := none.errLogAttr("plain"); got != "plain" {
		t.Fatalf("no fence changed text: %q", got)
	}
}

// The trail's detail is whitespace-collapsed by attemptDetail, so a prompt
// with indented code or double spaces has to match in its collapsed form too.
func TestContentFence_CollapsedTrailDetail(t *testing.T) {
	t.Parallel()
	prompt := "CANARY  merger  target  is  Aurora  Bio  Ltd\n    def leak():\n        print(SECRET_TOKEN_VALUE)\n"
	f := newContentFence(chatBody(prompt))
	raw := `{"error": {"message": "rate limit exceeded while processing: ` + strings.ReplaceAll(prompt, "\n", `\n`) + `"}}`
	if got := attemptDetail(credentialMasker{}, f, raw); got != "" {
		t.Fatalf("collapsed detail survived: %q", got)
	}
	// And the same prompt echoed with its spacing intact, as error_message
	// stores it.
	if got := f.fenceUpstream(raw); got != contentWithheld {
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
		if got := f.fenceUpstream("请求过长，无法处理：" + echo + " …"); got != contentWithheld {
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
	if got := newContentFence(chatBody(long)).fenceUpstream("无法处理：" + long[:120]); got != contentWithheld {
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
		// The forms are counted before the first fenced fragment: that call
		// builds the window set and releases the strings.
		if n := len(f.strings()); n < len(contentForms) {
			t.Fatalf("run %d: %d forms indexed, want every budget exhausted for the test to bite", i, n)
		}
		if got := f.fenceUpstream("echo " + canary); got != contentWithheld {
			t.Fatalf("run %d: messages lost to the budget: %q", i, got)
		}
	}
	rerank := []byte(`{"model":"p/m","documents":[` + jsonString(filler) + `],"query":` + jsonString(canary) + `}`)
	if got := newContentFence(rerank).fenceUpstream("cannot rerank: " + canary); got != contentWithheld {
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
	if got := attemptDetail(credentialMasker{}, f, raw); got != "" {
		t.Fatalf("the collapsed form was starved: %q", got)
	}
}

// The client's own identifiers are content: an e-mail in the OpenAI user
// field or a message name quoted back by a provider is fenced.
func TestContentFence_IdentifiersAreContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"p/m","user":"alice.mcgregor@acquisitions-corp.example","messages":[{"role":"user","name":"participant-long-name-value","content":"hello there friend"}]}`)
	f := newContentFence(body)
	if got := f.fenceUpstream("user alice.mcgregor@acquisitions-corp.example is not permitted"); got != contentWithheld {
		t.Fatalf("user field survived: %q", got)
	}
	if got := f.fenceUpstream("name participant-long-name-value rejected"); got != contentWithheld {
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
	if got := snap.content.fenceUpstream("cannot process: " + canary); got != contentWithheld {
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
		if got := f.fenceUpstream(keep); got != keep {
			t.Fatalf("routing text was fenced: %q -> %q", keep, got)
		}
	}
	if got := f.fenceUpstream("echo " + canary); got != contentWithheld {
		t.Fatalf("content beside the routing fields was not fenced: %q", got)
	}
	// An object under a routing key is still walked: only a bare string is
	// skipped.
	if got := f.fenceUpstream("schema text long enough to index"); got == "schema text long enough to index" {
		t.Fatal("a description under response_format was not indexed")
	}
}
