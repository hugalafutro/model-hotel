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
