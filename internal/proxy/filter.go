package proxy

import (
	"encoding/json"
	"strings"
)

type ParameterFilter struct {
	providerType string
}

func NewParameterFilter(providerType string) *ParameterFilter {
	return &ParameterFilter{providerType: providerType}
}

type UnsupportedParams map[string]bool

var providerUnsupportedParams = map[string]UnsupportedParams{
	"openai": {
		"all of them are supported": false,
	},
	"groq": {
		"logprobs":        true,
		"response_format": true,
		"stream_options":  true,
	},
	"ollama": {
		"logprobs":        true,
		"response_format": true,
		"stream_options":  true,
	},
	"azure": {
		"seed": true,
	},
}

func (f *ParameterFilter) FilterRequest(req map[string]interface{}) (map[string]interface{}, []string) {
	filtered := make(map[string]interface{})
	strippedParams := make([]string, 0)

	unsupported, ok := providerUnsupportedParams[strings.ToLower(f.providerType)]
	if !ok {
		return req, nil
	}

	for key, value := range req {
		if unsupported[key] {
			strippedParams = append(strippedParams, key)
		} else {
			filtered[key] = value
		}
	}

	return filtered, strippedParams
}

func DetectProviderType(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "openai.com") || strings.Contains(baseURL, "api.openai.com"):
		return "openai"
	case strings.Contains(baseURL, "groq.com"):
		return "groq"
	case strings.Contains(baseURL, "ollama") || strings.Contains(baseURL, "11434"):
		return "ollama"
	case strings.Contains(baseURL, "azure") || strings.Contains(baseURL, "openai.azure"):
		return "azure"
	default:
		return "unknown"
	}
}

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type ChatCompletionRequest struct {
	Model       string                 `json:"model"`
	Messages    []Message              `json:"messages"`
	Stream      bool                   `json:"stream,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	MaxTokens   *int                   `json:"max_tokens,omitempty"`
	Extra       map[string]interface{} `json:"-"`
}

func ParseRequest(raw map[string]interface{}) (*ChatCompletionRequest, error) {
	req := &ChatCompletionRequest{
		Extra: make(map[string]interface{}),
	}

	reqBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(reqBytes, req); err != nil {
		return nil, err
	}

	for key, value := range raw {
		switch key {
		case "model", "messages", "stream", "temperature", "max_tokens":
		default:
			req.Extra[key] = value
		}
	}

	return req, nil
}

func (req *ChatCompletionRequest) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}

	if req.Stream {
		result["stream"] = true
	}
	if req.Temperature != nil {
		result["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		result["max_tokens"] = *req.MaxTokens
	}

	for key, value := range req.Extra {
		result[key] = value
	}

	return result
}
