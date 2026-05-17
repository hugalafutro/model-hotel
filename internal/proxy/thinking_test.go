package proxy

import (
	"testing"
)

func TestExtractThinking_TagFormat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantThink string
		wantCont  string
	}{
		{
			name:      "thinking tags",
			input:     "<thinking>Let me reason</thinking>The answer is 42",
			wantThink: "Let me reason",
			wantCont:  "The answer is 42",
		},
		{
			name:      "thought tags",
			input:     "<thought>Step 1</thought>Result",
			wantThink: "Step 1",
			wantCont:  "Result",
		},
		{
			name:      "think tags",
			input:     "<think>Reasoning here</think>Final answer",
			wantThink: "Reasoning here",
			wantCont:  "Final answer",
		},
		{
			name:      "start_thought tags",
			input:     "<start_thought>Deep thought</start_thought>Output",
			wantThink: "Deep thought",
			wantCont:  "Output",
		},
		{
			name:      "no tags",
			input:     "Just regular content",
			wantThink: "",
			wantCont:  "Just regular content",
		},
		{
			name:      "open tag only (streaming)",
			input:     "<thinking>Still thinking...",
			wantThink: "Still thinking...",
			wantCont:  "",
		},
		{
			name:      "multiline thinking",
			input:     "<thinking>Line 1\nLine 2\nLine 3</thinking>Answer",
			wantThink: "Line 1\nLine 2\nLine 3",
			wantCont:  "Answer",
		},
		{
			name:      "empty thinking tags",
			input:     "<thinking></thinking>Content",
			wantThink: "",
			wantCont:  "Content",
		},
		{
			name:      "uppercase tags not matched (providers use lowercase)",
			input:     "<Thinking>Some thought</Thinking>Result",
			wantThink: "",
			wantCont:  "<Thinking>Some thought</Thinking>Result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotThink, gotCont := ExtractThinking(tt.input)
			if gotThink != tt.wantThink {
				t.Errorf("thinking = %q, want %q", gotThink, tt.wantThink)
			}
			if gotCont != tt.wantCont {
				t.Errorf("content = %q, want %q", gotCont, tt.wantCont)
			}
		})
	}
}

func TestExtractThinking_FenceFormat(t *testing.T) {
	input := "<<\nFenced thinking content\n>>\nActual response"
	wantThink := "Fenced thinking content"
	wantCont := "Actual response"

	gotThink, gotCont := ExtractThinking(input)
	if gotThink != wantThink {
		t.Errorf("thinking = %q, want %q", gotThink, wantThink)
	}
	if gotCont != wantCont {
		t.Errorf("content = %q, want %q", gotCont, wantCont)
	}
}

func TestExtractThinking_FenceAndTag(t *testing.T) {
	input := "<<\nFence part\n>>\n<thinking>Tag part</thinking>Content"
	gotThink, gotCont := ExtractThinking(input)
	if gotThink != "Fence part\nTag part" {
		t.Errorf("thinking = %q, want combined fence+tag", gotThink)
	}
	if gotCont != "Content" {
		t.Errorf("content = %q, want %q", gotCont, "Content")
	}
}

func TestIsPartialThinkingTag(t *testing.T) {
	tests := []struct {
		partial string
		want    bool
	}{
		{"think", true},
		{"thinki", true},
		{"thou", true},
		{"star", true},
		{"xyz", false},
		{"div", false},
	}

	for _, tt := range tests {
		got := isPartialThinkingTag(tt.partial)
		if got != tt.want {
			t.Errorf("isPartialThinkingTag(%q) = %v, want %v", tt.partial, got, tt.want)
		}
	}
}

func TestNormalizeReasoningFields_ReasoningToReasoningContent(t *testing.T) {
	// Ollama-style: delta.reasoning present, delta.reasoning_content absent
	delta := map[string]interface{}{
		"content":   "",
		"reasoning": "Let me think about this",
	}

	changed := NormalizeReasoningFields(delta)
	if !changed {
		t.Error("expected changed=true")
	}
	if delta["reasoning_content"] != "Let me think about this" {
		t.Errorf("reasoning_content = %v, want 'Let me think about this'", delta["reasoning_content"])
	}
	// Original reasoning field should be preserved
	if delta["reasoning"] != "Let me think about this" {
		t.Errorf("reasoning field should be preserved, got %v", delta["reasoning"])
	}
}

