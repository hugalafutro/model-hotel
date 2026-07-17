package provider

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// The models.modality column is a *derived endpoint class* with a closed
// vocabulary: "chat", "embedding", "rerank", "image", "video", "tts", "stt".
// Discovery code must never hand-write it; input_modalities/output_modalities
// are the source of truth and NormalizeModelClassification derives the class
// from them once, after enrichment and before upsert.
//
// The only exception is explicit endpoint knowledge: when a provider API
// states the endpoint (Cohere rerank/embed flags, xAI image-generation
// listing), discovery sets one of explicitClasses and it is final. "video" is
// deliberately NOT explicit — the legacy vocabulary used "video" for
// video-*input* chat models, so video generation must be signalled through
// output_modalities instead.
var explicitClasses = map[string]bool{
	"embedding": true,
	"rerank":    true,
	"image":     true,
	"tts":       true,
	"stt":       true,
}

// canonicalModalityRank orders array entries deterministically so stored JSON
// is stable across discoveries. Unknown values sort after known ones in
// first-seen order (default-allow: never dropped).
var canonicalModalityRank = map[string]int{
	"text": 0, "image": 1, "audio": 2, "video": 3, "pdf": 4,
	"embedding": 5, "rerank": 6,
}

// DeriveModelClass computes the endpoint class from the modality arrays, with
// model-ID heuristics as tiebreakers where arrays cannot decide (audio-in/
// text-out transcription vs audio chat, and models with no modality info at
// all). Unknown always derives "chat" so a new modality never silently
// disappears from pickers.
func DeriveModelClass(input, output []string, modelID string) string {
	switch {
	case containsModality(output, "rerank"):
		return "rerank"
	case containsModality(output, "embedding"):
		return "embedding"
	}

	if containsModality(output, "text") {
		// Audio-in/text-out is structurally identical for Whisper-style
		// transcription and audio chat models; only the name can tell.
		if containsModality(input, "audio") && !containsModality(input, "text") {
			if inferNonChatModality(modelID) == "stt" {
				return "stt"
			}
		}
		return "chat"
	}

	if len(output) > 0 {
		switch {
		case containsModality(output, "video"):
			return "video"
		case containsModality(output, "image"):
			return "image"
		case containsModality(output, "audio"):
			return "tts"
		}
		// Non-empty output of only unknown modalities: default-allow.
		return "chat"
	}

	if class := inferNonChatModality(modelID); class != "" {
		return class
	}
	return "chat"
}

// NormalizeModelClassification makes a discovered model's classification
// fields consistent: parses/canonicalizes the modality arrays, folds legacy
// modality words and arrow strings ("text+image->text") into them, reconciles
// input-flavored capability flags with the input array (union, never clear),
// and finally rewrites m.Modality to the derived endpoint class.
func NormalizeModelClassification(m *model.Model) {
	input := parseModalityList(m.InputModalities)
	output := parseModalityList(m.OutputModalities)
	legacy := strings.ToLower(strings.TrimSpace(m.Modality))

	// OpenRouter/NanoGPT-style arrow strings describe both sides; use them
	// to fill whichever arrays are still empty, then re-derive the class.
	if strings.Contains(legacy, "->") {
		arrowIn, arrowOut := parseArrowModality(legacy)
		if len(input) == 0 {
			input = arrowIn
		}
		if len(output) == 0 {
			output = arrowOut
		}
		legacy = ""
	}

	// Explicit endpoint classes from endpoint-aware discovery are final. The
	// capability flags still sync from the input array so e.g. an
	// image-editing generation model with image input shows its Vision pill.
	if explicitClasses[legacy] {
		defIn, defOut := classDefaultArrays(legacy)
		if len(input) == 0 {
			input = defIn
		}
		if len(output) == 0 {
			output = defOut
		}
		m.Capabilities = syncCapsFromInput(m.Capabilities, parseCapabilityFlags(m.Capabilities), input)
		m.InputModalities = marshalModalityList(input)
		m.OutputModalities = marshalModalityList(output)
		m.Modality = legacy
		return
	}

	// Legacy input-derived words seed the input array when discovery gave
	// none ("vision" meant image input, never image output).
	if len(input) == 0 {
		switch legacy {
		case "vision":
			input = []string{"text", "image"}
		case "audio":
			input = []string{"text", "audio"}
		case "video":
			input = []string{"text", "video"}
		case "multimodal":
			input = []string{"text", "image", "audio"}
		}
	}

	caps := parseCapabilityFlags(m.Capabilities)
	input = unionCapsIntoInput(caps, input)

	inputWasEmpty := len(input) == 0
	if inputWasEmpty {
		input = []string{"text"}
	}

	class := DeriveModelClass(input, output, m.ModelID)
	if len(output) == 0 {
		_, output = classDefaultArrays(class)
	}
	if class == "stt" && inputWasEmpty {
		input = []string{"audio"}
	}

	input = canonicalizeModalityList(input)
	m.Capabilities = syncCapsFromInput(m.Capabilities, caps, input)
	m.InputModalities = marshalModalityList(input)
	m.OutputModalities = marshalModalityList(output)
	m.Modality = class
}

