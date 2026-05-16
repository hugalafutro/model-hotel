package simulator

import (
	"slices"
	"testing"
	"time"
)

func TestModelInfoKey(t *testing.T) {
	m := ModelInfo{Provider: "NanoGPT", ID: "deepseek-chat"}
	if got := m.Key(); got != "NanoGPT/deepseek-chat" {
		t.Errorf("Key() = %q, want %q", got, "NanoGPT/deepseek-chat")
	}
}

func TestPickModel_Basic(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
			{Provider: "Ollama Cloud", ID: "gemma3:4b"},
		},
	})

	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil, expected a model")
	}
	key := model.Key()
	if key != "NanoGPT/deepseek-chat" && key != "Ollama Cloud/gemma3:4b" {
		t.Errorf("PickModel() returned unexpected model %q", key)
	}
}

func TestPickModel_SkipsDead(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
			{Provider: "Ollama Cloud", ID: "gemma3:4b"},
		},
	})

	s.MarkDead("NanoGPT/deepseek-chat")

	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil with one available model")
	}
	if model.Key() != "Ollama Cloud/gemma3:4b" {
		t.Errorf("PickModel() = %q, want Ollama Cloud/gemma3:4b", model.Key())
	}
}

func TestPickModel_SkipsCooldown(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
			{Provider: "Ollama Cloud", ID: "gemma3:4b"},
		},
	})

	s.MarkCooldown("NanoGPT/deepseek-chat", 10*time.Minute)

	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil with one available model")
	}
	if model.Key() != "Ollama Cloud/gemma3:4b" {
		t.Errorf("PickModel() = %q, want Ollama Cloud/gemma3:4b", model.Key())
	}
}

func TestPickModel_ExpiredCooldownAvailable(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
		},
	})

	// Set cooldown that already expired
	s.mu.Lock()
	s.cooldowns["NanoGPT/deepseek-chat"] = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()

	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil with expired cooldown")
	}
	if model.Key() != "NanoGPT/deepseek-chat" {
		t.Errorf("PickModel() = %q, want NanoGPT/deepseek-chat", model.Key())
	}
}

func TestPickModel_AllDead(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
			{Provider: "Ollama Cloud", ID: "gemma3:4b"},
		},
	})

	s.MarkDead("NanoGPT/deepseek-chat")
	s.MarkDead("Ollama Cloud/gemma3:4b")

	model := s.PickModel()
	if model != nil {
		t.Errorf("PickModel() = %q, want nil when all models dead", model.Key())
	}
}

func TestPickModel_AllCooldown(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "deepseek-chat"},
		},
	})

	s.MarkCooldown("NanoGPT/deepseek-chat", 10*time.Minute)

	model := s.PickModel()
	if model != nil {
		t.Errorf("PickModel() = %q, want nil when all models on cooldown", model.Key())
	}
}

func TestPickModel_EmptyModelList(t *testing.T) {
	s := New(Config{Models: []ModelInfo{}})

	model := s.PickModel()
	if model != nil {
		t.Errorf("PickModel() = %q, want nil with empty model list", model.Key())
	}
}

func TestRandomConvDuration(t *testing.T) {
	s := New(Config{
		ConvMin: 1 * time.Minute,
		ConvMax: 3 * time.Minute,
	})

	for range 100 {
		d := s.RandomConvDuration()
		if d < 1*time.Minute || d > 3*time.Minute {
			t.Errorf("RandomConvDuration() = %v, want between 1m and 3m", d)
		}
	}
}

func TestRandomConvDuration_MinEqualsMax(t *testing.T) {
	s := New(Config{
		ConvMin: 2 * time.Minute,
		ConvMax: 2 * time.Minute,
	})

	d := s.RandomConvDuration()
	if d != 2*time.Minute {
		t.Errorf("RandomConvDuration() = %v, want 2m when min==max", d)
	}
}

func TestRandomPrompt(t *testing.T) {
	s := New(Config{})

	for range 50 {
		p := s.RandomPrompt()
		if !slices.Contains(prompts, p) {
			t.Errorf("RandomPrompt() = %q, not found in prompts pool", p)
		}
	}
}

