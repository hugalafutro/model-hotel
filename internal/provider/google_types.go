package provider

// GoogleModel represents a model from Google AI Studio native /v1beta/models response.
type GoogleModel struct {
	Name                       string   `json:"name"`
	Version                    string   `json:"version,omitempty"`
	DisplayName                string   `json:"displayName,omitempty"`
	Description                string   `json:"description,omitempty"`
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	Temperature                float64  `json:"temperature,omitempty"`
	TopP                       float64  `json:"topP,omitempty"`
	TopK                       int      `json:"topK,omitempty"`
	MaxTemperature             float64  `json:"maxTemperature,omitempty"`
	Thinking                   bool     `json:"thinking,omitempty"`
}

// GoogleModelsResponse is the response from Google AI Studio models endpoint.
type GoogleModelsResponse struct {
	Models []GoogleModel `json:"models"`
}

// GoogleOpenAIModel is a single model from the OpenAI-compat /v1beta/openai/models response.
type GoogleOpenAIModel struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name"`
}

// GoogleOpenAIModelsResponse is the response from Google OpenAI-compatible models endpoint.
type GoogleOpenAIModelsResponse struct {
	Object string              `json:"object"`
	Data   []GoogleOpenAIModel `json:"data"`
}
