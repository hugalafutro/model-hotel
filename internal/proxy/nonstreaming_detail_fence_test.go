package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The decode-error detail is stored (request_logs.error_message, the attempt
// trail), so the upstream Content-Type it quotes is bounded and fenced: a
// provider that echoes the prompt into the header does not get it into a row.
func TestNonStreamingFailureDetail_FencesTheContentTypeHeader(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain; prompt=" + canary}}}
	fence := newContentFence(chatBody(canary))
	_, detail, kind, _ := nonStreamingFailureDetail(context.Background(), resp, []byte("<html>"), nil, errors.New("invalid character '<'"), "m", fence, credentialMasker{})
	if kind != KindProviderError {
		t.Fatalf("kind = %q, want %q", kind, KindProviderError)
	}
	if strings.Contains(detail, canary) || !strings.Contains(detail, contentWithheld) {
		t.Fatalf("detail carries the prompt or lacks the marker: %s", detail)
	}
	plain := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}}
	_, detail, _, _ = nonStreamingFailureDetail(context.Background(), plain, []byte("<html>"), nil, errors.New("invalid character '<'"), "m", fence, credentialMasker{})
	if !strings.Contains(detail, `content_type="text/html"`) {
		t.Fatalf("an ordinary content type must be kept verbatim: %s", detail)
	}
}

// The read-error detail lands in the same stored places as the decode-error one
// beside it, and an upstream that dies mid-body can put its own text in that
// error, so it takes the same pass. This is the branch the Content-Type test
// above did not reach.
func TestNonStreamingFailureDetail_FencesTheReadError(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	fence := newContentFence(chatBody(canary))

	_, detail, kind, _ := nonStreamingFailureDetail(context.Background(), resp, []byte("{"), errors.New("read tcp: "+canary), nil, "m", fence, credentialMasker{})
	if kind != KindProviderError {
		t.Fatalf("kind = %q, want %q", kind, KindProviderError)
	}
	if strings.Contains(detail, canary) || !strings.Contains(detail, contentWithheld) {
		t.Fatalf("detail carries the prompt or lacks the marker: %s", detail)
	}

	_, detail, _, _ = nonStreamingFailureDetail(context.Background(), resp, []byte("{"), errors.New("connection reset by peer"), nil, "m", fence, credentialMasker{})
	if !strings.Contains(detail, "connection reset by peer") {
		t.Fatalf("an ordinary read error must be kept verbatim: %s", detail)
	}
}

// fence() is nil-safe and masks() is its counterpart: a path that builds a
// fenced line before the log row exists must keep the regex layer rather than
// dereference nil.
func TestRequestLogData_MasksIsNilSafe(t *testing.T) {
	t.Parallel()
	var nilData *requestLogData
	if got := fencedFrameMessage(nilData.fence(), nilData.masks(), "plain upstream text"); got != "plain upstream text" {
		t.Fatalf("a nil log entry should still render the text, got %q", got)
	}
	held := &requestLogData{masker: newCredentialMasker("sk-" + strings.Repeat("a", 40))}
	if got := fencedFrameMessage(held.fence(), held.masks(), "key sk-"+strings.Repeat("a", 40)+" leaked"); strings.Contains(got, strings.Repeat("a", 40)) {
		t.Fatalf("the credential survived the masker: %q", got)
	}
}

// opaqueKey is a credential no key-shape rule recognises: no digit, so the
// ambiguous-shape veto leaves it alone, and only the exact pass over the
// attempt's own key can redact it. A test that used a key-shaped value would
// pass on the regex layer alone and prove nothing about the masker.
var opaqueKey = "sk-" + strings.Repeat("a", 40)

// The deadline branch stores a detail of its own, and it is the one a
// context.Background() test never reaches: abortKind only reports this
// gateway's per-attempt deadline when the context has actually expired.
func TestNonStreamingFailureDetail_FencesTheReadErrorOnTheDeadlineBranch(t *testing.T) {
	t.Parallel()
	// The origin stamp is what separates this gateway's own per-attempt
	// deadline from a client leaving: without it resolveCancelOrigin reads a
	// DeadlineExceeded as a disconnect and the abandoned branch answers.
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), ctxkeys.CancelOriginKey, "failover_timeout"), time.Now().Add(-time.Second))
	defer cancel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	fence := newContentFence(chatBody(canary))

	readErr := fmt.Errorf("read: %w: %s", context.DeadlineExceeded, canary)
	_, detail, kind, _ := nonStreamingFailureDetail(ctx, resp, []byte("{"), readErr, nil, "m", fence, credentialMasker{})
	if kind != KindProviderTimeout {
		t.Fatalf("kind = %q, want %q: the deadline branch was not reached", kind, KindProviderTimeout)
	}
	if strings.Contains(detail, canary) || !strings.Contains(detail, contentWithheld) {
		t.Fatalf("detail carries the prompt or lacks the marker: %s", detail)
	}
}

