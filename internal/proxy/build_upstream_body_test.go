package proxy

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
			result := buildUpstreamBody([]byte(inputBody), tt.providerType, tt.wantModel, "gpt-4", tt.isStreaming, cache, tt.extraStrip)

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

	result := buildUpstreamBody(
		[]byte(inputBody), "openai", "gpt-4o", "gpt-4",
		true, cache,
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

func TestBuildUpstreamBody_UnparseableInput(t *testing.T) {
	t.Parallel()

	cache := &sync.Map{}
	result := buildUpstreamBody([]byte("not json"), "openai", "gpt-4o", "gpt-4", true, cache, nil)
	if string(result) != "not json" {
		t.Errorf("unparseable input should be returned as-is, got %q", string(result))
	}
}
