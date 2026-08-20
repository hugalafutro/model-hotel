import { describe, expect, it } from "vitest";
import {
	baseUrls,
	hasEditableBaseUrl,
	isKnownProviderUrl,
	isLocalProviderType,
	localProviderPlaceholders,
	providerTypeAllowsEmptyKey,
	providerTypeTranslationKeys,
} from "../constants";

describe("baseUrls", () => {
	it("has entry for openai", () => {
		expect(baseUrls.openai).toBe("https://api.openai.com/v1");
	});

	it("has entry for anthropic", () => {
		expect(baseUrls.anthropic).toBe("https://api.anthropic.com");
	});

	it("has entry for deepseek", () => {
		expect(baseUrls.deepseek).toBe("https://api.deepseek.com/v1");
	});

	it("does not have localhost entry for ollama", () => {
		expect(baseUrls.ollama).toBeUndefined();
	});

	it("has entry for ollama-cloud", () => {
		expect(baseUrls["ollama-cloud"]).toBe("https://ollama.com/v1");
	});

	it("has entry for google", () => {
		expect(baseUrls.google).toBe(
			"https://generativelanguage.googleapis.com/v1beta/openai",
		);
	});

	it("has entry for xai", () => {
		expect(baseUrls.xai).toBe("https://api.x.ai/v1");
	});

	it("has entry for cohere", () => {
		expect(baseUrls.cohere).toBe("https://api.cohere.ai/compatibility/v1");
	});

	it("has entry for openrouter", () => {
		expect(baseUrls.openrouter).toBe("https://openrouter.ai/api/v1");
	});

	it("has entry for bedrock", () => {
		expect(baseUrls.bedrock).toBe(
			"https://bedrock-mantle.us-east-1.api.aws/v1",
		);
	});

	it("has entry for azure", () => {
		expect(baseUrls.azure).toBe(
			"https://your-resource.services.ai.azure.com/api/projects/your-project",
		);
	});

	it("has entry for vertex-express", () => {
		expect(baseUrls["vertex-express"]).toBe(
			"https://aiplatform.googleapis.com",
		);
	});

	it("does not have localhost entry for koboldcpp", () => {
		expect(baseUrls.koboldcpp).toBeUndefined();
	});

	it("does not have localhost entry for lmstudio", () => {
		expect(baseUrls.lmstudio).toBeUndefined();
	});

	it("has entry for nanogpt", () => {
		expect(baseUrls.nanogpt).toBe("https://nano-gpt.com/api/subscription/v1");
	});

	it("has entry for zai-coding", () => {
		expect(baseUrls["zai-coding"]).toBe("https://api.z.ai/api/coding/paas/v4");
	});

	it("has entry for kimi-code", () => {
		expect(baseUrls["kimi-code"]).toBe("https://api.kimi.com/coding/v1");
	});

	it("has entry for minimax", () => {
		expect(baseUrls.minimax).toBe("https://api.minimax.io/v1");
	});

	it("has entry for opencode-zen", () => {
		expect(baseUrls["opencode-zen"]).toBe("https://opencode.ai/zen/v1");
	});

	it("has entry for opencode-go", () => {
		expect(baseUrls["opencode-go"]).toBe("https://opencode.ai/zen/go/v1");
	});
});

describe("localProviderPlaceholders", () => {
	// These are placeholders, never values: a self-hosted server's address is
	// the operator's to supply, and a containerised Model Hotel cannot reach
	// its own localhost.
	it.each(["ollama", "koboldcpp", "lmstudio"])(
		"offers a routable example address for %s",
		(type) => {
			const example = localProviderPlaceholders[type];
			expect(example).toBeDefined();
			expect(example).not.toContain("localhost");
			expect(example).not.toContain("127.0.0.1");
		},
	);
});

