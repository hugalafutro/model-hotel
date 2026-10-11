package anthropicegress

import (
	"strings"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// CacheControlRefused reports a Messages 400 that rejects the cache_control
// field itself. Anthropic never answers this way (every active Claude model
// supports prompt caching), but an operator-entered Messages endpoint that is
// not Anthropic's can refuse the unknown field, and so could a relay in front
// of it. The proxy learns the refusal per model and rebuilds without
// breakpoints, so such an endpoint costs one wasted round-trip once rather than
// a 400 on every request.
//
// Narrow on purpose: the message has to name the field. A 400 about a document
// or a malformed message must not switch caching off for a model that supports
// it, since that would silently put every later request back at full price.
func CacheControlRefused(body []byte) bool {
	msg := strings.ToLower(util.ErrorEnvelopeMessage(body))
	return strings.Contains(msg, "cache_control") || strings.Contains(msg, "cache control")
}
