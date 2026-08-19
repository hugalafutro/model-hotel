package anthropicegress

import (
	"bytes"
	"encoding/json"
	"strings"
)

// imageMediaTypes are the media types Anthropic accepts in an image source.
// A data: URI carrying anything else is a document wearing an image costume —
// clients smuggle PDFs through image_url precisely because the OpenAI file
// part 400s on the compat endpoint.
var imageMediaTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// carriesDocuments reports whether a document on a message role reaches
// Anthropic intact. Only a user turn does. System and developer content is
// lifted into the top-level system prompt and a tool message becomes a
// tool_result, and Anthropic types both as text, so translateMessages flattens
// them and any document there is dropped. An assistant turn holds blocks, but
// document is not in Anthropic's assistant content-block schema, so a document
// there is a guaranteed 400.
//
// A document on a non-carrying role is dropped in translation, and a request
// routes native on the strength of its carrying-role documents alone: a request
// pairing a user document with a system one goes native and loses the system
// one, where the compat endpoint would have 400'd on both. That is the accepted
// cost of serving the document the request is actually about.
func carriesDocuments(role string) bool {
	switch role {
	case "system", "developer", "tool", "assistant":
		return false
	default: // "user", and anything unrecognised (translated as a user turn)
		return true
	}
}

// filePartHint is the literal an OpenAI file part cannot avoid: "file" is both
// its type and its content object's key.
var filePartHint = []byte(`"file"`)

// jsonEscapeHint is the pre-filter's escape hatch. JSON may spell any of the
// literals scanned for here with \uXXXX escapes that only the full decode
// resolves, so a body carrying any escape at all falls through to that decode
// rather than being ruled out on a byte scan that cannot see through it.
var jsonEscapeHint = []byte(`\u`)

// dataURIHint opens every inline payload, image or document alike.
var dataURIHint = []byte("data:")

// mayNeedNativeRouting is a pre-filter over the raw request body. The full
// decode below materialises every image and file payload as a Go string, on
// every chat request to an Anthropic provider and once per candidate attempt,
// so a multi-megabyte base64 image is copied and thrown away on requests that
// were never going native. This byte scan rules out the traffic that makes up
// almost all of it — plain text, and the vision requests this detector exists
// to reject — without allocating.
//
// It is one-sided by construction: false means no part can possibly route
// native, true only means the decode has to run.
func mayNeedNativeRouting(chatBody []byte) bool {
	return bytes.Contains(chatBody, filePartHint) ||
		bytes.Contains(chatBody, jsonEscapeHint) ||
		hasNonImageDataURI(chatBody)
}

// hasNonImageDataURI reports whether the body holds a data: URI that is not
// plainly one of Anthropic's four image media types. A base64 image is the one
// bulky payload that never routes native, so ruling it out here is the whole
// point of the pre-filter.
//
// Every way of being unsure resolves to true: an escaped or padded media type,
// an image/* type outside imageMediaTypes (image/svg+xml), a truncated URI. The
// scan returns false for an occurrence only when the bytes after "data:" are
// exactly an accepted image media type followed by the ";" or "," that ends it
// — precisely the shape parseDataURI and imageMediaTypes accept together.
func hasNonImageDataURI(body []byte) bool {
	for rest := body; ; {
		i := bytes.Index(rest, dataURIHint)
		if i < 0 {
			return false
		}
		rest = rest[i+len(dataURIHint):]
		if !startsWithImageMediaType(rest) {
			return true
		}
	}
}

// maxMediaTypeBytes bounds how far the pre-filter looks for the end of a media
// type. Every accepted one is under a dozen bytes; the cap keeps a body that
// opens a data: URI and never closes its media type from being copied out.
const maxMediaTypeBytes = 64

// startsWithImageMediaType reports whether b opens with an accepted image media
// type terminated by ";" or ",". The comparison is case-insensitive to match
// parseDataURI, which lower-cases before the imageMediaTypes lookup.
func startsWithImageMediaType(b []byte) bool {
	if len(b) > maxMediaTypeBytes {
		b = b[:maxMediaTypeBytes]
	}
	end := bytes.IndexAny(b, ";,")
	if end < 0 {
		return false
	}
	return imageMediaTypes[strings.ToLower(string(b[:end]))]
}

// routesNative reports whether one content part both needs the native route
// and survives translation to an Anthropic block.
//
// The two halves are inseparable, and this is the invariant any change here
// has to keep: A REQUEST ROUTES NATIVE ONLY IF EVERY PART THAT MADE IT ROUTE
// NATIVE SURVIVES TRANSLATION. translateBlocks drops a part it cannot express,
// so a gate that matched on the part TYPE alone would send a file part with
// nothing usable in it — {"file":{"file_id":"file-abc"}}, an unparseable
// file_data, an undecodable payload — to /v1/messages carrying only the
// surrounding text, and return a confident 200 about a document the model never
// saw. Nothing in the logs would show it: the request completes, delivers
// content, and meters normally. The compat endpoint answers the same request
// with a clean 400, which is the better outcome, so the untranslatable part
// keeps the compat path. Every check below therefore runs the same helper
// translateBlocks runs, not a copy of its conditions.
func routesNative(p oaiContentPart) bool {
	switch p.Type {
	case "file":
		if p.File == nil {
			return false
		}
		_, ok := fileBlock(p.File)
		return ok
	case "image_url":
		if p.ImageURL == nil || p.ImageURL.URL == "" {
			return false
		}
		du, ok := parseDataURI(p.ImageURL.URL)
		if !ok || imageMediaTypes[du.mediaType] {
			// A real image, or a URL the compat endpoint carries itself.
			return false
		}
		_, ok = documentBlock(du)
		return ok
	}
	return false
}

// NeedsNativeRouting reports whether a chat-completions request body carries a
// content part Anthropic's OpenAI-compat endpoint cannot express and that
// therefore has to go to the native /v1/messages route: an OpenAI file part, or
// an image part whose data: URI holds a non-image media type, on a message role
// whose documents survive translation — and that TranslateRequest can actually
// turn into an Anthropic block (see routesNative for why the second half is not
// optional).
//
// Detection is conservative by construction: an unparseable body, string
// message content, or a request holding only text and real images returns false
// and keeps the cheaper chat-completions path.
func NeedsNativeRouting(chatBody []byte) bool {
	if !mayNeedNativeRouting(chatBody) {
		return false
	}

	var probe struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(chatBody, &probe) != nil {
		return false
	}

	for _, m := range probe.Messages {
		if !carriesDocuments(m.Role) {
			continue
		}
		var parts []oaiContentPart
		// String content (and null content) fails this decode; only part arrays
		// can carry a file or an image source.
		if json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			if routesNative(p) {
				return true
			}
		}
	}
	return false
}

// dataURI is a parsed data: URI.
type dataURI struct {
	mediaType string // lower-cased, parameters stripped ("application/pdf")
	base64    bool   // the payload carries the ";base64" marker
	payload   string // everything after the comma, undecoded
}

// parseDataURI splits a data: URI into its media type and payload. ok is false
// for anything that is not a parseable data: URI — an http(s) URL, or a
// truncated data: value with no comma or no payload.
func parseDataURI(u string) (dataURI, bool) {
	rest, isData := strings.CutPrefix(u, "data:")
	if !isData {
		return dataURI{}, false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found || payload == "" {
		return dataURI{}, false
	}
	mediaType, params, _ := strings.Cut(meta, ";")
	return dataURI{
		mediaType: strings.ToLower(strings.TrimSpace(mediaType)),
		base64:    strings.Contains(strings.ToLower(params), "base64"),
		payload:   payload,
	}, true
}
