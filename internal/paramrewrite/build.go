package paramrewrite

import (
	"encoding/json"
	"fmt"
	"maps"
	"sync"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ProviderSupportsStreamOptions returns true for provider types that accept
// the OpenAI stream_options parameter in their Chat Completions endpoint.
// Providers with non-OpenAI APIs (Anthropic, Google, Cohere) or those that
// strict-validate unknown fields should return false to avoid 400 errors.
func ProviderSupportsStreamOptions(providerType string) bool {
	switch providerType {
	case "anthropic", "google", "cohere", "opencode-go", "opencode-zen":
		return false
	default:
		// All OpenAI-compatible providers (openai, deepseek, xai, openrouter,
		// ollama, ollama-cloud, nanogpt, zai-coding, lmstudio, koboldcpp,
		// neuralwatt, bedrock, etc.) accept or silently ignore stream_options.
		return true
	}
}

// BuildUpstreamBody rewrites the client request body for a specific provider
// candidate. This is the single shared rewrite path used by the initial proxy
// attempt, the proxy 400 auto-retry, and the admin "Test model" probe,
// preventing drift between them.
//
// Steps (applied in order):
//  1. Model rename (client model → resolved model)
//  2. stream_options injection (streaming + OpenAI-compatible providers only)
//  3. Provider-specific param injection (InjectProviderParams)
//  4. Learned param renaming (renameCache, e.g. max_tokens → max_completion_tokens)
//  5. Universal param stripping (ProviderUnsupportedParams)
//  6. Learned param stripping (deprecationCache)
//  7. Extra param stripping (additional rejected params, e.g. from 400 auto-retry)
//  8. Message sanitization (drop empty tool_calls arrays)
//
// Injection (step 3) runs before all stripping (steps 5-7) so that a param a
// provider injects but the upstream then rejects (learned into the deprecation
// cache) is removed and stays removed on subsequent requests. Were injection
// last, a learned rejection would be re-added on every fresh request, forcing a
// 400+retry round-trip every time instead of just the first.
//
// Renaming (step 4) runs before stripping so the moved value lands under its new
// key before any strip phase could delete the old key — a rename must preserve
// the caller's value (e.g. their token budget), unlike a strip which discards it.
func BuildUpstreamBody(
	proxyReqBody []byte,
	providerType string,
	resolvedModelID string,
	requestModel string,
	isStreaming bool,
	deprecationCache *sync.Map,
	renameCache *sync.Map,
	extraStrip map[string]bool,
) []byte {
	var raw map[string]any
	if err := json.Unmarshal(proxyReqBody, &raw); err != nil {
		return proxyReqBody // unparseable — forward as-is
	}

	// 1. Model rename
	if requestModel != resolvedModelID {
		raw["model"] = resolvedModelID
	}

	// 2. stream_options injection
	if isStreaming && ProviderSupportsStreamOptions(providerType) {
		raw["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}

	// 3. Provider-specific param injection.
	// Runs before all stripping so a learned/extra rejection of an injected
	// param (e.g. chat_template_args) removes it and keeps it removed, rather
	// than the param being re-added after the strip phases.
	InjectProviderParams(raw, providerType, resolvedModelID)

	cacheKey := fmt.Sprintf("%s:%s", providerType, resolvedModelID)

	// 4. Learned param renaming (runs before stripping to preserve the value).
	// Each rename moves old→new only when old is present and new is not already
	// set, so an explicit caller value under the new key is never overwritten.
	if renames := cachedRenames(renameCache, cacheKey); renames != nil {
		for oldName, newName := range renames {
			if v, ok := raw[oldName]; ok {
				if _, exists := raw[newName]; !exists {
					raw[newName] = v
				}
				delete(raw, oldName)
			}
		}
	}

	// 5. Universal param stripping
	if params, ok := ProviderUnsupportedParams[providerType]; ok {
		for _, p := range params {
			delete(raw, p)
		}
	}

	// 6. Learned param stripping
	if cached := CachedRejectedParams(deprecationCache, cacheKey); cached != nil {
		for param := range cached {
			delete(raw, param)
		}
	}

	// 7. Extra param stripping (e.g. newly-learned rejections from 400 auto-retry)
	for param := range extraStrip {
		delete(raw, param)
	}

	// 8. Message sanitization
	stripEmptyToolCalls(raw)

	if b, err := json.Marshal(raw); err == nil {
		return b
	}
	return proxyReqBody
}

// stripEmptyToolCalls removes "tool_calls": [] from every message in the
// request. Some clients serialize an assistant turn whose tool calls were
// aborted or filtered out as an empty array instead of omitting the field;
// the OpenAI spec requires min length 1 and strict providers (DeepSeek,
// OpenCode Zen) reject the whole request with a 400, permanently bricking any
// conversation that has such a turn in its history. No provider needs the
// empty array, so dropping it is always safe.
func stripEmptyToolCalls(raw map[string]any) {
	msgs, ok := raw["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) == 0 {
			delete(msg, "tool_calls")
		}
	}
}

// MergeLearnedParamCache merges newly-learned per-model param metadata into a
// sync.Map cache keyed by "providerType:modelID", race-free under concurrent
// goroutines via CompareAndSwap. Values are stored as *map[string]V (pointers,
// because maps are not comparable and CompareAndSwap requires comparable values).
func MergeLearnedParamCache[V any](cache *sync.Map, key string, learned map[string]V) {
	for {
		existing, loaded := cache.LoadOrStore(key, &learned)
		if !loaded {
			return // first entry for this key — we just stored 'learned'
		}
		existingMap, ok := existing.(*map[string]V)
		if !ok {
			debuglog.Error("learned param cache: unexpected type", "key", key, "type", fmt.Sprintf("%T", existing))
			return
		}
		merged := make(map[string]V, len(*existingMap)+len(learned))
		maps.Copy(merged, *existingMap)
		maps.Copy(merged, learned)
		if cache.CompareAndSwap(key, existing, &merged) {
			return
		}
		// CompareAndSwap failed — another goroutine updated it, retry.
	}
}
