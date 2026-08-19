package anthropicegress

import (
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

// NeedsNativeRouting reports whether a chat-completions request body carries a
// content part Anthropic's OpenAI-compat endpoint cannot express and that
// therefore has to go to the native /v1/messages route: an OpenAI file part, or
// an image part whose data: URI holds a non-image media type.
//
// Detection is conservative by construction: an unparseable body, string
// message content, or a request holding only text and real images returns false
// and keeps the cheaper chat-completions path.
func NeedsNativeRouting(chatBody []byte) bool {
	var probe struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(chatBody, &probe) != nil {
		return false
	}

	for _, m := range probe.Messages {
		var parts []oaiContentPart
		// String content (and null content) fails this decode; only part arrays
		// can carry a file or an image source.
		if json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			switch p.Type {
			case "file":
				return true
			case "image_url":
				if p.ImageURL == nil {
					continue
				}
				if du, ok := parseDataURI(p.ImageURL.URL); ok && !imageMediaTypes[du.mediaType] {
					return true
				}
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
