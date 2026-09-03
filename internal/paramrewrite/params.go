// Package paramrewrite is the single shared implementation of model-hotel's
// request/response parameter self-healing: rewriting an OpenAI-style chat
// completion body for a specific provider (model rename, stream_options and
// provider-specific injection, universal + learned param stripping, learned
// param renaming) and parsing upstream 400 bodies to learn which params a
// provider rejects or wants renamed. Both the streaming proxy failover loop and
// the admin "Test model" probe build their upstream bodies and self-heal 400s
// through this package so the two paths can never drift.
package paramrewrite

import (
	"encoding/json"
	"strings"
	"sync"
)

// anthropicUnsupportedParams are the params no Anthropic endpoint accepts as
// itself, over the OpenAI-compat route or as a translated Messages body.
var anthropicUnsupportedParams = []string{
	"top_p",             // deprecated on all current Anthropic models
	"frequency_penalty", // Anthropic uses a single penalties param, not separate freq/presence
	"presence_penalty",  // Anthropic uses a single penalties param, not separate freq/presence
	"min_p",             // not part of Anthropic API
	"reasoning_effort",  // not supported by Anthropic API
}

// anthropicMessagesUnsupportedParams is the same list without reasoning_effort,
// which the Messages type translates rather than forwards. Derived from the list
// above rather than restated, so a param added there is stripped from both.
var anthropicMessagesUnsupportedParams = withoutParam(anthropicUnsupportedParams, "reasoning_effort")

// withoutParam copies a strip list minus one entry.
func withoutParam(params []string, drop string) []string {
	out := make([]string, 0, len(params))
	for _, p := range params {
		if p != drop {
			out = append(out, p)
		}
	}
	return out
}

