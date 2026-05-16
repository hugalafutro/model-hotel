package simulator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds simulation parameters.
type Config struct {
	URL          string
	Key          string
	Users        int
	Duration     time.Duration
	ConvMin      time.Duration
	ConvMax      time.Duration
	Streaming    bool
	MaxTokensMin int
	MaxTokensMax int
	Jitter       bool
	Models       []ModelInfo
}

// ModelInfo represents a model available for simulation.
type ModelInfo struct {
	Provider string
	ID       string
}

// Key returns the fully qualified model key used in API requests.
func (m ModelInfo) Key() string {
	return m.Provider + "/" + m.ID
}

// ErrorAction determines how to handle a failed request.
type ErrorAction int

const (
	ActionCooldown ErrorAction = iota // Temporary: skip model for a while
	ActionDead                        // Permanent: model is broken
	ActionRetry                       // Transient: just retry
)

// ClassifyError returns the action to take based on HTTP status code.
func ClassifyError(statusCode int) ErrorAction {
	switch {
	case statusCode == 429 || statusCode == 503 || statusCode == 502:
		return ActionCooldown
	case statusCode == 400 || statusCode == 404 || statusCode == 422:
		return ActionDead
	default:
		return ActionRetry
	}
}

// Stats tracks simulation metrics.
type Stats struct {
	TotalRequests int64
	TotalErrors   int64
	ModelsUsed    map[string]int
	mu            sync.Mutex
}

// RecordRequest increments the request counter for a model.
func (s *Stats) RecordRequest(modelKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalRequests++
	if s.ModelsUsed == nil {
		s.ModelsUsed = make(map[string]int)
	}
	s.ModelsUsed[modelKey]++
}

// RecordError increments the error counter.
func (s *Stats) RecordError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalErrors++
}

// Snapshot returns a copy of current stats.
func (s *Stats) Snapshot() (requests int64, errors int64, models map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	models = make(map[string]int, len(s.ModelsUsed))
	for k, v := range s.ModelsUsed {
		models[k] = v
	}
	return s.TotalRequests, s.TotalErrors, models
}

// Simulator orchestrates concurrent user simulations.
type Simulator struct {
	Config    Config
	Stats     Stats
	dead      map[string]time.Time
	cooldowns map[string]time.Time
	mu        sync.Mutex
	client    *http.Client
}

// New creates a Simulator with the given config.
func New(cfg Config) *Simulator {
	return &Simulator{
		Config:    cfg,
		dead:      make(map[string]time.Time),
		cooldowns: make(map[string]time.Time),
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

// PickModel selects a random available model, skipping dead and cooled-down ones.
func (s *Simulator) PickModel() *ModelInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var available []ModelInfo
	for _, m := range s.Config.Models {
		key := m.Key()
		if _, isDead := s.dead[key]; isDead {
			continue
		}
		if expiry, ok := s.cooldowns[key]; ok {
			if now.Before(expiry) {
				continue
			}
			delete(s.cooldowns, key)
		}
		available = append(available, m)
	}

	if len(available) == 0 {
		return nil
	}

	idx := rand.IntN(len(available))
	return &available[idx]
}

// MarkDead marks a model as permanently failed.
func (s *Simulator) MarkDead(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dead[key] = time.Now()
}

// MarkCooldown marks a model as temporarily unavailable.
func (s *Simulator) MarkCooldown(key string, dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cooldowns[key] = time.Now().Add(dur)
}

// DeadCount returns the number of permanently dead models.
func (s *Simulator) DeadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.dead)
}

// RandomConvDuration returns a random duration between ConvMin and ConvMax.
func (s *Simulator) RandomConvDuration() time.Duration {
	min := s.Config.ConvMin
	max := s.Config.ConvMax
	if min >= max {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min)))
}

// RandomPrompt returns a random prompt from the pool.
func (s *Simulator) RandomPrompt() string {
	return prompts[rand.IntN(len(prompts))]
}

// RandomJitter returns a random delay for between-turn pauses (2-8s).
func (s *Simulator) RandomJitter() time.Duration {
	if !s.Config.Jitter {
		return 0
	}
	return time.Duration(2+rand.IntN(7)) * time.Second
}

// RandomMaxTokens returns a random token count between MaxTokensMin and MaxTokensMax.
func (s *Simulator) RandomMaxTokens() int {
	min := s.Config.MaxTokensMin
	max := s.Config.MaxTokensMax
	if min >= max {
		return min
	}
	return min + rand.IntN(max-min+1)
}

// RunUser simulates a single user's behavior until the context is cancelled.
func (s *Simulator) RunUser(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		model := s.PickModel()
		if model == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}

		convDur := s.RandomConvDuration()
		s.runConversation(ctx, id, model, convDur)

		// Pause between conversations (5-15s)
		pause := time.Duration(5+rand.IntN(11)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
	}
}

func (s *Simulator) runConversation(ctx context.Context, userID int, model *ModelInfo, dur time.Duration) {
	deadline := time.Now().Add(dur)
	modelKey := model.Key()
	messages := []map[string]string{
		{"role": "user", "content": s.RandomPrompt()},
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		statusCode, err := s.sendCompletion(ctx, modelKey, messages)
		if err != nil {
			s.Stats.RecordError()
			action := ClassifyError(statusCode)
			switch action {
			case ActionDead:
				s.MarkDead(modelKey)
				return
			case ActionCooldown:
				s.MarkCooldown(modelKey, 5*time.Minute)
				return
			case ActionRetry:
				// Back off briefly and continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		s.Stats.RecordRequest(modelKey)

		// Append assistant placeholder and next user message
		messages = append(messages,
			map[string]string{"role": "assistant", "content": "..."},
			map[string]string{"role": "user", "content": s.RandomPrompt()},
		)

		// Jitter between turns
		jitter := s.RandomJitter()
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}
	}
}

func (s *Simulator) sendCompletion(ctx context.Context, model string, messages []map[string]string) (int, error) {
	body := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"stream":     s.Config.Streaming,
		"max_tokens": s.RandomMaxTokens(),
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.Config.URL+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Config.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if s.Config.Streaming {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "data: [DONE]" {
				break
			}
			if strings.HasPrefix(line, "data: ") {
				// Consume stream, don't parse
			}
		}
	} else {
		io.Copy(io.Discard, resp.Body)
	}

	return resp.StatusCode, nil
}

// prompts is a pool of varied conversation starters.
var prompts = []string{
	"Explain quantum computing in simple terms.",
	"Write a short poem about rain.",
	"What are the best practices for REST APIs?",
	"Help me debug a Python function that's returning None.",
	"Summarize the history of the internet in 3 sentences.",
	"What's the difference between TCP and UDP?",
	"Give me 5 tips for better time management.",
	"Explain Docker containers to a beginner.",
	"What are the pros and cons of microservices?",
	"Write a haiku about programming.",
	"How does garbage collection work in Go?",
	"What are the SOLID principles? Give a brief example of each.",
	"Explain the CAP theorem in distributed systems.",
	"What's the difference between SQL and NoSQL databases?",
	"Help me write a regex that matches email addresses.",
	"What are common design patterns in software engineering?",
	"Explain how DNS resolution works step by step.",
	"What are the benefits of functional programming?",
	"Compare WebSockets and Server-Sent Events.",
	"Give me a brief overview of the Rust programming language.",
}
