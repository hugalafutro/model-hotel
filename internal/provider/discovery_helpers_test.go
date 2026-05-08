package provider

import (
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// Test parseOpenRouterPricing
func TestParseOpenRouterPricing(t *testing.T) {
	tests := []struct {
		name    string
		pricing OpenRouterPricing
		wantIn  float64
		wantOut float64
	}{
		{
			name:    "zero pricing",
			pricing: OpenRouterPricing{Prompt: "0", Completion: "0"},
			wantIn:  0,
			wantOut: 0,
		},
		{
			name:    "decimal pricing",
			pricing: OpenRouterPricing{Prompt: "0.000001", Completion: "0.000002"},
			wantIn:  1.0,
			wantOut: 2.0,
		},
		{
			name:    "empty strings",
			pricing: OpenRouterPricing{Prompt: "", Completion: ""},
			wantIn:  0,
			wantOut: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inPrice, outPrice := parseOpenRouterPricing(tt.pricing)
			if inPrice != tt.wantIn {
				t.Errorf("inPrice = %f, want %f", inPrice, tt.wantIn)
			}
			if outPrice != tt.wantOut {
				t.Errorf("outPrice = %f, want %f", outPrice, tt.wantOut)
			}
		})
	}
}

// Test openRouterParamsToCapabilities
func TestOpenRouterParamsToCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		params   []string
		wantCaps model.Capability
	}{
		{
			name:   "empty params",
			params: []string{},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: false,
			},
		},
		{
			name:   "tools only",
			params: []string{"tools"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        false,
				StructuredOutput: false,
			},
		},
		{
			name:   "reasoning only",
			params: []string{"reasoning"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        true,
				StructuredOutput: false,
			},
		},
		{
			name:   "structured_outputs only",
			params: []string{"structured_outputs"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: true,
			},
		},
		{
			name:   "all capabilities",
			params: []string{"tools", "reasoning", "structured_outputs"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        true,
				StructuredOutput: true,
			},
		},
		{
			name:   "unknown params ignored",
			params: []string{"tools", "unknown-param"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        false,
				StructuredOutput: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := openRouterParamsToCapabilities(tt.params)
			if got.Streaming != tt.wantCaps.Streaming {
				t.Errorf("Streaming = %v, want %v", got.Streaming, tt.wantCaps.Streaming)
			}
			if got.ToolCalling != tt.wantCaps.ToolCalling {
				t.Errorf("ToolCalling = %v, want %v", got.ToolCalling, tt.wantCaps.ToolCalling)
			}
			if got.Reasoning != tt.wantCaps.Reasoning {
				t.Errorf("Reasoning = %v, want %v", got.Reasoning, tt.wantCaps.Reasoning)
			}
			if got.StructuredOutput != tt.wantCaps.StructuredOutput {
				t.Errorf("StructuredOutput = %v, want %v", got.StructuredOutput, tt.wantCaps.StructuredOutput)
			}
		})
	}
}

// Test isOpenRouterChatModel
func TestIsOpenRouterChatModel(t *testing.T) {
	tests := []struct {
		name  string
		model OpenRouterModel
		want  bool
	}{
		{
			name: "text output modality",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "text",
					OutputModalities: []string{"text"},
				},
			},
			want: true,
		},
		{
			name: "code output modality",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "code",
					OutputModalities: []string{"code"},
				},
			},
			want: true,
		},
		{
			name: "mixed output modalities with text",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "text,image",
					OutputModalities: []string{"text", "image"},
				},
			},
			want: true,
		},
		{
			name: "image only output",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "image",
					OutputModalities: []string{"image"},
				},
			},
			want: false,
		},
		{
			name: "modality string with ->text",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "image->text",
					OutputModalities: []string{"image"},
				},
			},
			want: true,
		},
		{
			name: "modality string with ->code",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "text->code",
					OutputModalities: []string{"code"},
				},
			},
			want: true,
		},
		{
			name: "empty output modalities",
			model: OpenRouterModel{
				Architecture: OpenRouterArchitecture{
					Modality:         "text",
					OutputModalities: []string{},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOpenRouterChatModel(tt.model)
			if got != tt.want {
				t.Errorf("isOpenRouterChatModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test toCohereNativeURL
func TestToCohereNativeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "compatibility URL",
			input: "https://api.cohere.ai/compatibility/v1",
			want:  "https://api.cohere.com",
		},
		{
			input: "https://api.cohere.ai/compatibility/v1/",
			want:  "https://api.cohere.com",
		},
		{
			name:  "native URL",
			input: "https://api.cohere.com",
			want:  "https://api.cohere.com",
		},
		{
			name:  "custom URL",
			input: "https://custom.cohere.example.com",
			want:  "https://custom.cohere.example.com",
		},
		{
			name:  "with trailing slash",
			input: "https://api.cohere.com/",
			want:  "https://api.cohere.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toCohereNativeURL(tt.input)
			if got != tt.want {
				t.Errorf("toCohereNativeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Test cohereFeaturesToCapabilities
func TestCohereFeaturesToCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		features []string
		wantCaps model.Capability
	}{
		{
			name:     "empty features",
			features: []string{},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: false,
				Vision:           false,
			},
		},
		{
			name:     "tools",
			features: []string{"tools"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        false,
				StructuredOutput: false,
				Vision:           false,
			},
		},
		{
			name:     "tool_choice",
			features: []string{"tool_choice"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        false,
				StructuredOutput: false,
				Vision:           false,
			},
		},
		{
			name:     "json_mode",
			features: []string{"json_mode"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: true,
				Vision:           false,
			},
		},
		{
			name:     "json_schema",
			features: []string{"json_schema"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: true,
				Vision:           false,
			},
		},
		{
			name:     "reasoning",
			features: []string{"reasoning"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        true,
				StructuredOutput: false,
				Vision:           false,
			},
		},
		{
			name:     "vision",
			features: []string{"vision"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      false,
				Reasoning:        false,
				StructuredOutput: false,
				Vision:           true,
			},
		},
		{
			name:     "all capabilities",
			features: []string{"tools", "reasoning", "json_schema", "vision"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        true,
				StructuredOutput: true,
				Vision:           true,
			},
		},
		{
			name:     "unknown features ignored",
			features: []string{"tools", "unknown-feature"},
			wantCaps: model.Capability{
				Streaming:        true,
				ToolCalling:      true,
				Reasoning:        false,
				StructuredOutput: false,
				Vision:           false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cohereFeaturesToCapabilities(tt.features)
			if got.Streaming != tt.wantCaps.Streaming {
				t.Errorf("Streaming = %v, want %v", got.Streaming, tt.wantCaps.Streaming)
			}
			if got.ToolCalling != tt.wantCaps.ToolCalling {
				t.Errorf("ToolCalling = %v, want %v", got.ToolCalling, tt.wantCaps.ToolCalling)
			}
			if got.Reasoning != tt.wantCaps.Reasoning {
				t.Errorf("Reasoning = %v, want %v", got.Reasoning, tt.wantCaps.Reasoning)
			}
			if got.StructuredOutput != tt.wantCaps.StructuredOutput {
				t.Errorf("StructuredOutput = %v, want %v", got.StructuredOutput, tt.wantCaps.StructuredOutput)
			}
			if got.Vision != tt.wantCaps.Vision {
				t.Errorf("Vision = %v, want %v", got.Vision, tt.wantCaps.Vision)
			}
		})
	}
}
