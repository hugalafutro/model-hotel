package proxy

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
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
// request's content strings, and any run of contentEchoWindow or more runes
// that both share is replaced by "[content]". The provider's own words (its
// error code, its phrase, a reset timestamp) do not come from the request, so
// they survive; only what the client wrote is removed. That keeps the
// fragments useful for the work they exist for (reading an unrecognised 429
// body to extend the phrase table) while making the invariant true by
// construction.
//
// Each content string is indexed in the forms a stored fragment can carry it:
// as written; whitespace-collapsed, because attemptDetail collapses the
// trail's detail and a prompt with indented code or double spaces would
// otherwise stop matching at every run of spaces; and the JSON-escaped
// rendering of both, because error_message stores the provider's body as sent
// and an echo inside a JSON string member carries \" and \n where the request
// had a quote and a newline.
//
// Limits, all deliberate: an echo shorter than the window is not caught (a
// twelve-character secret quoted alone survives; the length cap on every
// fragment bounds what that can be), a provider that re-cases or otherwise
// rewrites the text breaks the match at each change (the runs either side
// still fall), and encoded payloads are not indexed, since a fragment of an
// image is not a disclosure and indexing megabytes of it would cost seconds
// per failure. Content is content: a prompt that quotes a provider's error
// text back at the gateway ("why do I get 'Rate limit exceeded for ...'") is
// fenced when the provider says the same words, and that is the invariant
// holding, not a false positive.

const (
	// contentEchoWindow is the shortest shared run (in runes) the fence
	// masks. Shorter, and ordinary words the request and the provider both
	// use ("rate limit") would blank the provider's message; longer, and a
	// quoted sentence fragment would slip through.
	contentEchoWindow = 16
	// contentIndexCap bounds the runes of request content the fence indexes
	// per request, per form (each of the four forms below has its own
	// budget, so the form the trail's detail needs is never starved by the
	// raw one). The ceiling is therefore four times this: on the largest
	// request whose forms all differ, about four million windows hashed
	// once (windowSet, tens of milliseconds) and then a few microseconds per
	// fenced text, with the window set retained for the failure's lifetime.
	// The walk
	// visits the content-bearing members first (contentFirstKeys) and every
	// map in sorted key order, so what the cap leaves out is deterministic
	// and is the tail of the request.
	contentIndexCap = 1 << 20
	// contentBlobProbe is how many runes into a long string the walk looks
	// before deciding it is an encoded payload rather than text.
	contentBlobProbe = 4096
	// contentMask replaces a masked run.
	contentMask = "[content]"
)

// contentFirstKeys are the members that carry the prompt, visited before any
// other member of the same object so the index budget covers them first.
var contentFirstKeys = []string{"messages", "input", "prompt", "query", "documents", "system", "instructions", "contents", "text", "content"}

// contentRoutingKeys are members whose string value is routing metadata, not
// content: a model name is 16 runes on its own and appears verbatim in the
// gateway's own messages ("no available provider for hotel/x"), which the
// fence would otherwise blank. Only a string directly under one of these keys
// is skipped; an object under them is walked. Identifiers the client chose
// (the OpenAI "user" field, a message's "name") are not here: an e-mail
// address in one is the client's data, and nothing the gateway says repeats
// it.
var contentRoutingKeys = map[string]bool{
	"model": true, "role": true, "type": true, "id": true,
	"format": true, "voice": true, "size": true, "quality": true, "style": true,
	"tool_choice": true, "encoding_format": true, "reasoning_effort": true, "response_format": true,
}

// contentFence holds the request body and, once something has to be fenced,
// the content strings it carries. Built at request start from the body the
// proxy already holds (no copy) and consulted only when there is upstream
// error text to store, so a request that never records a failed attempt
// never parses its body a second time. Nil is a request with no content to
// fence (nothing parsed, or a multipart upload with no text fields), and
// every method tolerates it. The parse is guarded by a Once: the fence is
// shared between the log entry and the stream state, and though every caller
// today runs on the serving goroutine, nothing should depend on that.
type contentFence struct {
	body  []byte
	extra []string
	once  sync.Once
	strs  [][]rune
	// windows is the sorted, deduplicated hash of every content window,
	// built once on the first mask (windowSet) and reused by every later
	// one: the streaming error attribute fences once per SSE error frame,
	// and walking the content again for each was the cost that scaled with
	// the prompt rather than with the frame. Once built, the content strings
	// themselves are released; nothing on the request path reads them
	// again.
	winOnce sync.Once
	windows []uint64
}

