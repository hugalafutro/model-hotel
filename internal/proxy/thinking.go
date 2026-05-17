package proxy

import (
	"regexp"
	"strings"
)

// Thinking tag names that providers may use to wrap reasoning content.
// Mirrors the frontend's THINKING_TAG_NAMES in web/src/utils/thinking.ts.
var thinkingTagNames = []string{"thinking", "thought", "start_thought", "think"}

// Pre-compiled regexes for thinking tag extraction.
var (
	thinkingOpenRE  = regexp.MustCompile(`<(?:` + strings.Join(thinkingTagNames, "|") + `)>`)
	thinkingCloseRE = regexp.MustCompile(`<\/(?:` + strings.Join(thinkingTagNames, "|") + `)>`)
	// Fence format: <<\n...\n>>
	thinkingFenceRE = regexp.MustCompile(`^<<\s*\n([\s\S]*?)\n>>\s*\n?`)
	// Partial tag at end of string (e.g. "<thinki").
	partialTagRE = regexp.MustCompile(`<([a-z]*)$`)
)

// isPartialThinkingTag checks whether a partial tag (e.g. "thinki")
// could be the start of a known thinking tag name.
func isPartialThinkingTag(partial string) bool {
	lower := strings.ToLower(partial)
	for _, name := range thinkingTagNames {
		if strings.HasPrefix(name, lower) || strings.HasPrefix(lower, name) {
			return true
		}
	}
	return false
}

// ExtractThinking extracts reasoning content from thinking tags and/or
// fence markers in raw text. Returns (thinkingContent, remainingContent).
//
// This is a Go port of the frontend's extractThinking() in
// web/src/utils/thinking.ts, supporting:
//   - Fence format: <<\n...\n>>
//   - Tag format: <thinking>...</thinking>, <thought>...</thought>, etc.
//   - Partial tags at end of string (streaming edge case)
func ExtractThinking(raw string) (thinking, content string) {
	content = raw
	thinking = ""

	// Check fence format first.
	if fenceMatch := thinkingFenceRE.FindStringSubmatch(content); len(fenceMatch) > 1 {
		thinking = strings.TrimSpace(fenceMatch[1])
		content = content[len(fenceMatch[0]):]
	}

	// Check tag format.
	tagOpenIdx := thinkingOpenRE.FindStringIndex(content)
	if tagOpenIdx != nil {
		afterOpen := content[tagOpenIdx[0]:]
		closeMatch := thinkingCloseRE.FindStringIndex(afterOpen)
		if closeMatch != nil {
			// Full tag pair: <thinking>...</thinking>
			closeStart := tagOpenIdx[0] + closeMatch[0]
			closeEnd := tagOpenIdx[0] + closeMatch[1]
			inner := afterOpen[tagOpenIdx[1]-tagOpenIdx[0] : closeStart-tagOpenIdx[0]]
			inner = strings.TrimSpace(inner)
			if thinking != "" {
				thinking = thinking + "\n" + inner
			} else {
				thinking = inner
			}
			content = content[:tagOpenIdx[0]] + content[closeEnd:]
		} else {
			// Open tag without close (still streaming).
			inner := afterOpen[tagOpenIdx[1]-tagOpenIdx[0]:]
			inner = strings.TrimSpace(inner)
			if thinking != "" {
				thinking = thinking + "\n" + inner
			} else {
				thinking = inner
			}
			content = content[:tagOpenIdx[0]]
		}
	}

	// Clean up any remaining stray open/close tags.
	content = thinkingOpenRE.ReplaceAllString(content, "")
	content = thinkingCloseRE.ReplaceAllString(content, "")
	content = strings.TrimLeft(content, " \t\n")

	// Handle partial tag at end of content (streaming edge case).
	if content != "" {
		if partialMatch := partialTagRE.FindStringSubmatch(content); len(partialMatch) > 1 {
			if isPartialThinkingTag(partialMatch[1]) {
				content = content[:len(content)-len(partialMatch[0])]
			}
		}
	}

	return thinking, content
}

