package proxy

import (
	"encoding/json"
	"testing"
)

func TestExtractCacheTokens_PromptCacheHitTokens(t *testing.T) {
	u := Usage{PromptTokens: 100, PromptCacheHitTokens: 50}
	hit, miss := extractCacheTokens(u)
	if hit != 50 {
		t.Errorf("hit = %d, want 50", hit)
	}
	if miss != 50 {
		t.Errorf("miss = %d, want 50", miss)
	}
}

func TestExtractCacheTokens_CacheReadInputTokens(t *testing.T) {
	u := Usage{PromptTokens: 100, CacheReadInputTokens: 30}
	hit, miss := extractCacheTokens(u)
	if hit != 30 {
		t.Errorf("hit = %d, want 30", hit)
	}
	if miss != 70 {
		t.Errorf("miss = %d, want 70", miss)
	}
}

func TestExtractCacheTokens_PromptTokensDetails(t *testing.T) {
	u := Usage{PromptTokens: 100, PromptTokensDetails: &PromptTokensDetails{CachedTokens: 20}}
	hit, miss := extractCacheTokens(u)
	if hit != 20 {
		t.Errorf("hit = %d, want 20", hit)
	}
	if miss != 80 {
		t.Errorf("miss = %d, want 80", miss)
	}
}

func TestExtractCacheTokens_NoCache(t *testing.T) {
	u := Usage{PromptTokens: 100}
	hit, miss := extractCacheTokens(u)
	if hit != 0 || miss != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", hit, miss)
	}
}

func TestExtractCacheTokens_Priority(t *testing.T) {
	// PromptCacheHitTokens takes priority over CacheReadInputTokens
	u := Usage{
		PromptTokens:         100,
		PromptCacheHitTokens: 40,
		CacheReadInputTokens: 30,
	}
	hit, _ := extractCacheTokens(u)
	if hit != 40 {
		t.Errorf("hit = %d, want 40 (PromptCacheHitTokens priority)", hit)
	}
}

func TestExtractCacheTokens_MissClampedToZero(t *testing.T) {
	// When cached tokens > prompt tokens, miss should be clamped to 0
	u := Usage{PromptTokens: 10, PromptCacheHitTokens: 50}
	hit, miss := extractCacheTokens(u)
	if hit != 50 {
		t.Errorf("hit = %d, want 50", hit)
	}
	if miss != 0 {
		t.Errorf("miss = %d, want 0 (clamped)", miss)
	}
}

func TestExtractCacheTokens_NilPromptTokensDetails(t *testing.T) {
	u := Usage{PromptTokens: 100, PromptTokensDetails: nil}
	hit, _ := extractCacheTokens(u)
	if hit != 0 {
		t.Errorf("Expected hit=0 for nil PromptTokensDetails, got %d", hit)
	}
}

func TestNormalizeFinishReasonInChoices_EmptyChoices(t *testing.T) {
	var lastReason string
	normalizeFinishReasonInChoices(nil, &lastReason, "model", "provider")
	if lastReason != "" {
		t.Errorf("Expected no change, got %q", lastReason)
	}
	normalizeFinishReasonInChoices([]map[string]json.RawMessage{}, &lastReason, "model", "provider")
	if lastReason != "" {
		t.Errorf("Expected no change for empty slice, got %q", lastReason)
	}
}

func TestNormalizeFinishReasonInChoices_Anthropic(t *testing.T) {
	choices := []map[string]json.RawMessage{
		{"finish_reason": json.RawMessage(`"end_turn"`)},
	}
	var lastReason string
	normalizeFinishReasonInChoices(choices, &lastReason, "claude-3", "anthropic")

	var result string
	if err := json.Unmarshal(choices[0]["finish_reason"], &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if result != "stop" {
		t.Errorf("Expected 'stop', got %q", result)
	}
	if lastReason != "stop" {
		t.Errorf("Expected lastReason='stop', got %q", lastReason)
	}
}

func TestNormalizeFinishReasonInChoices_AlreadyNormalized(t *testing.T) {
	choices := []map[string]json.RawMessage{
		{"finish_reason": json.RawMessage(`"stop"`)},
	}
	var lastReason string
	normalizeFinishReasonInChoices(choices, &lastReason, "gpt-4", "openai")

	var result string
	if err := json.Unmarshal(choices[0]["finish_reason"], &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if result != "stop" {
		t.Errorf("Expected unchanged 'stop', got %q", result)
	}
}

func TestNormalizeFinishReasonInChoices_MissingKey(t *testing.T) {
	choices := []map[string]json.RawMessage{
		{"index": json.RawMessage(`0`)},
	}
	var lastReason string
	normalizeFinishReasonInChoices(choices, &lastReason, "model", "provider")
	if lastReason != "" {
		t.Errorf("Expected no change for missing finish_reason, got %q", lastReason)
	}
}

func TestNormalizeFinishReasonInChoices_EmptyFinishReason(t *testing.T) {
	choices := []map[string]json.RawMessage{
		{"finish_reason": json.RawMessage(`""`)},
	}
	var lastReason string
	normalizeFinishReasonInChoices(choices, &lastReason, "model", "provider")
	if lastReason != "" {
		t.Errorf("Expected no change for empty finish_reason, got %q", lastReason)
	}
}
