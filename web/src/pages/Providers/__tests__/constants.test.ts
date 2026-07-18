import { describe, expect, it } from "vitest";
import {
	baseUrls,
	getProviderType,
	isKnownProviderUrl,
	isLocalProviderType,
	localProviderDefaults,
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

	it("has entry for opencode-zen", () => {
		expect(baseUrls["opencode-zen"]).toBe("https://opencode.ai/zen/v1");
	});

	it("has entry for opencode-go", () => {
		expect(baseUrls["opencode-go"]).toBe("https://opencode.ai/zen/go/v1");
	});
});

describe("localProviderDefaults", () => {
	it("has default for ollama", () => {
		expect(localProviderDefaults.ollama).toBe("http://localhost:11434");
	});

	it("has default for koboldcpp", () => {
		expect(localProviderDefaults.koboldcpp).toBe("http://localhost:5001/v1");
	});

	it("has default for lmstudio", () => {
		expect(localProviderDefaults.lmstudio).toBe("http://localhost:1234/v1");
	});
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

describe("getProviderType", () => {
	it("returns openai for openai url", () => {
		expect(getProviderType("https://api.openai.com/v1")).toBe("openai");
	});

	it("returns anthropic for anthropic url", () => {
		expect(getProviderType("https://api.anthropic.com")).toBe("anthropic");
	});

	it("returns deepseek for deepseek url", () => {
		expect(getProviderType("https://api.deepseek.com/v1")).toBe("deepseek");
	});

	it("returns ollama for localhost ollama url (port-based detection)", () => {
		expect(getProviderType("http://localhost:11434")).toBe("ollama");
	});

	it("returns ollama for LAN ollama url (port-based detection)", () => {
		expect(getProviderType("http://192.168.1.50:11434")).toBe("ollama");
	});

	it("returns ollama-cloud for ollama-cloud url", () => {
		expect(getProviderType("https://ollama.com/v1")).toBe("ollama-cloud");
	});

	it("returns bedrock for any-region mantle url (host-based detection)", () => {
		expect(
			getProviderType("https://bedrock-mantle.eu-central-1.api.aws/v1"),
		).toBe("bedrock");
	});

	it("returns bedrock for runtime url (host-based detection)", () => {
		expect(
			getProviderType("https://bedrock-runtime.us-west-2.amazonaws.com/v1"),
		).toBe("bedrock");
	});

	it("does not detect bedrock-named hosts on other domains", () => {
		expect(getProviderType("https://bedrock-mantle.example.com/v1")).toBe(
			"custom",
		);
	});

	it("returns google for google url", () => {
		expect(
			getProviderType(
				"https://generativelanguage.googleapis.com/v1beta/openai",
			),
		).toBe("google");
	});

	it("returns koboldcpp for localhost koboldcpp url (port-based detection)", () => {
		expect(getProviderType("http://localhost:5001/v1")).toBe("koboldcpp");
	});

	it("returns koboldcpp for LAN koboldcpp url (port-based detection)", () => {
		expect(getProviderType("http://192.168.1.50:5001/v1")).toBe("koboldcpp");
	});

	it("returns lmstudio for localhost lmstudio url (port-based detection)", () => {
		expect(getProviderType("http://localhost:1234/v1")).toBe("lmstudio");
	});

	it("returns lmstudio for LAN lmstudio url (port-based detection)", () => {
		expect(getProviderType("http://10.0.0.5:1234/v1")).toBe("lmstudio");
	});

	it("returns custom for LAN host with unrecognised port", () => {
		expect(getProviderType("http://192.168.1.50:9999/v1")).toBe("custom");
	});

	it("returns custom for unknown url", () => {
		expect(getProviderType("https://custom-provider.com/api")).toBe("custom");
	});

	it("returns custom for empty string", () => {
		expect(getProviderType("")).toBe("custom");
	});

	it("returns custom for partial match", () => {
		expect(getProviderType("https://api.openai.com")).toBe("custom");
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
