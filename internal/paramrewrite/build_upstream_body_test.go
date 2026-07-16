package paramrewrite

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestBuildUpstreamBody(t *testing.T) {
	t.Parallel()

	inputBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stream":true}`
	cache := &sync.Map{}

	tests := []struct {
		name           string
		providerType   string
		isStreaming    bool
		extraStrip     map[string]bool
		wantStreamOpts bool
		wantModel      string
		wantGoneParams []string
	}{
		{
			name:           "openai_streaming_gets_stream_options",
			providerType:   "openai",
			isStreaming:    true,
			extraStrip:     nil,
			wantStreamOpts: true,
			wantModel:      "gpt-4o",
			wantGoneParams: nil,
		},
		{
			name:           "anthropic_streaming_no_stream_options",
			providerType:   "anthropic",
			isStreaming:    true,
			extraStrip:     nil,
			wantStreamOpts: false,
			wantModel:      "claude-3-opus-20240229",
			wantGoneParams: []string{"top_p", "frequency_penalty", "presence_penalty"},
		},
		{
			name:           "openai_non_streaming_no_stream_options",
			providerType:   "openai",
			isStreaming:    false,
			extraStrip:     nil,
			wantStreamOpts: false,
			wantModel:      "gpt-4o",
			wantGoneParams: nil,
		},
		{
			name:           "retry_with_extra_strip_still_has_stream_options",
			providerType:   "openai",
			isStreaming:    true,
			extraStrip:     map[string]bool{"some_rejected_param": true},
			wantStreamOpts: true,
			wantModel:      "gpt-4o",
			wantGoneParams: []string{"some_rejected_param"},
		},
		{
			name:           "google_streaming_no_stream_options",
			providerType:   "google",
			isStreaming:    true,
			extraStrip:     nil,
			wantStreamOpts: false,
			wantModel:      "gemini-pro",
			wantGoneParams: []string{"frequency_penalty", "presence_penalty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildUpstreamBody([]byte(inputBody), tt.providerType, tt.wantModel, "gpt-4", tt.isStreaming, cache, &sync.Map{}, tt.extraStrip)

			var raw map[string]interface{}
			if err := json.Unmarshal(result, &raw); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}

			// Check model rename
			if raw["model"] != tt.wantModel {
				t.Errorf("model = %v, want %v", raw["model"], tt.wantModel)
			}

			// Check stream_options
			_, hasOpts := raw["stream_options"]
			if hasOpts != tt.wantStreamOpts {
				t.Errorf("stream_options present = %v, want %v", hasOpts, tt.wantStreamOpts)
			}
			if hasOpts {
				opts, ok := raw["stream_options"].(map[string]interface{})
				if !ok {
					t.Error("stream_options is not a map")
				} else if opts["include_usage"] != true {
					t.Errorf("stream_options.include_usage = %v, want true", opts["include_usage"])
				}
			}

			// Check stripped params are gone
			for _, p := range tt.wantGoneParams {
				if _, exists := raw[p]; exists {
					t.Errorf("param %q should be stripped but is present", p)
				}
			}
		})
	}
}

func TestBuildUpstreamBody_ExtraStripOnRetry(t *testing.T) {
	t.Parallel()

	// Simulates the 400 auto-retry path: the upstream rejected "min_p",
	// and the retry must strip it while preserving stream_options.
	inputBody := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stream":true,"min_p":0.8}`
	cache := &sync.Map{}

	result := BuildUpstreamBody(
		[]byte(inputBody), "openai", "gpt-4o", "gpt-4",
		true, cache, &sync.Map{},
		map[string]bool{"min_p": true}, // extra strip from 400 auto-retry
	)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// stream_options must be present (the regression this test guards against)
	if _, ok := raw["stream_options"]; !ok {
		t.Error("stream_options missing on retry — metering regression!")
	}

	// The rejected param must be stripped
	if _, ok := raw["min_p"]; ok {
		t.Error("min_p should be stripped on retry")
	}

	// Model must be renamed
	if raw["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", raw["model"])
	}
}