func TestNormalizeReasoningFields_ReasoningContentAlreadyPresent(t *testing.T) {
	// DeepSeek-style: reasoning_content already present, no change needed
	delta := map[string]interface{}{
		"content":           "",
		"reasoning_content": "Already here",
		"reasoning":         "Should not override",
	}

	changed := NormalizeReasoningFields(delta)
	if changed {
		t.Error("expected changed=false when reasoning_content already present")
	}
	if delta["reasoning_content"] != "Already here" {
		t.Errorf("reasoning_content should not be overridden, got %v", delta["reasoning_content"])
	}
}

func TestNormalizeReasoningFields_ReasoningDetailsToReasoningContent(t *testing.T) {
	// OpenRouter/MiniMax-style: reasoning_details with text entries
	delta := map[string]interface{}{
		"content": "",
		"reasoning_details": []interface{}{
			map[string]interface{}{
				"type":   "reasoning.text",
				"text":   "Step 1: Analyze",
				"format": "google-gemini-v1",
			},
			map[string]interface{}{
				"type":   "reasoning.encrypted",
				"text":   "",
				"format": "anthropic-claude-v1",
			},
		},
	}

	changed := NormalizeReasoningFields(delta)
	if !changed {
		t.Error("expected changed=true")
	}
	if delta["reasoning_content"] != "Step 1: Analyze" {
		t.Errorf("reasoning_content = %v, want 'Step 1: Analyze'", delta["reasoning_content"])
	}
}

func TestNormalizeReasoningFields_ThinkingTagsInContent(t *testing.T) {
	// MiniMax native-style: <thinking> tags in content
	delta := map[string]interface{}{
		"content": "<thinking>My reasoning</thinking>The answer",
	}

	changed := NormalizeReasoningFields(delta)
	if !changed {
		t.Error("expected changed=true")
	}
	if delta["reasoning_content"] != "My reasoning" {
		t.Errorf("reasoning_content = %v, want 'My reasoning'", delta["reasoning_content"])
	}
	if delta["content"] != "The answer" {
		t.Errorf("content = %v, want 'The answer'", delta["content"])
	}
}

func TestNormalizeReasoningFields_NoChangeNeeded(t *testing.T) {
	// Standard DeepSeek-style: reasoning_content already present, no tags
	delta := map[string]interface{}{
		"content":           "Hello",
		"reasoning_content": "I thought about it",
	}

	changed := NormalizeReasoningFields(delta)
	if changed {
		t.Error("expected changed=false when no normalization needed")
	}
}

func TestNormalizeReasoningFields_EmptyDelta(t *testing.T) {
	delta := map[string]interface{}{
		"role": "assistant",
	}

	changed := NormalizeReasoningFields(delta)
	if changed {
		t.Error("expected changed=false for empty delta")
	}
}

func TestNormalizeMessageReasoning_ReasoningToReasoningContent(t *testing.T) {
	msg := map[string]interface{}{
		"role":      "assistant",
		"content":   "The answer",
		"reasoning": "My thought process",
	}

	changed := NormalizeMessageReasoning(msg)
	if !changed {
		t.Error("expected changed=true")
	}
	if msg["reasoning_content"] != "My thought process" {
		t.Errorf("reasoning_content = %v, want 'My thought process'", msg["reasoning_content"])
	}
}

func TestNormalizeMessageReasoning_ReasoningDetailsToReasoningContent(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "assistant",
		"content": "",
		"reasoning_details": []interface{}{
			map[string]interface{}{
				"type": "reasoning.text",
				"text": "Structured reasoning",
			},
		},
	}

	changed := NormalizeMessageReasoning(msg)
	if !changed {
		t.Error("expected changed=true")
	}
	if msg["reasoning_content"] != "Structured reasoning" {
		t.Errorf("reasoning_content = %v, want 'Structured reasoning'", msg["reasoning_content"])
	}
}

func TestNormalizeMessageReasoning_ThinkingTagsInContent(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "assistant",
		"content": "<thinking>Hidden reasoning</thinking>Visible answer",
	}

	changed := NormalizeMessageReasoning(msg)
	if !changed {
		t.Error("expected changed=true")
	}
	if msg["reasoning_content"] != "Hidden reasoning" {
		t.Errorf("reasoning_content = %v, want 'Hidden reasoning'", msg["reasoning_content"])
	}
	if msg["content"] != "Visible answer" {
		t.Errorf("content = %v, want 'Visible answer'", msg["content"])
	}
}
