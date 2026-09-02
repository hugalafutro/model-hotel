package proxy

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The content fence: the request log stores fragments of upstream error text
// (request_logs.error_message, and attempts[].detail in the per-attempt
// trail), sanitized for credentials and UUIDs and bounded in length. None of
// that stops a provider that quotes the prompt back in its error message
// ("rate limit exceeded while processing: <the user's text>") from putting
// request content into a column the logs API returns and the dashboard
// renders, which breaks the one invariant the request log makes: request and
// prompt content is never logged.
//
// The fence closes that exactly rather than heuristically. The gateway holds
// the request body, so an echo of its content is, by definition, a substring
// of a string the client sent. Every stored fragment is checked against the
// request's own strings, and any run of contentEchoWindow or more runes that
// both share is replaced by "[content]". The provider's own words (its error
// code, its phrase, a reset timestamp) never appear in the request, so they
// survive; only what the client wrote is removed. That keeps the fragments
// useful for the work they exist for (reading an unrecognised 429 body to
// extend the phrase table) while making the invariant true by construction.
//
// Limits, all deliberate: an echo shorter than the window is not caught (a
// twelve-character secret quoted alone survives; the length cap on every
// fragment bounds what that can be), a provider that re-cases or re-spaces
// the text breaks the match at each change (the runs either side still fall),
// and base64 payloads are not indexed, since a fragment of an image is not a
// disclosure and indexing megabytes of it would cost seconds per failure.

const (
	// contentEchoWindow is the shortest shared run (in runes) the fence
	// masks. Shorter, and ordinary words the request and the provider both
	// use ("rate limit") would blank the provider's message; longer, and a
	// quoted sentence fragment would slip through.
	contentEchoWindow = 16
	// contentIndexCap bounds the runes of request content the fence indexes
	// per request, so a tool-heavy body cannot make one failure cost seconds.
	// A provider echoes the beginning of what it was sent, which is what the
	// first part of the walk covers.
	contentIndexCap = 2 << 20
	// contentBlobProbe is how far into a long string the walk looks for
	// whitespace before deciding it is an encoded payload rather than text.
	contentBlobProbe = 4096
	// contentMask replaces a masked run.
	contentMask = "[content]"
)

// contentFence holds the request body and, once something has to be fenced,
// the strings it carries. Built at request start from the body the proxy
// already holds (no copy) and consulted only on the error path, so a request
// that succeeds never parses its body a second time. Nil is a request with no
// content to fence (nothing parsed, or a multipart upload with no text
// fields), and every method tolerates it.
type contentFence struct {
	body   []byte
	extra  []string
	parsed bool
	strs   [][]rune
}

// newContentFence fences the strings of a JSON request body plus any extra
// text fields (a multipart request's prompt). Either may be empty.
func newContentFence(body []byte, extra ...string) *contentFence {
	if len(body) == 0 && len(extra) == 0 {
		return nil
	}
	return &contentFence{body: body, extra: extra}
}

// strings returns the request's content strings as rune slices, in the raw
// form and, where it differs, the JSON-escaped form: error_message stores
// the provider's body as sent, so an echo inside a JSON string member carries
// \" and \n where the request had " and a newline.
func (f *contentFence) strings() [][]rune {
	if f.parsed {
		return f.strs
	}
	f.parsed = true
	budget := contentIndexCap
	add := func(s string) {
		if budget <= 0 || utf8.RuneCountInString(s) < contentEchoWindow || isEncodedPayload(s) {
			return
		}
		r := []rune(s)
		if len(r) > budget {
			r = r[:budget]
		}
		budget -= len(r)
		f.strs = append(f.strs, r)
		if esc, err := json.Marshal(string(r)); err == nil {
			if e := string(esc[1 : len(esc)-1]); e != string(r) {
				f.strs = append(f.strs, []rune(e))
			}
		}
	}
	for _, s := range f.extra {
		add(s)
	}
	if len(f.body) > 0 {
		var v any
		if json.Unmarshal(f.body, &v) == nil {
			walkStrings(v, add)
		}
	}
	f.body, f.extra = nil, nil
	return f.strs
}

