package proxy

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

func extractStreamingUsage(data string) *Usage {
	scanner := bufio.NewScanner(strings.NewReader(data))
	var lastUsage *Usage
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Usage *Usage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) == nil && chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}
	return lastUsage
}

func generateRequestHash() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}