func TestRandomMaxTokens(t *testing.T) {
	s := New(Config{
		MaxTokensMin: 10,
		MaxTokensMax: 500,
	})

	for range 100 {
		n := s.RandomMaxTokens()
		if n < 10 || n > 500 {
			t.Errorf("RandomMaxTokens() = %d, want between 10 and 500", n)
		}
	}
}

func TestRandomMaxTokens_MinEqualsMax(t *testing.T) {
	s := New(Config{
		MaxTokensMin: 150,
		MaxTokensMax: 150,
	})

	n := s.RandomMaxTokens()
	if n != 150 {
		t.Errorf("RandomMaxTokens() = %d, want 150 when min==max", n)
	}
}

func TestRandomJitter(t *testing.T) {
	s := New(Config{Jitter: true})

	for range 50 {
		j := s.RandomJitter()
		if j < 2*time.Second || j > 8*time.Second {
			t.Errorf("RandomJitter() = %v, want between 2s and 8s", j)
		}
	}
}

func TestRandomJitter_Disabled(t *testing.T) {
	s := New(Config{Jitter: false})

	j := s.RandomJitter()
	if j != 0 {
		t.Errorf("RandomJitter() = %v, want 0 when jitter disabled", j)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		code   int
		action ErrorAction
	}{
		{429, ActionCooldown},
		{503, ActionCooldown},
		{502, ActionCooldown},
		{400, ActionDead},
		{404, ActionDead},
		{422, ActionDead},
		{500, ActionRetry},
		{401, ActionRetry},
		{408, ActionRetry},
	}

	for _, tt := range tests {
		got := ClassifyError(tt.code)
		if got != tt.action {
			t.Errorf("ClassifyError(%d) = %v, want %v", tt.code, got, tt.action)
		}
	}
}

func TestMarkDead(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "a"},
			{Provider: "NanoGPT", ID: "b"},
		},
	})

	s.MarkDead("NanoGPT/a")

	if s.DeadCount() != 1 {
		t.Errorf("DeadCount() = %d, want 1", s.DeadCount())
	}

	// Only "b" should be available
	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil")
	}
	if model.Key() != "NanoGPT/b" {
		t.Errorf("PickModel() = %q, want NanoGPT/b", model.Key())
	}
}

func TestMarkCooldown(t *testing.T) {
	s := New(Config{
		Models: []ModelInfo{
			{Provider: "NanoGPT", ID: "a"},
			{Provider: "NanoGPT", ID: "b"},
		},
	})

	s.MarkCooldown("NanoGPT/a", 5*time.Minute)

	// Only "b" should be available
	model := s.PickModel()
	if model == nil {
		t.Fatal("PickModel() returned nil")
	}
	if model.Key() != "NanoGPT/b" {
		t.Errorf("PickModel() = %q, want NanoGPT/b", model.Key())
	}
}

func TestStats(t *testing.T) {
	s := New(Config{})

	s.Stats.RecordRequest("NanoGPT/a")
	s.Stats.RecordRequest("NanoGPT/a")
	s.Stats.RecordRequest("NanoGPT/b")
	s.Stats.RecordError()

	reqs, errs, models := s.Stats.Snapshot()
	if reqs != 3 {
		t.Errorf("TotalRequests = %d, want 3", reqs)
	}
	if errs != 1 {
		t.Errorf("TotalErrors = %d, want 1", errs)
	}
	if models["NanoGPT/a"] != 2 {
		t.Errorf("ModelsUsed[NanoGPT/a] = %d, want 2", models["NanoGPT/a"])
	}
	if models["NanoGPT/b"] != 1 {
		t.Errorf("ModelsUsed[NanoGPT/b] = %d, want 1", models["NanoGPT/b"])
	}
}

func TestStats_SnapshotIsCopy(t *testing.T) {
	s := New(Config{})
	s.Stats.RecordRequest("NanoGPT/a")

	_, _, models := s.Stats.Snapshot()
	models["NanoGPT/a"] = 999

	_, _, models2 := s.Stats.Snapshot()
	if models2["NanoGPT/a"] == 999 {
		t.Error("Snapshot should return a copy, not a reference")
	}
}
