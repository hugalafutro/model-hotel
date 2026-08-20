package provider

import (
	"net/url"
	"slices"
	"strings"
)

// KnownTypes is the provider-type vocabulary shared with the dashboard's add
// dialog (web/src/pages/Providers/constants.ts). The operator picks one when a
// provider is added and it is stored on the row; nothing derives it from the
// URL afterwards.
//
// "custom" and "openai" both mean a generic OpenAI-compatible endpoint: they
// take the same discovery and proxying path, and are kept apart only so the
// dashboard can label a hand-entered endpoint as custom rather than claiming
// it is OpenAI.
//
// "anthropic-messages" is the Messages-API twin of "custom": a hand-entered
// endpoint that speaks Anthropic's native /v1/messages instead of OpenAI's
// /v1/chat/completions. It carries no host rule, because the operator picks it
// for an address nothing can infer the dialect from.
var KnownTypes = []string{
	"custom",
	"openai",
	"nanogpt",
	"zai-coding",
	"kimi-code",
	"minimax",
	"anthropic",
	"anthropic-messages",
	"deepseek",
	"ollama",
	"ollama-cloud",
	"opencode-zen",
	"opencode-go",
	"xai",
	"google",
	"vertex-express",
	"cohere",
	"openrouter",
	"neuralwatt",
	"bedrock",
	"azure",
	"koboldcpp",
	"lmstudio",
}

// IsKnownType reports whether t is part of the provider-type vocabulary.
func IsKnownType(t string) bool {
	return slices.Contains(KnownTypes, t)
}

// LocalServerTypes are the self-hosted server families that run on an address
// the operator chooses. They are the only types Model Hotel verifies by
// probing, because they are the only ones whose address says nothing about
// what is listening on it.
var LocalServerTypes = []string{"ollama", "lmstudio", "koboldcpp"}

// IsLocalServerType reports whether t is a self-hosted server family.
func IsLocalServerType(t string) bool {
	return slices.Contains(LocalServerTypes, t)
}

// NormalizeLocalBaseURL puts a self-hosted server's base URL in the form the
// rest of the code expects: the OpenAI-compatible mount, ending in /v1.
// Ollama, LM Studio and KoboldCPP all serve /v1/chat/completions and all serve
// their native endpoints at the root, so storing the /v1 form loses nothing and
// spares the operator from having to know which half to type. Other types are
// returned unchanged.
func NormalizeLocalBaseURL(providerType, baseURL string) string {
	if !IsLocalServerType(providerType) {
		return baseURL
	}
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return baseURL
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return baseURL
	}
	// Append to the path, never to the whole string: a base URL carrying a
	// query would otherwise end up with the mount inside it.
	u.Path = strings.TrimRight(u.Path, "/")
	// Case-insensitive: a base URL typed as /V1 is the same mount, and adding a
	// second one would send every request to a path that does not exist.
	if !strings.EqualFold(pathSuffix(u.Path, len("/v1")), "/v1") {
		u.Path += "/v1"
	}
	return u.String()
}

// SameLocalAddress reports whether two base URLs point at the same self-hosted
// server, ignoring the differences that do not change where the request lands:
// the /v1 mount, a trailing slash, and host case.
func SameLocalAddress(a, b string) bool {
	ka, kb := localServerOrigin(a), localServerOrigin(b)
	return ka != "" && strings.EqualFold(ka, kb)
}

// pathSuffix returns the last n characters of p, or all of p when it is
// shorter.
func pathSuffix(p string, n int) string {
	if len(p) < n {
		return p
	}
	return p[len(p)-n:]
}

// localServerOrigin strips the OpenAI-compatible /v1 mount so the native
// endpoints (/api/...) can be addressed.
func localServerOrigin(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u, err := url.Parse(trimmed); err == nil && u.Host != "" {
		trimmedPath := strings.TrimRight(u.Path, "/")
		if strings.EqualFold(pathSuffix(trimmedPath, len("/v1")), "/v1") {
			trimmedPath = trimmedPath[:len(trimmedPath)-len("/v1")]
		}
		u.Path = trimmedPath
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}
	return strings.TrimSuffix(trimmed, "/v1")
}