// newContentFence fences the strings of a JSON request body plus any extra
// text fields (a multipart request's prompt). Either may be empty.
func newContentFence(body []byte, extra ...string) *contentFence {
	if len(body) == 0 && len(extra) == 0 {
		return nil
	}
	return &contentFence{body: body, extra: extra}
}

// strings returns the request's content strings in every indexed form.
// Nil once windowSet has built the set and released them.
func (f *contentFence) strings() [][]rune {
	f.once.Do(f.parse)
	return f.strs
}

// The forms a stored fragment can carry a string in: as written (a plain-text
// body); whitespace-collapsed (the trail's detail of one); JSON-escaped
// (error_message stores a JSON body as sent); and collapsed after escaping
// (the trail's detail of a JSON body). Each has its own index budget.
var contentForms = []func(string) string{
	func(s string) string { return s },
	collapseSpace,
	escapeJSON,
	func(s string) string { return collapseSpace(escapeJSON(s)) },
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func escapeJSON(s string) string {
	esc, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return string(esc[1 : len(esc)-1])
}

func (f *contentFence) parse() {
	budgets := make([]int, len(contentForms))
	for i := range budgets {
		budgets[i] = contentIndexCap
	}
	forms := func(s string) {
		if isEncodedPayload(s) {
			return
		}
		seen := map[string]bool{}
		for i, form := range contentForms {
			text := form(s)
			if seen[text] || budgets[i] <= 0 || utf8.RuneCountInString(text) < contentEchoWindow {
				continue
			}
			seen[text] = true
			r := []rune(text)
			if len(r) > budgets[i] {
				r = r[:budgets[i]]
			}
			budgets[i] -= len(r)
			f.strs = append(f.strs, r)
		}
	}
	for _, s := range f.extra {
		forms(s)
	}
	if len(f.body) > 0 {
		var v any
		if json.Unmarshal(f.body, &v) == nil {
			walkStrings(v, "", forms)
		}
	}
	f.body, f.extra = nil, nil
}

// walkStrings visits every content string in a decoded JSON document. The
// order is deterministic: within an object the content-bearing members come
// first, then the rest by key, so the index budget always covers the same
// part of the same request. key is the member the value sits under.
func walkStrings(v any, key string, visit func(string)) {
	switch x := v.(type) {
	case string:
		if !contentRoutingKeys[key] {
			visit(x)
		}
	case []any:
		for _, e := range x {
			walkStrings(e, key, visit)
		}
	case map[string]any:
		for _, k := range orderedKeys(x) {
			walkStrings(x[k], k, visit)
		}
	}
}

func orderedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rank := func(k string) int {
		for i, first := range contentFirstKeys {
			if k == first {
				return i
			}
		}
		return len(contentFirstKeys)
	}
	sort.SliceStable(keys, func(i, j int) bool { return rank(keys[i]) < rank(keys[j]) })
	return keys
}

// isEncodedPayload reports a string that is an encoded blob rather than
// text: a data: URL, or a long run whose first contentBlobProbe runes are all
// from the base64 / URL-safe alphabet (base64 audio, an image without the
// data: prefix). Prose in any script fails that test at its first space or
// punctuation mark, CJK included, so it is indexed. Two edges are accepted:
// base64 wrapped with newlines is judged text and spends index budget, and a
// long run that happens to be all letters (a nucleotide sequence) is judged
// a blob and not indexed.
func isEncodedPayload(s string) bool {
	if strings.HasPrefix(s, "data:") {
		return true
	}
	if utf8.RuneCountInString(s) <= contentBlobProbe {
		return false
	}
	n := 0
	for _, r := range s {
		n++
		if n > contentBlobProbe {
			break
		}
		if !isBase64Rune(r) {
			return false
		}
	}
	return true
}

func isBase64Rune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		r == '+' || r == '/' || r == '=' || r == '-' || r == '_'
}

