package proxy

import (
	"encoding/json"
)

type VisionHandler struct{}

func NewVisionHandler() *VisionHandler {
	return &VisionHandler{}
}

type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url,omitempty"`
	} `json:"image_url,omitempty"`
}

type VisionMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

func (h *VisionHandler) DetectVisionMessages(messages []Message) bool {
	for _, msg := range messages {
		if contentParts, ok := msg.Content.([]interface{}); ok {
			for _, part := range contentParts {
				if partMap, ok := part.(map[string]interface{}); ok {
					if typeStr, ok := partMap["type"].(string); ok && typeStr == "image_url" {
						return true
					}
				}
			}
		}
	}
	return false
}

func (h *VisionHandler) ValidateVisionSupport(capabilities string) bool {
	if capabilities == "" {
		return false
	}

	var caps struct {
		Vision bool `json:"vision"`
	}
	if err := json.Unmarshal([]byte(capabilities), &caps); err != nil {
		return false
	}

	return caps.Vision
}

func (h *VisionHandler) FormatMessages(messages []Message) ([]interface{}, error) {
	formatted := make([]interface{}, len(messages))

	for i, msg := range messages {
		if contentParts, ok := msg.Content.([]interface{}); ok {
			formatted[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": contentParts,
			}
		} else {
			formatted[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			}
		}
	}

	return formatted, nil
}