// Issue #281: chat_template_args is injected for OpenCode providers, but strict
// upstream backends reject it. These tests cover the inject/strip interaction.

func TestBuildUpstreamBody_OpenCodeInjectsChatTemplateArgs(t *testing.T) {
	t.Parallel()

	// Fresh request, empty cache: a model that accepts the field still gets it.
	inputBody := `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"stream":false}`
	cache := &sync.Map{}

	result := BuildUpstreamBody([]byte(inputBody), "opencode-go", "glm-5.2", "glm-5.2", false, cache, &sync.Map{}, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["chat_template_args"]; !ok {
		t.Error("chat_template_args should be injected for opencode-go when not rejected")
	}
}

func TestBuildUpstreamBody_LearnedChatTemplateArgsRejectionStays(t *testing.T) {
	t.Parallel()

	// A model that previously rejected chat_template_args has it cached. Because
	// injection now runs before the strip phases, the cached rejection must win:
	// the rebuilt body must NOT re-add the field. This is the core #281 fix —
	// without the reorder, every fresh request would 400 then retry.
	inputBody := `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"stream":false}`
	cache := &sync.Map{}
	rejected := map[string]bool{"chat_template_args": true}
	cache.Store("opencode-go:glm-5.2", &rejected)

	result := BuildUpstreamBody([]byte(inputBody), "opencode-go", "glm-5.2", "glm-5.2", false, cache, &sync.Map{}, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["chat_template_args"]; ok {
		t.Error("chat_template_args must stay stripped for a model with a learned rejection")
	}
}

func TestBuildUpstreamBody_ExtraStripChatTemplateArgsOnRetry(t *testing.T) {
	t.Parallel()

	// The immediate 400 auto-retry passes the freshly-learned rejection as
	// extraStrip; the injected field must be removed before the retry is sent.
	inputBody := `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"stream":false}`
	cache := &sync.Map{}

	result := BuildUpstreamBody(
		[]byte(inputBody), "opencode-go", "glm-5.2", "glm-5.2", false, cache, &sync.Map{},
		map[string]bool{"chat_template_args": true},
	)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["chat_template_args"]; ok {
		t.Error("chat_template_args must be stripped on the auto-retry")
	}
}

// gpt-5/o-series: max_tokens must be renamed to max_completion_tokens, carrying
// the caller's value, not dropped. These cover the learned-rename phase.

func TestBuildUpstreamBody_LearnedRenameMovesValue(t *testing.T) {
	t.Parallel()

	// A model with a learned max_tokens→max_completion_tokens rename cached: the
	// rebuilt body must drop max_tokens and carry its value under the new key.
	inputBody := `{"model":"gpt-5-nano","messages":[{"role":"user","content":"hi"}],"stream":false,"max_tokens":16}`
	renameCache := &sync.Map{}
	renames := map[string]string{"max_tokens": "max_completion_tokens"}
	renameCache.Store("openai:gpt-5-nano", &renames)

	result := BuildUpstreamBody([]byte(inputBody), "openai", "gpt-5-nano", "gpt-5-nano", false, &sync.Map{}, renameCache, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["max_tokens"]; ok {
		t.Error("max_tokens must be removed after rename")
	}
	if v, ok := raw["max_completion_tokens"]; !ok {
		t.Error("max_completion_tokens must be present after rename")
	} else if v != float64(16) {
		t.Errorf("max_completion_tokens = %v, want 16 (the original budget)", v)
	}
}

func TestBuildUpstreamBody_RenameDoesNotOverwriteExplicitTarget(t *testing.T) {
	t.Parallel()

	// If the caller already set max_completion_tokens, the rename must not
	// clobber it; it only drops the stale max_tokens.
	inputBody := `{"model":"gpt-5-nano","messages":[{"role":"user","content":"hi"}],"max_tokens":16,"max_completion_tokens":99}`
	renameCache := &sync.Map{}
	renames := map[string]string{"max_tokens": "max_completion_tokens"}
	renameCache.Store("openai:gpt-5-nano", &renames)

	result := BuildUpstreamBody([]byte(inputBody), "openai", "gpt-5-nano", "gpt-5-nano", false, &sync.Map{}, renameCache, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["max_tokens"]; ok {
		t.Error("stale max_tokens must be removed")
	}
	if raw["max_completion_tokens"] != float64(99) {
		t.Errorf("explicit max_completion_tokens must be preserved, got %v", raw["max_completion_tokens"])
	}
}

func TestBuildUpstreamBody_NoRenameWhenSourceAbsent(t *testing.T) {
	t.Parallel()

	// Rename cached but the request never set max_tokens: nothing to move, and
	// max_completion_tokens must not be conjured.
	inputBody := `{"model":"gpt-5-nano","messages":[{"role":"user","content":"hi"}]}`
	renameCache := &sync.Map{}
	renames := map[string]string{"max_tokens": "max_completion_tokens"}
	renameCache.Store("openai:gpt-5-nano", &renames)

	result := BuildUpstreamBody([]byte(inputBody), "openai", "gpt-5-nano", "gpt-5-nano", false, &sync.Map{}, renameCache, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := raw["max_completion_tokens"]; ok {
		t.Error("max_completion_tokens must not be added when max_tokens was absent")
	}
}

func TestBuildUpstreamBody_UnparseableInput(t *testing.T) {
	t.Parallel()

	cache := &sync.Map{}
	result := BuildUpstreamBody([]byte("not json"), "openai", "gpt-4o", "gpt-4", true, cache, &sync.Map{}, nil)
	if string(result) != "not json" {
		t.Errorf("unparseable input should be returned as-is, got %q", string(result))
	}
}

func TestBuildUpstreamBody_StripsEmptyToolCalls(t *testing.T) {
	t.Parallel()

	inputBody := `{"model":"gpt-4","messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":"aborted turn","tool_calls":[]},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"ok"}]}`

	result := BuildUpstreamBody([]byte(inputBody), "openai", "gpt-4o", "gpt-4", false, &sync.Map{}, &sync.Map{}, nil)

	var raw map[string]interface{}
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	msgs, ok := raw["messages"].([]interface{})
	if !ok || len(msgs) != 4 {
		t.Fatalf("messages = %v, want 4 entries", raw["messages"])
	}

	empty := msgs[1].(map[string]interface{})
	if _, exists := empty["tool_calls"]; exists {
		t.Error("empty tool_calls array should be stripped")
	}
	if empty["content"] != "aborted turn" {
		t.Errorf("stripped message content = %v, want unchanged", empty["content"])
	}

	withCalls := msgs[2].(map[string]interface{})
	tc, ok := withCalls["tool_calls"].([]interface{})
	if !ok || len(tc) != 1 {
		t.Errorf("non-empty tool_calls must be preserved, got %v", withCalls["tool_calls"])
	}
}

func TestBuildUpstreamBody_StripEmptyToolCallsTolerantOfShapes(t *testing.T) {
	t.Parallel()

	// No messages key, messages not an array, and a non-object message must
	// all pass through without panicking or being altered.
	for _, body := range []string{
		`{"model":"gpt-4"}`,
		`{"model":"gpt-4","messages":"weird"}`,
		`{"model":"gpt-4","messages":[42,{"role":"user","content":"hi","tool_calls":"nope"}]}`,
	} {
		result := BuildUpstreamBody([]byte(body), "openai", "gpt-4", "gpt-4", false, &sync.Map{}, &sync.Map{}, nil)
		var raw map[string]interface{}
		if err := json.Unmarshal(result, &raw); err != nil {
			t.Fatalf("result is not valid JSON for input %s: %v", body, err)
		}
	}
}