// ReasoningDetail represents a structured reasoning entry in
// OpenRouter/MiniMax reasoning_details arrays.
type ReasoningDetail struct {
	Type   string `json:"type"`             // "reasoning.text", "reasoning.summary", "reasoning.encrypted"
	Text   string `json:"text"`             // Plaintext reasoning content
	Format string `json:"format,omitempty"` // "anthropic-claude-v1", "google-gemini-v1", etc.
}

// NormalizeReasoningFields ensures that reasoning_content is always
// populated regardless of which field the upstream provider used.
// Returns true if the chunk was modified and needs re-serialization.
//
// Normalization rules:
//  1. delta.reasoning → delta.reasoning_content (Ollama, OpenRouter)
//  2. delta.reasoning_details text → delta.reasoning_content (OpenRouter structured, MiniMax split)
//  3. delta.content with <thinking> tags → extract to delta.reasoning_content (MiniMax native, some vLLM)
//
// Original fields are preserved (dual representation) for backward compatibility.
func NormalizeReasoningFields(delta map[string]interface{}) bool {
	changed := false

	// Get current reasoning_content value.
	rc, _ := delta["reasoning_content"].(string)
	rcEmpty := rc == ""

	// Rule 1: reasoning → reasoning_content
	if r, ok := delta["reasoning"].(string); ok && r != "" && rcEmpty {
		delta["reasoning_content"] = r
		rc = r
		rcEmpty = false
		changed = true
	}

	// Rule 2: reasoning_details text → reasoning_content
	if rcEmpty {
		if details, ok := delta["reasoning_details"].([]interface{}); ok && len(details) > 0 {
			var texts []string
			for _, d := range details {
				if dm, ok := d.(map[string]interface{}); ok {
					if t, _ := dm["type"].(string); t == "reasoning.text" {
						if txt, _ := dm["text"].(string); txt != "" {
							texts = append(texts, txt)
						}
					}
				}
			}
			if len(texts) > 0 {
				delta["reasoning_content"] = strings.Join(texts, "")
				changed = true
			}
		}
	}

	// Rule 3: <thinking> tags in content → reasoning_content
	if c, ok := delta["content"].(string); ok && c != "" {
		if thinking, remaining, found := extractThinkingFromContent(c); found {
			if rcEmpty {
				delta["reasoning_content"] = thinking
			} else {
				delta["reasoning_content"] = rc + thinking
			}
			delta["content"] = remaining
			changed = true
		}
	}

	return changed
}

// extractThinkingFromContent is a thin wrapper around ExtractThinking
// that returns (thinking, remaining, found) for use in normalization.
func extractThinkingFromContent(content string) (string, string, bool) {
	thinking, remaining := ExtractThinking(content)
	return thinking, remaining, thinking != ""
}

// NormalizeMessageReasoning applies the same normalization rules to
// a non-streaming message object. Returns true if modified.
func NormalizeMessageReasoning(msg map[string]interface{}) bool {
	changed := false

	rc, _ := msg["reasoning_content"].(string)
	rcEmpty := rc == ""

	// Rule 1: reasoning → reasoning_content
	if r, ok := msg["reasoning"].(string); ok && r != "" && rcEmpty {
		msg["reasoning_content"] = r
		rc = r
		rcEmpty = false
		changed = true
	}

	// Rule 2: reasoning_details text → reasoning_content
	if rcEmpty {
		if details, ok := msg["reasoning_details"].([]interface{}); ok && len(details) > 0 {
			var texts []string
			for _, d := range details {
				if dm, ok := d.(map[string]interface{}); ok {
					if t, _ := dm["type"].(string); t == "reasoning.text" {
						if txt, _ := dm["text"].(string); txt != "" {
							texts = append(texts, txt)
						}
					}
				}
			}
			if len(texts) > 0 {
				msg["reasoning_content"] = strings.Join(texts, "")
				changed = true
			}
		}
	}

	// Rule 3: <thinking> tags in content → reasoning_content
	if c, ok := msg["content"].(string); ok && c != "" {
		if thinking, remaining, found := extractThinkingFromContent(c); found {
			if rcEmpty {
				msg["reasoning_content"] = thinking
			} else {
				msg["reasoning_content"] = rc + thinking
			}
			msg["content"] = remaining
			changed = true
		}
	}

	return changed
}
