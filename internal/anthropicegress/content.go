package anthropicegress

// The message-content half of the request translation: one OpenAI message to
// one Anthropic turn, and each content part (text, image, file, refusal) to
// the block Anthropic takes for it. A part that carries something Messages
// cannot be given fails the translation with ErrUnsupportedContent (see
// translateBlocks); a part that carries nothing is dropped.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// translateTurn builds one Anthropic turn from an OpenAI message. It returns
// nil when the message carries nothing Anthropic can hold (empty or null
// content and no tool calls).
func translateTurn(role string, m oaiMessage) (*antMessage, error) {
	// Plain string content stays a plain string — but only when nothing else
	// has to ride along in the same turn.
	if s, ok := egress.AsJSONString(m.Content); ok && len(m.ToolCalls) == 0 {
		if s == "" {
			return nil, nil
		}
		return &antMessage{Role: role, Content: s}, nil
	}

	blocks, err := translateBlocks(m.Content)
	if err != nil {
		return nil, err
	}
	for _, tc := range m.ToolCalls {
		blocks = append(blocks, antBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: util.ToolArgumentsObject(tc.Function.Arguments),
		})
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return &antMessage{Role: role, Content: blocks}, nil
}

// translateBlocks converts an OpenAI message content field (string, part array
// or null) into Anthropic content blocks. An empty text part and an empty
// image or file part are dropped: they carry nothing. A part that carries
// something Anthropic cannot be given (ErrUnsupportedContent) fails the
// translation, so the caller gets a refusal instead of an answer about content
// the model never saw.
func translateBlocks(raw json.RawMessage) ([]antBlock, error) {
	if !util.JSONMemberSet(raw) {
		return nil, nil
	}
	if s, ok := egress.AsJSONString(raw); ok {
		if s == "" {
			return nil, nil
		}
		return []antBlock{{Type: "text", Text: s}}, nil
	}

	var parts []oaiContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, invalidRequest("invalid message content: %s", jsonfault.Describe(err, len(raw)))
	}

	var blocks []antBlock
	for i, p := range parts {
		switch p.Type {
		case "text", "":
			// A part with no type is read as text, as egress.FlattenText reads it
			// for the system and tool roles, unless it carries an image or file
			// slot, which would be dropped unseen. An empty text block marshals as
			// {"type":"text"}, which Anthropic rejects — drop it.
			if p.Type == "" && partCarriesMedia(p) {
				return nil, fmt.Errorf("%w: part %d has no type and carries an image or file", ErrUnsupportedContent, i)
			}
			if p.Text == "" {
				continue
			}
			blocks = append(blocks, antBlock{Type: "text", Text: p.Text})
		case "refusal":
			// OpenAI's assistant refusal part, replayed from a transcript: its
			// text is what the model said, so it stays a text block.
			if p.Refusal == "" {
				continue
			}
			blocks = append(blocks, antBlock{Type: "text", Text: p.Refusal})
		case "image_url":
			if p.ImageURL == nil || p.ImageURL.URL == "" || emptyDataURI(p.ImageURL.URL) {
				continue
			}
			b, ok := imageBlock(p.ImageURL.URL)
			if !ok {
				return nil, fmt.Errorf("%w: part %d image_url holds an undecodable data: URI", ErrUnsupportedContent, i)
			}
			blocks = append(blocks, b)
		case "file":
			if p.File == nil {
				continue
			}
			f := *p.File
			if emptyDataURI(f.FileData) {
				// Nothing inline; the part is judged on what else it names.
				f.FileData = ""
			}
			if f == (oaiFile{}) {
				continue
			}
			b, ok := fileBlock(&f)
			if !ok {
				return nil, fmt.Errorf("%w: part %d file carries no inline data or URL (a file_id cannot be fetched)", ErrUnsupportedContent, i)
			}
			blocks = append(blocks, b)
		default:
			return nil, fmt.Errorf("%w: part %d has type %q", ErrUnsupportedContent, i, p.Type)
		}
	}
	return blocks, nil
}

// partCarriesMedia reports whether a part's image or file slot holds
// something: a URL, inline data, a file_url or a file_id. An empty slot
// carries nothing, like an empty text part.
func partCarriesMedia(p oaiContentPart) bool {
	if p.ImageURL != nil && p.ImageURL.URL != "" && !emptyDataURI(p.ImageURL.URL) {
		return true
	}
	if p.File != nil {
		f := *p.File
		if emptyDataURI(f.FileData) {
			f.FileData = ""
		}
		return f != (oaiFile{})
	}
	return false
}

