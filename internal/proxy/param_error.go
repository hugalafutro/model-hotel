package proxy

import (
	"encoding/json"
	"strings"
	"sync"
)

// providerUnsupportedParams lists OpenAI Chat Completions parameters that are
// universally unsupported (cause 400 errors) per provider type. These are
// preemptively stripped from requests to avoid a wasted round-trip.
// Sources: official provider docs + empirical testing.
var providerUnsupportedParams = map[string][]string{
	"anthropic": {
		"top_p",             // deprecated on all current Anthropic models
		"frequency_penalty", // Anthropic uses a single penalties param, not separate freq/presence
		"presence_penalty",  // Anthropic uses a single penalties param, not separate freq/presence
		"min_p",             // not part of Anthropic API
		"reasoning_effort",  // not supported by Anthropic API
	},
	"google": {
		"frequency_penalty", // not supported on Gemini OpenAI-compat endpoint
		"presence_penalty",  // not supported on Gemini OpenAI-compat endpoint
		"logprobs",          // not supported
		"top_logprobs",      // not supported
		"min_p",             // not supported on Gemini API
		"top_k",             // Gemini top_k ≠ OpenAI top_k; causes unexpected behavior
		"reasoning_effort",  // not supported on Gemini API
	},
	"cohere": {
		"logprobs",         // not supported
		"top_logprobs",     // not supported
		"min_p",            // not supported
		"top_k",            // Cohere uses 'k' differently; not recommended
		"reasoning_effort", // not supported by Cohere API
	},
	"openai": {
		"min_p", // not part of OpenAI API
		"top_k", // not part of OpenAI API
	},
	"deepseek": {
		"min_p",            // not supported by DeepSeek API
		"top_k",            // not supported by DeepSeek API
		"reasoning_effort", // not supported by DeepSeek API
	},
	"xai": {
		"min_p", // not supported by xAI API
		"top_k", // not supported by xAI API
	},
	"ollama": {
		"reasoning_effort", // not supported by Ollama
	},
	"ollama-cloud": {
		"reasoning_effort", // not supported by Ollama Cloud
	},
	"koboldcpp": {
		"reasoning_effort", // not supported by KoboldCpp
	},
	"lmstudio": {
		"reasoning_effort", // not supported by LM Studio
	},
	"nanogpt": {
		"reasoning_effort", // not supported by NanoGPT
	},
	"zai-coding": {
		"reasoning_effort", // not supported by z.ai Coding
	},
	"openrouter": {
		"reasoning_effort", // not supported by OpenRouter
	},
	"opencode-zen": {
		"reasoning_effort", // not supported
	},
	"opencode-go": {
		"reasoning_effort", // not supported
	},
}

// getCachedRejectedParams returns params known to be rejected for a provider+model,
// learned from previous 400 responses.
// NOTE: Values are stored as *map[string]bool in sync.Map to support CompareAndSwap
// (maps are not comparable, so pointers are required).
func getCachedRejectedParams(cache *sync.Map, cacheKey string) map[string]bool {
	if v, ok := cache.Load(cacheKey); ok {
		if ptr, ok := v.(*map[string]bool); ok {
			return *ptr
		}
		// Fallback for legacy map[string]bool values (pre-pointer migration)
		if m, ok := v.(map[string]bool); ok {
			return m
		}
	}
	return nil
}

// getCachedRenames returns param renames known to be required for a
// provider+model, learned from previous 400 responses (e.g. an OpenAI gpt-5/o
// model that rejects max_tokens and demands max_completion_tokens).
// NOTE: Values are stored as *map[string]string in sync.Map to support
// CompareAndSwap (maps are not comparable, so pointers are required).
func getCachedRenames(cache *sync.Map, cacheKey string) map[string]string {
	if v, ok := cache.Load(cacheKey); ok {
		if ptr, ok := v.(*map[string]string); ok {
			return *ptr
		}
		// Fallback for legacy map[string]string values (pre-pointer migration)
		if m, ok := v.(map[string]string); ok {
			return m
		}
	}
	return nil
}