// ProviderUnsupportedParams lists OpenAI Chat Completions parameters that are
// universally unsupported (cause 400 errors) per provider type. These are
// preemptively stripped from requests to avoid a wasted round-trip.
// Sources: official provider docs + empirical testing.
var ProviderUnsupportedParams = map[string][]string{
	"anthropic": anthropicUnsupportedParams,
	// The Messages type strips one param fewer: reasoning_effort survives, for
	// internal/anthropicegress to turn into an Anthropic thinking request. Every
	// chat request to this type is translated, so the param never reaches an
	// upstream as itself, and the translator asks in the shape the model wants
	// (learned from a 400 the first time; see proxy/anthropic_thinking_retry.go).
	//
	// "anthropic" keeps stripping it, deliberately. Its default route is
	// Anthropic's OpenAI-compat endpoint, which has no thinking control at all,
	// and honouring the param only on the rarer document-egress requests would
	// make one provider reason and the next not, for a difference the caller
	// cannot see. That type reasons by model default, uniformly.
	"anthropic-messages": anthropicMessagesUnsupportedParams,
	"google": {
		"frequency_penalty", // not supported on Gemini OpenAI-compat endpoint
		"presence_penalty",  // not supported on Gemini OpenAI-compat endpoint
		"logprobs",          // not supported
		"top_logprobs",      // not supported
		"min_p",             // not supported on Gemini API
		"top_k",             // Gemini top_k ≠ OpenAI top_k; causes unexpected behavior
		"reasoning_effort",  // not supported on Gemini API
		"reasoning",         // rejected outright: Unknown name "reasoning": Cannot find field
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

// CachedRejectedParams returns params known to be rejected for a provider+model,
// learned from previous 400 responses.
// NOTE: Values are stored as *map[string]bool in sync.Map to support CompareAndSwap
// (maps are not comparable, so pointers are required).
func CachedRejectedParams(cache *sync.Map, cacheKey string) map[string]bool {
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

// cachedRenames returns param renames known to be required for a
// provider+model, learned from previous 400 responses (e.g. an OpenAI gpt-5/o
// model that rejects max_tokens and demands max_completion_tokens).
// NOTE: Values are stored as *map[string]string in sync.Map to support
// CompareAndSwap (maps are not comparable, so pointers are required).
func cachedRenames(cache *sync.Map, cacheKey string) map[string]string {
	if v, ok := cache.Load(cacheKey); ok {
		if ptr, ok := v.(*map[string]string); ok {
			return *ptr
		}
	}
	return nil
}

// providerErrorMessage extracts the human-readable message from a provider's
// error body. Most providers return a bare object ({"error":{"message":...}}),
// but Google AI Studio wraps the same shape in a one-element array
// ([{"error":{...}}]). Reading only the object form leaves Google's messages
// unparsed, so a rejected param it names can never be learned or stripped.
func providerErrorMessage(body []byte) string {
	type errEnvelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	var obj errEnvelope
	if json.Unmarshal(body, &obj) == nil {
		return obj.Error.Message
	}
	var arr []errEnvelope
	if json.Unmarshal(body, &arr) == nil {
		// Joined, not first-wins: a body naming two rejected fields must teach
		// both in one pass rather than costing a 400 round-trip each.
		msgs := make([]string, 0, len(arr))
		for _, e := range arr {
			if e.Error.Message != "" {
				msgs = append(msgs, e.Error.Message)
			}
		}
		return strings.Join(msgs, "; ")
	}
	return ""
}

// ParseProviderParamRename parses 400 error bodies for params the upstream wants
// renamed rather than dropped. Unlike a rejected param (which we strip), a
// renamed param carries a value we must preserve under the new name — stripping
// it would silently discard the caller's intent (e.g. their token budget).
//
// The only case in the wild today: OpenAI's gpt-5 and o-series models reject the
// classic max_tokens and require max_completion_tokens
// ("Unsupported parameter: 'max_tokens' is not supported with this model. Use
// 'max_completion_tokens' instead."). These reach model-hotel directly via the
// openai provider and indirectly via passthrough gateways (e.g. OpenCode Zen).
func ParseProviderParamRename(body []byte) map[string]string {
	msg := strings.ToLower(providerErrorMessage(body))
	if msg == "" {
		return nil
	}
	renames := make(map[string]string)

	// max_tokens -> max_completion_tokens (OpenAI gpt-5/o-series deprecation).
	// Match the full directive, not just the presence of the replacement token:
	// require the old name, the new name, AND the "use X instead" wording. This
	// excludes value-validation errors that merely mention max_completion_tokens
	// (e.g. "max_completion_tokens must not exceed 4096"), which would otherwise
	// poison the rename cache and force every max_tokens request to be renamed —
	// breaking a sibling model on the same key that natively accepts max_tokens.
	if strings.Contains(msg, "max_tokens") &&
		strings.Contains(msg, "max_completion_tokens") &&
		strings.Contains(msg, "instead") {
		renames["max_tokens"] = "max_completion_tokens"
	}

	if len(renames) == 0 {
		return nil
	}
	return renames
}

// paramQuoteChars are the quote styles providers wrap a parameter name in when
// they name it in a 400. Anchoring on a quote is what keeps short names like
// "n" and "stop" from matching unrelated prose.
//
// The single quote was the gap: OpenAI names the offending param that way in
// its value-validation errors ("Unsupported value: 'temperature' does not
// support 0 with this model. Only the default (1) value is supported."), so
// every gpt-5-family request carrying temperature:0 failed with a 400 that
// model-hotel could have healed by stripping the param and retrying.
var paramQuoteChars = []byte{'`', '"', '\''}

// paramIsQuoted reports whether the error message names param inside one of the
// recognised quote styles.
func paramIsQuoted(msg, param string) bool {
	for _, q := range paramQuoteChars {
		if strings.Contains(msg, string(q)+param+string(q)) {
			return true
		}
	}
	return false
}

// ParseProviderParamError parses 400 error bodies for rejected sampling/param names.
// Any LLM API mentioning these param names in a 400 error can only be referring
// to the request parameter — there is no other meaning in this context.
// This works universally across all providers, not just Anthropic.
func ParseProviderParamError(body []byte) map[string]bool {
	msg := providerErrorMessage(body)
	if msg == "" {
		return nil
	}
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
		// "reasoning" is an OpenAI-dialect field callers send that Google AI
		// Studio rejects outright ("Unknown name \"reasoning\": Cannot find
		// field."), failing the whole request. The quote/backtick anchoring
		// below keeps it from matching "reasoning_effort", which is a
		// separate param with its own entry.
		"reasoning",
	}
	for _, p := range matchParams {
		if paramIsQuoted(msg, p) {
			rejected[p] = true
		}
	}
	// "stop", "n", "seed" are too common as substrings — only match when
	// explicitly quoted or backticked in the error message.
	for _, p := range []string{"stop", "n", "seed"} {
		if paramIsQuoted(msg, p) {
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
	// A refusal about the response format is learned as the schema fallback
	// rather than a strip: dropping response_format would hand the caller
	// prose where they asked for JSON, while JSON mode with the schema in the
	// prompt still answers the request. See downgradeJSONSchema.
	if refusesJSONSchema(msg) {
		rejected[SchemaFallbackKey] = true
	}
	// Also catch any top_{single_letter} variant when quoted in any style.
	for _, q := range paramQuoteChars {
		if idx := strings.Index(msg, string(q)+"top_"); idx >= 0 && idx+7 <= len(msg) {
			c := msg[idx+5]
			if c >= 'a' && c <= 'z' && msg[idx+6] == q {
				rejected[msg[idx+1:idx+6]] = true
			}
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	return rejected
}