// emptyDataURI reports a well-formed data: URI with nothing in it
// ("data:text/plain,"): a part that carries nothing, dropped like an empty
// text part rather than refused like a malformed one.
func emptyDataURI(u string) bool {
	rest, isData := util.CutDataScheme(u)
	if !isData {
		return false
	}
	_, payload, found := strings.Cut(rest, ",")
	return found && payload == ""
}

// capTemperature folds OpenAI's 0-2 temperature range into Anthropic's 0-1.
func capTemperature(t *float64) *float64 {
	if t == nil || *t <= maxTemperature {
		return t
	}
	capped := maxTemperature
	return &capped
}

// nonBlankStops drops whitespace-only stop sequences, which Messages refuses.
func nonBlankStops(stops []string) []string {
	out := stops[:0]
	for _, s := range stops {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// imageBlock maps an OpenAI image_url value onto an Anthropic block: a base64
// image source for a data: URI with an image media type, a url image source for
// an ordinary URL, and a document for a data: URI carrying anything else (which
// is how clients smuggle PDFs past the compat endpoint's file-part rejection).
func imageBlock(u string) (antBlock, bool) {
	du, ok := parseDataURI(u)
	if !ok {
		// A data: URI that does not parse (no comma, no payload) is malformed,
		// not a remote image: forwarded as a url source it would only 400.
		if _, isData := util.CutDataScheme(u); isData {
			return antBlock{}, false
		}
		return antBlock{Type: "image", Source: &antSource{Type: "url", URL: u}}, true
	}
	if imageMediaTypes[du.mediaType] {
		data, ok := base64Payload(du)
		if !ok {
			return antBlock{}, false
		}
		return antBlock{Type: "image", Source: &antSource{
			Type:      "base64",
			MediaType: du.mediaType,
			Data:      data,
		}}, true
	}
	return documentBlock(du)
}

// fileBlock maps an OpenAI file part onto an Anthropic document block.
func fileBlock(f *oaiFile) (antBlock, bool) {
	if f.FileData != "" {
		du, ok := parseDataURI(f.FileData)
		if !ok {
			return antBlock{}, false
		}
		return documentBlock(du)
	}
	if f.FileURL != "" {
		return antBlock{Type: "document", Source: &antSource{Type: "url", URL: f.FileURL}}, true
	}
	return antBlock{}, false
}

// documentBlock builds a document block from a parsed data: URI. A text/plain
// payload becomes a text source carrying the decoded text; everything else
// becomes a base64 PDF source, the only binary document Anthropic accepts.
func documentBlock(du dataURI) (antBlock, bool) {
	if du.mediaType == "text/plain" {
		text, ok := decodePayload(du)
		if !ok || text == "" {
			return antBlock{}, false
		}
		return antBlock{Type: "document", Source: &antSource{
			Type:      "text",
			MediaType: "text/plain",
			Data:      text,
		}}, true
	}
	data, ok := base64Payload(du)
	if !ok {
		return antBlock{}, false
	}
	return antBlock{Type: "document", Source: &antSource{
		Type:      "base64",
		MediaType: documentMediaType,
		Data:      data,
	}}, true
}

// base64Payload returns a data: URI payload in the base64 an Anthropic base64
// source expects. A ";base64" payload is already there; anything else is
// percent-encoded bytes, which re-encode losslessly. ok is false when the
// percent-decode fails — malformed input is unrecoverable, and forwarding it
// under a base64 label would ship garbage.
func base64Payload(du dataURI) (string, bool) {
	if du.base64 {
		return du.payload, true
	}
	raw, err := url.PathUnescape(du.payload)
	if err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString([]byte(raw)), true
}

// decodePayload returns the plain-text bytes of a data: URI payload: base64 for
// a ";base64" URI, percent-decoding otherwise.
func decodePayload(du dataURI) (string, bool) {
	if du.base64 {
		decoded, err := base64.StdEncoding.DecodeString(du.payload)
		if err != nil {
			return "", false
		}
		return string(decoded), true
	}
	unescaped, err := url.PathUnescape(du.payload)
	if err != nil {
		return "", false
	}
	return unescaped, true
}
