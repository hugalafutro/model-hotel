package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/llm-proxy/tools/stress-test/metrics"
)

// ProxyClient sends chat completion requests through the proxy.
type ProxyClient struct {
	baseURL string
	client  *http.Client
}

// NewProxyClient creates a client that sends requests to the proxy.
// The timeout should be generous for high-concurrency scenarios.
func NewProxyClient(proxyURL string, timeout time.Duration) *ProxyClient {
	return &ProxyClient{
		baseURL: proxyURL,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// SendChatCompletion sends a single chat completion request through the proxy
// and returns the measured result including TTFT for streaming requests.
func (p *ProxyClient) SendChatCompletion(virtualKey, model string, streaming bool) metrics.Result {
	start := time.Now()

	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello, this is a stress test message."},
		},
		"stream": streaming,
	}
	if streaming {
		reqBody["stream_options"] = map[string]interface{}{
			"include_usage": true,
		}
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return metrics.Result{
			Duration:   time.Since(start),
			StatusCode: 0,
			Error:      fmt.Sprintf("create request: %v", err),
		}
	}
	req.Header.Set("Authorization", "Bearer "+virtualKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return metrics.Result{
			Duration:   time.Since(start),
			StatusCode: 0,
			Error:      fmt.Sprintf("do request: %v", err),
		}
	}
	defer resp.Body.Close()

	if streaming {
		return p.readStreamingResponse(resp, start)
	}
	return p.readNonStreamingResponse(resp, start)
}

func (p *ProxyClient) readStreamingResponse(resp *http.Response, start time.Time) metrics.Result {
	result := metrics.Result{
		StatusCode: resp.StatusCode,
	}

	// Even on error status codes, try to read the body for error info
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		// Check for rate limit headers
		if resp.StatusCode == 429 {
			retryAfter := resp.Header.Get("Retry-After")
			result.Error = fmt.Sprintf("HTTP 429 (rate limited, Retry-After: %s)", retryAfter)
		}
		return result
	}

	// Read SSE stream line by line to measure TTFT
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ttftMeasured := false
	var lastLine string

	for scanner.Scan() {
		line := scanner.Text()

		// Measure TTFT on the first data line
		if !ttftMeasured && strings.HasPrefix(line, "data: ") {
			result.TTFT = time.Since(start)
			ttftMeasured = true
		}
		lastLine = line

		// Check for stream end
		if line == "data: [DONE]" {
			break
		}
	}

	result.Duration = time.Since(start)

	if err := scanner.Err(); err != nil {
		if result.Error == "" {
			result.Error = fmt.Sprintf("stream read error: %v", err)
		}
	}

	// If we never got a data line, that's an error
	if !ttftMeasured && lastLine == "" {
		result.Error = "no SSE data received from upstream"
	}

	return result
}

func (p *ProxyClient) readNonStreamingResponse(resp *http.Response, start time.Time) metrics.Result {
	result := metrics.Result{
		Duration:   time.Since(start),
		StatusCode: resp.StatusCode,
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		result.Error = fmt.Sprintf("read body: %v", err)
		return result
	}

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		if resp.StatusCode == 429 {
			retryAfter := resp.Header.Get("Retry-After")
			result.Error = fmt.Sprintf("HTTP 429 (rate limited, Retry-After: %s)", retryAfter)
		}
		return result
	}

	// Parse just enough to verify it's a valid response
	var chatResp struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Usage  struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		result.Error = fmt.Sprintf("invalid response JSON: %v", err)
	}

	return result
}
