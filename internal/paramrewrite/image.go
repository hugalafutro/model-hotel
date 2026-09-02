package paramrewrite

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// RewriteImageRequest adapts an OpenAI-shaped /v1/images/generations body to
// what a provider TYPE's image API accepts; like the other type-keyed rewrites
// here it cannot tell a relay behind that type apart from the real endpoint.
// Today that is xAI only: its image API has no "size" and answers 400
// "Argument not supported: size" to any request carrying one, which the OpenAI
// SDKs and open-webui send by default. The dimensions are kept as the
// aspect_ratio the grok-imagine family accepts when they reduce to one of its
// ratios, and dropped otherwise (xAI then picks its own default, which is
// still an image rather than a refusal). The older grok-2-image models take
// neither member, so for them the size is only dropped. A body that does not
// parse is forwarded as it came, like every other rewriter here. It reports
// the aspect_ratio it chose, empty when none was set, for the caller's log.
func RewriteImageRequest(body []byte, providerType, modelID string) ([]byte, string) {
	if providerType != "xai" {
		return body, ""
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var raw map[string]any
	if dec.Decode(&raw) != nil {
		return body, ""
	}
	size, present := raw["size"]
	if !present {
		return body, ""
	}
	delete(raw, "size")
	chosen := ""
	if s, ok := size.(string); ok && strings.Contains(modelID, "imagine") {
		if _, has := raw["aspect_ratio"]; !has {
			if ratio, ok := xaiAspectRatio(s); ok {
				raw["aspect_ratio"] = ratio
				chosen = ratio
			}
		}
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return body, ""
	}
	return out, chosen
}

// xaiAspectRatios are the aspect_ratio values xAI's image API documents.
var xaiAspectRatios = []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "2:1", "1:2", "19.5:9", "9:19.5", "20:9", "9:20", "21:9", "5:2"}

// xaiAspectRatioTolerance is how far a WxH size may sit from a documented
// ratio and still be read as it. DALL-E's 1792x1024 is 1.75 against 16:9's
// 1.78, so the OpenAI sizes need a few percent of slack; anything further off
// is a size this table cannot honestly stand in for.
const xaiAspectRatioTolerance = 0.03

// xaiAspectRatio maps an OpenAI "WxH" size to the nearest documented xAI
// aspect_ratio, reporting false for "auto", a malformed size, or one that no
// documented ratio approximates.
func xaiAspectRatio(size string) (string, bool) {
	w, h, ok := strings.Cut(strings.ToLower(strings.TrimSpace(size)), "x")
	if !ok {
		return "", false
	}
	wf, errW := strconv.ParseFloat(w, 64)
	hf, errH := strconv.ParseFloat(h, 64)
	if errW != nil || errH != nil || wf <= 0 || hf <= 0 {
		return "", false
	}
	want := wf / hf
	best, bestDiff := "", math.Inf(1)
	for _, r := range xaiAspectRatios {
		a, b, _ := strings.Cut(r, ":")
		af, _ := strconv.ParseFloat(a, 64)
		bf, _ := strconv.ParseFloat(b, 64)
		if diff := math.Abs(want-af/bf) / (af / bf); diff < bestDiff {
			best, bestDiff = r, diff
		}
	}
	if bestDiff > xaiAspectRatioTolerance {
		return "", false
	}
	return best, true
}
