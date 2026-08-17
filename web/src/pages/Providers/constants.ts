export const baseUrls: Record<string, string> = {
	nanogpt: "https://nano-gpt.com/api/subscription/v1",
	"zai-coding": "https://api.z.ai/api/coding/paas/v4",
	"kimi-code": "https://api.kimi.com/coding/v1",
	minimax: "https://api.minimax.io/v1",
	openai: "https://api.openai.com/v1",
	anthropic: "https://api.anthropic.com",
	deepseek: "https://api.deepseek.com/v1",
	"ollama-cloud": "https://ollama.com/v1",
	"opencode-zen": "https://opencode.ai/zen/v1",
	"opencode-go": "https://opencode.ai/zen/go/v1",
	xai: "https://api.x.ai/v1",
	google: "https://generativelanguage.googleapis.com/v1beta/openai",
	cohere: "https://api.cohere.ai/compatibility/v1",
	openrouter: "https://openrouter.ai/api/v1",
	neuralwatt: "https://api.neuralwatt.com/v1",
	bedrock: "https://bedrock-mantle.us-east-1.api.aws/v1",
	azure:
		"https://your-resource.services.ai.azure.com/api/projects/your-project",
	"vertex-express": "https://aiplatform.googleapis.com",
};

/** Example addresses for self-hosted providers, shown as placeholder text.
 * Nothing is pre-filled: only the operator knows whether their server runs on
 * this machine or another one, and a containerised Model Hotel cannot reach
 * its own localhost. The ports are the conventional ones, but any port works. */
export const localProviderPlaceholders: Record<string, string> = {
	ollama: "http://192.168.1.50:11434",
	koboldcpp: "http://192.168.1.50:5001",
	lmstudio: "http://192.168.1.50:1234",
};

/** Self-hosted provider types whose base URL is editable (not locked). */
export const localProviderTypes = new Set(["ollama", "koboldcpp", "lmstudio"]);

/** Returns true for provider types whose base URL defaults to localhost but may run elsewhere. */
export function isLocalProviderType(type: string): boolean {
	return localProviderTypes.has(type);
}

export function isKnownProviderUrl(url: string): boolean {
	return Object.values(baseUrls).includes(url);
}

/** @deprecated Use providerTypeTranslationKeys + t() instead. Kept for reference only. */
export const providerTypeDisplayNames: Record<string, string> = {
	custom: "Custom",
	nanogpt: "NanoGPT",
	"zai-coding": "Z.ai Coding Plan",
	"kimi-code": "Kimi Code",
	minimax: "MiniMax",
	openai: "OpenAI",
	anthropic: "Anthropic",
	deepseek: "DeepSeek",
	"ollama-cloud": "Ollama Cloud",
	ollama: "Ollama",
	"opencode-zen": "OpenCode Zen",
	"opencode-go": "OpenCode Go",
	xai: "xAI (Grok)",
	google: "Google AI Studio (Gemini)",
	cohere: "Cohere",
	openrouter: "OpenRouter",
	neuralwatt: "NeuralWatt",
	koboldcpp: "KoboldCPP",
	lmstudio: "LM Studio",
	bedrock: "AWS Bedrock",
	azure: "Azure AI Foundry",
	"vertex-express": "Vertex AI (express keys)",
};

/** Translation keys for provider type display names. Use with t() at consumption sites. */
export const providerTypeTranslationKeys: Record<string, string> = {
	custom: "providers.type_custom",
	nanogpt: "providers.type_nanogpt",
	"zai-coding": "providers.type_zai_coding",
	"kimi-code": "providers.type_kimi_code",
	minimax: "providers.type_minimax",
	openai: "providers.type_openai",
	anthropic: "providers.type_anthropic",
	deepseek: "providers.type_deepseek",
	"ollama-cloud": "providers.type_ollama_cloud",
	ollama: "providers.type_ollama",
	"opencode-zen": "providers.type_opencode_zen",
	"opencode-go": "providers.type_opencode_go",
	xai: "providers.type_xai",
	google: "providers.type_google",
	cohere: "providers.type_cohere",
	openrouter: "providers.type_openrouter",
	neuralwatt: "providers.type_neuralwatt",
	koboldcpp: "providers.type_koboldcpp",
	lmstudio: "providers.type_lmstudio",
	bedrock: "providers.type_bedrock",
	azure: "providers.type_azure",
	"vertex-express": "providers.type_vertex_express",
};

export function providerTypeAllowsEmptyKey(type: string): boolean {
	return (
		type === "opencode-zen" ||
		type === "ollama" ||
		type === "custom" ||
		type === "koboldcpp" ||
		type === "lmstudio"
	);
}

/** Returns true for providers that offer free models without requiring a key. */
export function providerTypeHasFreeModels(type: string): boolean {
	return type === "opencode-zen";
}