// walkStrings visits every string value in a decoded JSON document, in
// document order, so the index budget covers the front of the request first.
func walkStrings(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		visit(x)
	case []any:
		for _, e := range x {
			walkStrings(e, visit)
		}
	case map[string]any:
		// Deterministic order is not required for correctness (every string
		// is visited unless the budget runs out) and a sort would cost more
		// than it buys on a body this size.
		for _, e := range x {
			walkStrings(e, visit)
		}
	}
}

// isEncodedPayload reports a string that is an encoded blob rather than
// text: a data: URL, or a long run with no whitespace at all in its first
// contentBlobProbe runes (base64 audio, an image without the data: prefix).
func isEncodedPayload(s string) bool {
	if strings.HasPrefix(s, "data:") {
		return true
	}
	if len(s) <= contentBlobProbe {
		return false
	}
	for i, r := range s {
		if i >= contentBlobProbe {
			break
		}
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// mask returns texts with every run of contentEchoWindow or more runes that
// also appears in the request's content replaced by contentMask. The texts
// are indexed together so the request's strings are walked once for all of
// them; a nil fence or texts too short to hold a window come back unchanged.
func (f *contentFence) mask(texts []string) []string {
	if f == nil {
		return texts
	}
	type at struct{ text, pos int }
	index := map[string][]at{}
	runes := make([][]rune, len(texts))
	for t, s := range texts {
		if utf8.RuneCountInString(s) < contentEchoWindow {
			continue
		}
		runes[t] = []rune(s)
		for i := 0; i+contentEchoWindow <= len(runes[t]); i++ {
			key := string(runes[t][i : i+contentEchoWindow])
			index[key] = append(index[key], at{t, i})
		}
	}
	if len(index) == 0 {
		return texts
	}
	marks := make([][]bool, len(texts))
	hit := false
	for _, c := range f.strings() {
		for i := 0; i+contentEchoWindow <= len(c); i++ {
			hits, ok := index[string(c[i:i+contentEchoWindow])]
			if !ok {
				continue
			}
			hit = true
			for _, h := range hits {
				if marks[h.text] == nil {
					marks[h.text] = make([]bool, len(runes[h.text]))
				}
				for j := h.pos; j < h.pos+contentEchoWindow; j++ {
					marks[h.text][j] = true
				}
			}
		}
	}
	if !hit {
		return texts
	}
	out := make([]string, len(texts))
	for t, s := range texts {
		if marks[t] == nil {
			out[t] = s
			continue
		}
		var b strings.Builder
		masked := false
		for i, r := range runes[t] {
			if marks[t][i] {
				if !masked {
					b.WriteString(contentMask)
					masked = true
				}
				continue
			}
			masked = false
			b.WriteRune(r)
		}
		out[t] = b.String()
	}
	return out
}

// maskOne is mask for a single text.
func (f *contentFence) maskOne(text string) string {
	if f == nil || text == "" {
		return text
	}
	return f.mask([]string{text})[0]
}

// fenceContent runs the fence over everything on the row that came from an
// upstream error body: the error message and every attempt's detail. Called
// at the one write boundary every terminal update passes through, so no
// producer of upstream text has to remember it, and idempotent, since the
// interim and terminal updates both pass.
func (l *requestLogData) fenceContent() {
	if l == nil || l.content == nil {
		return
	}
	texts := make([]string, 0, 1+len(l.attempts))
	texts = append(texts, l.errorMessage)
	for _, a := range l.attempts {
		texts = append(texts, a.Detail)
	}
	fenced := l.content.mask(texts)
	l.errorMessage = fenced[0]
	for i := range l.attempts {
		l.attempts[i].Detail = fenced[i+1]
	}
}