// parseProviderParamRename parses 400 error bodies for params the upstream wants
// renamed rather than dropped. Unlike a rejected param (which we strip), a
// renamed param carries a value we must preserve under the new name — stripping
// it would silently discard the caller's intent (e.g. their token budget).
//
// The only case in the wild today: OpenAI's gpt-5 and o-series models reject the
// classic max_tokens and require max_completion_tokens
// ("Unsupported parameter: 'max_tokens' is not supported with this model. Use
// 'max_completion_tokens' instead."). These reach model-hotel directly via the
// openai provider and indirectly via passthrough gateways (e.g. OpenCode Zen).
// The replacement token max_completion_tokens has no other meaning in an error
// message, so its mere presence is a safe, provider-agnostic signal to rename.
func parseProviderParamRename(body []byte) map[string]string {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) != nil {
		return nil
	}
	msg := errResp.Error.Message
	renames := make(map[string]string)

	// max_tokens -> max_completion_tokens (OpenAI gpt-5/o-series deprecation).
	if strings.Contains(msg, "max_completion_tokens") {
		renames["max_tokens"] = "max_completion_tokens"
	}

	if len(renames) == 0 {
		return nil
	}
	return renames
}

// parseProviderParamError parses 400 error bodies for rejected sampling/param names.
// Any LLM API mentioning these param names in a 400 error can only be referring
// to the request parameter — there is no other meaning in this context.
// This works universally across all providers, not just Anthropic.
func parseProviderParamError(body []byte) map[string]bool {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) != nil {
		return nil
	}
	msg := errResp.Error.Message
	rejected := make(map[string]bool)

	// "cannot both be specified" — strip top_p, keep temperature
	if strings.Contains(msg, "cannot both be specified") {
		rejected["top_p"] = true
	}
	// Known sampling/optional params that providers commonly reject.
	// We match against backtick-wrapped names (e.g. `top_p`) and quote-wrapped
	// names (e.g. "top_p") to avoid false positives from substring matching.
	// Short/common words like "n", "stop", "seed" are NOT matched loosely
	// because they appear in many unrelated error messages.
	matchParams := []string{
		"temperature", "top_p", "top_k", "top_a",
		"frequency_penalty", "presence_penalty",
		"logprobs", "top_logprobs",
		"max_tokens", "stream_options", "reasoning_effort",
	}
	for _, p := range matchParams {
		// Match backtick-wrapped: `param` or quote-wrapped: "param"
		if strings.Contains(msg, "`"+p+"`") || strings.Contains(msg, "\""+p+"\"") {
			rejected[p] = true
		}
	}
	// "stop", "n", "seed" are too common as substrings — only match when
	// explicitly quoted or backticked in the error message.
	for _, p := range []string{"stop", "n", "seed"} {
		if strings.Contains(msg, "`"+p+"`") || strings.Contains(msg, "\""+p+"\"") {
			rejected[p] = true
		}
	}
	// chat_template_args is a non-standard field model-hotel injects for some
	// OpenCode providers (see InjectProviderParams). Strict upstream backends
	// reject it with varying message formats and quote styles, e.g. vLLM's
	// "Extra inputs are not permitted, field: 'chat_template_args'" (single
	// quotes) or OpenAI's "Unrecognized request argument: chat_template_args"
	// (bare). The token is specific enough that a bare substring match is safe —
	// it has no other meaning in an error message. Stripping it on retry trades
	// reasoning output for a successful completion on models that reject it.
	if strings.Contains(msg, "chat_template_args") {
		rejected["chat_template_args"] = true
	}
	// Also catch any top_{single_letter} variant when backtick/quote-wrapped
	if idx := strings.Index(msg, "`top_"); idx >= 0 && idx+7 <= len(msg) {
		c := msg[idx+5]
		if c >= 'a' && c <= 'z' && msg[idx+6] == '`' {
			rejected[msg[idx+1:idx+6]] = true
		}
	}
	if idx := strings.Index(msg, "\"top_"); idx >= 0 && idx+7 <= len(msg) {
		c := msg[idx+5]
		if c >= 'a' && c <= 'z' && msg[idx+6] == '"' {
			rejected[msg[idx+1:idx+6]] = true
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	return rejected
}