// The read-error detail is masked with the attempt's own credential, not only
// the key-shape layer: a provider quoting a key back into a read error must
// not put it in the stored detail.
func TestNonStreamingFailureDetail_MasksTheAttemptsCredential(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	_, detail, _, _ := nonStreamingFailureDetail(context.Background(), resp, []byte("{"),
		errors.New("read tcp: auth "+opaqueKey+" rejected"), nil, "m", nil, newCredentialMasker(opaqueKey))
	if strings.Contains(detail, strings.Repeat("a", 40)) {
		t.Fatalf("the attempt's credential survived into the stored detail: %s", detail)
	}
}

// setReqErr is where Underlying is prepared for the row, and the exhaustion
// path renders it into error_message. It is masked there as well as fenced: a
// transport error can quote a target URL whose query carries the key.
func TestSetReqErr_MasksTheUnderlyingCredential(t *testing.T) {
	t.Parallel()
	st := &requestState{logData: &requestLogData{masker: newCredentialMasker(opaqueKey)}}
	st.setReqErr(reqError{Kind: KindProviderError, Underlying: `Post "https://gw.example/v1?key=` + opaqueKey + `": EOF`})
	if strings.Contains(st.lastReqErr.Underlying, strings.Repeat("a", 40)) {
		t.Fatalf("the credential survived into Underlying: %s", st.lastReqErr.Underlying)
	}
	if strings.Contains(st.lastErr, strings.Repeat("a", 40)) {
		t.Fatalf("the credential survived into the rendered error: %s", st.lastErr)
	}
}

// A hedged probe logs against its own throwaway entry, and that entry carries
// the probe candidate's exact masker. The key here is never registered with
// util.HoldSecret, so the held-set union cannot catch it: only the candidate's
// own masker can, which is the case provider.HoldKeys leaves open when a key
// fails to decrypt.
func TestHedgeProbeLog_MasksTheProbeCandidatesCredential(t *testing.T) {
	t.Parallel()
	probeKey := "hk-" + strings.Repeat("b", 40)
	entry := &requestLogData{modelID: "m", endpointType: "chat", masker: newCredentialMasker("some-other-candidates-key-xxxxxxxx")}
	candidate := modelCandidate{provider: &provider.Provider{Name: "p"}, apiKey: probeKey}

	probe := hedgeProbeLog(entry, candidate)
	got := fencedFrameMessage(probe.fence(), probe.masks(), `Post "https://gw.example/v1?key=`+probeKey+`": malformed HTTP response`)
	if strings.Contains(got, strings.Repeat("b", 40)) {
		t.Fatalf("the probe candidate's key survived into the hedge log line: %s", got)
	}
}

// errString cuts at 500 runes, and every later mask (the log handler, the row
// choke point) sees only what survived the cut: a held key straddling rune 500
// would reach them as a head no exact pass can match. It is masked before the
// cut instead.
func TestErrString_MasksAHeldKeyStraddlingTheCut(t *testing.T) {
	t.Parallel()
	key := "heldkeyerrstring-" + strings.Repeat("r", 24)
	util.HoldSecret(key)
	got := errString(errors.New(strings.Repeat("x", 495) + key))
	if strings.Contains(got, key[:5]) {
		t.Fatalf("the head of a held key survived errString's cut: %q", got[480:])
	}
}

// The row's own counterpart of the log handler's masker: whatever built the
// message, the terminal write masks it with the attempt's key before its own
// cut. The key here is deliberately NOT held, so only the attempt's exact pass
// can catch it, and it straddles the cut, so only masking before the cut can.
func TestUpdateRequestLog_MasksTheRowBeforeTheCut(t *testing.T) {
	t.Parallel()
	key := "rowchokepointkey-" + strings.Repeat("s", 24)
	entry := &requestLogData{
		id:           "row-choke-point",
		state:        "pending",
		masker:       newCredentialMasker(key),
		errorMessage: strings.Repeat("x", maxLogMessageRunes-5) + key + " tail",
	}
	(&Handler{}).updateRequestLog(entry)
	if strings.Contains(entry.errorMessage, key[:5]) {
		t.Fatalf("the head of the attempt's key survived the row's cut: %q", entry.errorMessage[len(entry.errorMessage)-40:])
	}
}

// A wrapped error can carry a whole upstream body. errString masks only the
// window its cut can keep, plus slack for a key straddling the cut, so the
// scan stays bounded however large the error is, and a held key at the front
// is still masked.
func TestErrString_MasksInABoundedWindowOfAHugeError(t *testing.T) {
	t.Parallel()
	key := "heldkeyhugeerror-" + strings.Repeat("t", 24)
	util.HoldSecret(key)
	got := errString(errors.New(key + " then " + strings.Repeat("x", 1<<20)))
	if strings.Contains(got, key) {
		t.Fatalf("a held key at the front of a huge error survived: %q", got[:60])
	}
	if n := utf8.RuneCountInString(got); n > 501 {
		t.Fatalf("errString returned %d runes, want at most the 500-rune cut plus its marker", n)
	}
}