// windowHash is an FNV-1a over the runes of one window, computed per
// position without building a string. A hit is taken on the 64-bit hash
// alone. Two distinct windows collide with probability 2^-64, so a lookup
// against a set of N windows is wrong with probability N/2^64 (2^-42 at the
// four-million-window ceiling), and a false hit costs a masked run of
// provider text (sixteen runes, or more where it bridges two real masks),
// never a leak: a genuine echo has the same hash by construction, so no
// echo is ever missed. That is not worth keeping every content window in
// memory for a rune-by-rune check.
func windowHash(r []rune) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range r {
		h ^= uint64(c) //nolint:gosec // a rune is 21 bits wide; the hash only needs distinct inputs to differ
		h *= 1099511628211
	}
	return h
}

// windowSet returns the sorted, deduplicated hashes of every window of the
// request's content, built once. Eight bytes per window: a 10 KB prompt is
// 80 KB, and the largest request the index budget admits is about 32 MB,
// held only for the failure's lifetime; the content strings are released
// once the set exists, so the fence holds less than it did when it kept
// them to walk on every call. The sort is a radix sort: a comparison sort
// of four million hashes cost more than the walk it replaced, so a
// single-frame failure would have paid for the memo without using it.
func (f *contentFence) windowSet() []uint64 {
	f.winOnce.Do(func() {
		strs := f.strings()
		n := 0
		for _, c := range strs {
			if len(c) >= contentEchoWindow {
				n += len(c) - contentEchoWindow + 1
			}
		}
		set := make([]uint64, 0, n)
		for _, c := range strs {
			for i := 0; i+contentEchoWindow <= len(c); i++ {
				set = append(set, windowHash(c[i:i+contentEchoWindow]))
			}
		}
		radixSort(set)
		set = slices.Compact(set)
		f.windows = slices.Clip(set)
		f.strs = nil
	})
	return f.windows
}

// radixSort sorts hashes in place with an LSD radix sort, eight passes of
// eight bits, which is linear in the count where a comparison sort of the
// same four million values is not: on the largest request the index admits
// the sort is about 100ms, the hashing about 15ms, and the old per-call
// walk this replaces was about 70ms, so the first fence of such a request
// costs somewhat more than before and every later one costs microseconds.
// The scratch buffer is transient.
func radixSort(v []uint64) {
	if len(v) < 2 {
		return
	}
	buf := make([]uint64, len(v))
	src, dst := v, buf
	for shift := uint(0); shift < 64; shift += 8 {
		var count [256]int
		for _, x := range src {
			count[(x>>shift)&0xff]++
		}
		pos := 0
		for i := range count {
			c := count[i]
			count[i] = pos
			pos += c
		}
		for _, x := range src {
			d := (x >> shift) & 0xff
			dst[count[d]] = x
			count[d]++
		}
		src, dst = dst, src
	}
	// Eight passes end with the result back in v.
}

// hasWindow reports whether the sorted set holds a window with this hash.
func hasWindow(set []uint64, h uint64) bool {
	i := sort.Search(len(set), func(i int) bool { return set[i] >= h })
	return i < len(set) && set[i] == h
}

// mask returns texts with every run of contentEchoWindow or more runes that
// also appears in the request's content replaced by contentMask. Each text is
// walked once, window by window, against the content's window set; a nil
// fence or texts too short to hold a window come back unchanged.
func (f *contentFence) mask(texts []string) []string {
	if f == nil {
		return texts
	}
	runes := make([][]rune, len(texts))
	marks := make([][]bool, len(texts))
	hit := false
	var set []uint64
	for t, s := range texts {
		if utf8.RuneCountInString(s) < contentEchoWindow {
			continue
		}
		if set == nil {
			set = f.windowSet()
		}
		runes[t] = []rune(s)
		for i := 0; i+contentEchoWindow <= len(runes[t]); i++ {
			if !hasWindow(set, windowHash(runes[t][i:i+contentEchoWindow])) {
				continue
			}
			hit = true
			if marks[t] == nil {
				marks[t] = make([]bool, len(runes[t]))
			}
			for j := i; j < i+contentEchoWindow; j++ {
				marks[t][j] = true
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
