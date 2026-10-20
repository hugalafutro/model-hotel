package anthropicegress

import (
	"strings"

	"github.com/hugalafutro/model-hotel/internal/util"
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

// dataURI is a parsed data: URI.
type dataURI struct {
	mediaType string // lower-cased, parameters stripped ("application/pdf"); text/plain when omitted (RFC 2397)
	base64    bool   // the payload carries the ";base64" parameter
	payload   string // everything after the comma, undecoded
}

// parseDataURI splits a data: URI into its media type and payload. ok is false
// for anything that is not a parseable data: URI — an http(s) URL, or a
// truncated data: value with no comma or no payload.
func parseDataURI(u string) (dataURI, bool) {
	rest, isData := util.CutDataScheme(u)
	if !isData {
		return dataURI{}, false
	}
	meta, payload, found := strings.Cut(rest, ",")
	if !found || payload == "" {
		return dataURI{}, false
	}
	mediaType, params, _ := strings.Cut(meta, ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	du := dataURI{mediaType: mediaType, payload: payload}
	for _, param := range strings.Split(params, ";") {
		if strings.EqualFold(strings.TrimSpace(param), "base64") {
			du.base64 = true
		}
	}
	return du, true
}
