export const baseUrls: Record<string, string> = {
	nanogpt: "https://nano-gpt.com/api/subscription/v1",
	"zai-coding": "https://api.z.ai/api/paas/v4",
	openai: "https://api.openai.com/v1",
	anthropic: "https://api.anthropic.com",
	deepseek: "https://api.deepseek.com/v1",
	"ollama-cloud": "https://ollama.com/v1",
	ollama: "http://localhost:11434",
	"opencode-zen": "https://opencode.ai/zen/v1",
	"opencode-go": "https://opencode.ai/zen/go/v1",
	xai: "https://api.x.ai/v1",
	google: "https://generativelanguage.googleapis.com/v1beta/openai",
	cohere: "https://api.cohere.ai/compatibility/v1",
	openrouter: "https://openrouter.ai/api/v1",
	koboldcpp: "http://localhost:5001/v1",
	lmstudio: "http://localhost:1234/v1",
};

export function isKnownProviderUrl(url: string): boolean {
	return Object.values(baseUrls).includes(url);
}

export function getProviderType(baseUrl: string): string {
	for (const [type, url] of Object.entries(baseUrls)) {
		if (baseUrl === url) return type;
	}
	return "custom";
}

export const providerTypeDisplayNames: Record<string, string> = {
	custom: "Custom",
	nanogpt: "NanoGPT",
	"zai-coding": "Z.ai Coding Plan",
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
	koboldcpp: "KoboldCPP (Local)",
	lmstudio: "LM Studio (Local)",
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