describe("isLocalProviderType", () => {
	it("returns true for ollama", () => {
		expect(isLocalProviderType("ollama")).toBe(true);
	});

	it("returns true for koboldcpp", () => {
		expect(isLocalProviderType("koboldcpp")).toBe(true);
	});

	it("returns true for lmstudio", () => {
		expect(isLocalProviderType("lmstudio")).toBe(true);
	});

	it("returns false for openai", () => {
		expect(isLocalProviderType("openai")).toBe(false);
	});

	it("returns false for custom", () => {
		expect(isLocalProviderType("custom")).toBe(false);
	});
});

describe("hasEditableBaseUrl", () => {
	// The two hand-entered dialects and the self-hosted servers: an operator
	// types the address, so the field must not be locked or pre-filled.
	it.each(["custom", "anthropic-messages", "ollama", "koboldcpp", "lmstudio"])(
		"leaves the base URL editable for %s",
		(type) => {
			expect(hasEditableBaseUrl(type)).toBe(true);
		},
	);

	// Hosted APIs live at one known address, which the dialog fills in and locks
	// so it cannot be mistyped. "anthropic" is the pointed case: its Messages
	// sibling is editable, and confusing the two would unlock the official one.
	it.each(["anthropic", "openai", "deepseek", "vertex-express", "openrouter"])(
		"locks the base URL for %s",
		(type) => {
			expect(hasEditableBaseUrl(type)).toBe(false);
		},
	);

	it("locks an unknown type rather than assuming it is hand-entered", () => {
		expect(hasEditableBaseUrl("")).toBe(false);
		expect(hasEditableBaseUrl("not-a-type")).toBe(false);
	});
});

describe("isKnownProviderUrl", () => {
	it("returns true for openai url", () => {
		expect(isKnownProviderUrl("https://api.openai.com/v1")).toBe(true);
	});

	it("returns true for anthropic url", () => {
		expect(isKnownProviderUrl("https://api.anthropic.com")).toBe(true);
	});

	it("returns true for deepseek url", () => {
		expect(isKnownProviderUrl("https://api.deepseek.com/v1")).toBe(true);
	});

	it("returns false for ollama localhost url (editable, not locked)", () => {
		expect(isKnownProviderUrl("http://localhost:11434")).toBe(false);
	});

	it("returns true for ollama-cloud url", () => {
		expect(isKnownProviderUrl("https://ollama.com/v1")).toBe(true);
	});

	it("returns true for google url", () => {
		expect(
			isKnownProviderUrl(
				"https://generativelanguage.googleapis.com/v1beta/openai",
			),
		).toBe(true);
	});

	it("returns false for koboldcpp localhost url (editable, not locked)", () => {
		expect(isKnownProviderUrl("http://localhost:5001/v1")).toBe(false);
	});

	it("returns false for lmstudio localhost url (editable, not locked)", () => {
		expect(isKnownProviderUrl("http://localhost:1234/v1")).toBe(false);
	});

	it("returns false for unknown url", () => {
		expect(isKnownProviderUrl("https://unknown-provider.com/api")).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(isKnownProviderUrl("")).toBe(false);
	});

	it("returns false for similar but different url", () => {
		expect(isKnownProviderUrl("https://api.openai.com/v2")).toBe(false);
		expect(isKnownProviderUrl("https://api.anthropic.com/v1")).toBe(false);
	});
});