// NormalizeModels normalizes classification for a batch of discovered models.
func NormalizeModels(models []*model.Model) {
	for _, m := range models {
		if m != nil {
			NormalizeModelClassification(m)
		}
	}
}

// classDefaultArrays returns the default modality arrays implied by an
// endpoint class when discovery supplied none.
func classDefaultArrays(class string) (input, output []string) {
	switch class {
	case "stt":
		return []string{"audio"}, []string{"text"}
	case "tts":
		return []string{"text"}, []string{"audio"}
	case "embedding", "rerank", "image", "video":
		return []string{"text"}, []string{class}
	default:
		return []string{"text"}, []string{"text"}
	}
}

// capsInputFlags maps input-flavored capability flags to the input modality
// they imply. Kept in sync with web/src/components/capMeta.ts.
var capsInputFlags = map[string]string{
	"vision":      "image",
	"audio_input": "audio",
	"video_input": "video",
}

func parseCapabilityFlags(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		return nil
	}
	return caps
}

// unionCapsIntoInput adds modalities implied by capability flags (fill-only).
func unionCapsIntoInput(caps map[string]any, input []string) []string {
	for flag, modality := range capsInputFlags {
		if truthy, ok := caps[flag].(bool); ok && truthy && !containsModality(input, modality) {
			if len(input) == 0 {
				input = []string{"text"}
			}
			input = append(input, modality)
		}
	}
	return input
}

// syncCapsFromInput sets input-flavored capability flags implied by the input
// array (fill-only, never clears). Returns the (possibly rewritten)
// capabilities JSON.
func syncCapsFromInput(raw string, caps map[string]any, input []string) string {
	changed := false
	for flag, modality := range capsInputFlags {
		if !containsModality(input, modality) {
			continue
		}
		if truthy, ok := caps[flag].(bool); ok && truthy {
			continue
		}
		if caps == nil {
			caps = map[string]any{}
		}
		caps[flag] = true
		changed = true
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(caps)
	if err != nil {
		return raw
	}
	return string(out)
}

// parseModalityList parses a JSON array of modality strings, tolerating
// malformed input (returns nil so defaults apply).
func parseModalityList(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return canonicalizeModalityList(list)
}

// parseArrowModality splits an arrow-notation modality string like
// "text+image->text" into input and output modality lists.
func parseArrowModality(s string) (input, output []string) {
	parts := strings.SplitN(s, "->", 2)
	split := func(side string) []string {
		return canonicalizeModalityList(strings.FieldsFunc(side, func(r rune) bool {
			return r == '+' || r == ',' || r == ' '
		}))
	}
	input = split(parts[0])
	if len(parts) == 2 {
		output = split(parts[1])
	}
	return input, output
}

// canonicalizeModalityList lowercases, trims, dedupes and orders a modality
// list deterministically. Unknown values are kept (default-allow) after the
// known vocabulary, in first-seen order.
func canonicalizeModalityList(list []string) []string {
	seen := make(map[string]bool, len(list))
	cleaned := make([]string, 0, len(list))
	for _, v := range list {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		cleaned = append(cleaned, v)
	}
	// Stable insertion sort by canonical rank; unknowns (no rank) keep
	// first-seen order after known entries.
	const unknownRank = 100
	rank := func(v string) int {
		if r, ok := canonicalModalityRank[v]; ok {
			return r
		}
		return unknownRank
	}
	for i := 1; i < len(cleaned); i++ {
		for j := i; j > 0 && rank(cleaned[j]) < rank(cleaned[j-1]); j-- {
			cleaned[j], cleaned[j-1] = cleaned[j-1], cleaned[j]
		}
	}
	return cleaned
}

func marshalModalityList(list []string) string {
	if len(list) == 0 {
		return "[]"
	}
	out, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(out)
}

func containsModality(list []string, want string) bool {
	return slices.Contains(list, want)
}
