package anthropicegress

import (
	"strings"
)

// imageMediaTypes are the media types Anthropic accepts in an image source.
// A data: URI carrying anything else is a document wearing an image costume:
// clients smuggle PDFs through image_url because the OpenAI file part 400s on
// Anthropic's compat endpoint, and imageBlock translates it to a document.
var imageMediaTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// cutDataScheme strips a data: scheme, which RFC 2397 makes case-insensitive.
func cutDataScheme(u string) (string, bool) {
	if len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		return u[5:], true
	}
	return "", false
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
	rest, isData := cutDataScheme(u)
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