describe("providerTypeTranslationKeys", () => {
	it("has translation key for custom", () => {
		expect(providerTypeTranslationKeys.custom).toBe("providers.type_custom");
	});

	it("has translation key for openai", () => {
		expect(providerTypeTranslationKeys.openai).toBe("providers.type_openai");
	});

	it("has translation key for anthropic", () => {
		expect(providerTypeTranslationKeys.anthropic).toBe(
			"providers.type_anthropic",
		);
	});

	it("has translation key for anthropic-messages", () => {
		expect(providerTypeTranslationKeys["anthropic-messages"]).toBe(
			"providers.type_anthropic_messages",
		);
	});

	it("has translation key for deepseek", () => {
		expect(providerTypeTranslationKeys.deepseek).toBe(
			"providers.type_deepseek",
		);
	});

	it("has translation key for ollama", () => {
		expect(providerTypeTranslationKeys.ollama).toBe("providers.type_ollama");
	});

	it("has translation key for ollama-cloud", () => {
		expect(providerTypeTranslationKeys["ollama-cloud"]).toBe(
			"providers.type_ollama_cloud",
		);
	});

	it("has translation key for google", () => {
		expect(providerTypeTranslationKeys.google).toBe("providers.type_google");
	});

	it("has translation key for xai", () => {
		expect(providerTypeTranslationKeys.xai).toBe("providers.type_xai");
	});

	it("has translation key for cohere", () => {
		expect(providerTypeTranslationKeys.cohere).toBe("providers.type_cohere");
	});

	it("has translation key for openrouter", () => {
		expect(providerTypeTranslationKeys.openrouter).toBe(
			"providers.type_openrouter",
		);
	});

	it("has translation key for koboldcpp", () => {
		expect(providerTypeTranslationKeys.koboldcpp).toBe(
			"providers.type_koboldcpp",
		);
	});

	it("has translation key for lmstudio", () => {
		expect(providerTypeTranslationKeys.lmstudio).toBe(
			"providers.type_lmstudio",
		);
	});

	it("has translation key for nanogpt", () => {
		expect(providerTypeTranslationKeys.nanogpt).toBe("providers.type_nanogpt");
	});

	it("has translation key for zai-coding", () => {
		expect(providerTypeTranslationKeys["zai-coding"]).toBe(
			"providers.type_zai_coding",
		);
	});

	it("has translation key for kimi-code", () => {
		expect(providerTypeTranslationKeys["kimi-code"]).toBe(
			"providers.type_kimi_code",
		);
	});

	it("has translation key for minimax", () => {
		expect(providerTypeTranslationKeys.minimax).toBe("providers.type_minimax");
	});

	it("has translation key for opencode-zen", () => {
		expect(providerTypeTranslationKeys["opencode-zen"]).toBe(
			"providers.type_opencode_zen",
		);
	});

	it("has translation key for opencode-go", () => {
		expect(providerTypeTranslationKeys["opencode-go"]).toBe(
			"providers.type_opencode_go",
		);
	});
});

describe("providerTypeAllowsEmptyKey", () => {
	it("returns true for opencode-zen", () => {
		expect(providerTypeAllowsEmptyKey("opencode-zen")).toBe(true);
	});

	it("returns true for ollama", () => {
		expect(providerTypeAllowsEmptyKey("ollama")).toBe(true);
	});

	it("returns true for custom", () => {
		expect(providerTypeAllowsEmptyKey("custom")).toBe(true);
	});

	it("returns true for koboldcpp", () => {
		expect(providerTypeAllowsEmptyKey("koboldcpp")).toBe(true);
	});

	it("returns true for lmstudio", () => {
		expect(providerTypeAllowsEmptyKey("lmstudio")).toBe(true);
	});

	it("returns false for openai", () => {
		expect(providerTypeAllowsEmptyKey("openai")).toBe(false);
	});

	it("returns false for anthropic", () => {
		expect(providerTypeAllowsEmptyKey("anthropic")).toBe(false);
	});

	// A remote Messages endpoint authenticates by key like any other hosted API;
	// only self-hosted servers and keyless tiers may be added without one.
	it("returns false for anthropic-messages", () => {
		expect(providerTypeAllowsEmptyKey("anthropic-messages")).toBe(false);
	});

	it("returns false for deepseek", () => {
		expect(providerTypeAllowsEmptyKey("deepseek")).toBe(false);
	});

	it("returns false for ollama-cloud", () => {
		expect(providerTypeAllowsEmptyKey("ollama-cloud")).toBe(false);
	});

	it("returns false for google", () => {
		expect(providerTypeAllowsEmptyKey("google")).toBe(false);
	});

	it("returns false for unknown provider type", () => {
		expect(providerTypeAllowsEmptyKey("unknown")).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(providerTypeAllowsEmptyKey("")).toBe(false);
	});
});
