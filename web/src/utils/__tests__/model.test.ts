import { describe, expect, it } from "vitest";
import {
	formatPrice,
	formatPriceInput,
	is5xxError,
	isChatModel,
	nonTextOutputs,
	normalizeProviderName,
	parseCapabilities,
	providerFromModelID,
	proxyModelID,
} from "../model";

describe("normalizeProviderName", () => {
	it("replaces spaces with hyphens", () => {
		expect(normalizeProviderName("Open AI")).toBe("Open-AI");
		expect(normalizeProviderName("Google Cloud")).toBe("Google-Cloud");
		expect(normalizeProviderName("Ollama Cloud")).toBe("Ollama-Cloud");
	});

	it("handles multiple spaces", () => {
		expect(normalizeProviderName("My Test Provider")).toBe("My-Test-Provider");
	});

	it("returns unchanged string with no spaces", () => {
		expect(normalizeProviderName("OpenAI")).toBe("OpenAI");
		expect(normalizeProviderName("Anthropic")).toBe("Anthropic");
	});

	it("handles empty string", () => {
		expect(normalizeProviderName("")).toBe("");
	});
});

describe("proxyModelID", () => {
	it("combines normalized provider name and model ID with slash", () => {
		expect(proxyModelID("OpenAI", "gpt-4o")).toBe("OpenAI/gpt-4o");
		expect(proxyModelID("Ollama Cloud", "gemma3:4b")).toBe(
			"Ollama-Cloud/gemma3:4b",
		);
		expect(proxyModelID("Google Cloud", "gemini-2.0")).toBe(
			"Google-Cloud/gemini-2.0",
		);
	});

	it("handles provider names with multiple spaces", () => {
		expect(proxyModelID("My Test Provider", "model-1")).toBe(
			"My-Test-Provider/model-1",
		);
	});

	it("handles empty model ID", () => {
		expect(proxyModelID("OpenAI", "")).toBe("OpenAI/");
	});
});

describe("providerFromModelID", () => {
	it("extracts provider from simple proxy model ID", () => {
		expect(providerFromModelID("OpenAI/gpt-4o")).toBe("OpenAI");
		expect(providerFromModelID("Anthropic/claude-3.5")).toBe("Anthropic");
	});

	it("extracts provider with spaces (normalized)", () => {
		expect(providerFromModelID("Ollama-Cloud/gemma3:4b")).toBe("Ollama-Cloud");
		expect(providerFromModelID("Google-Cloud/gemini-2.0")).toBe("Google-Cloud");
	});

	it("uses knownProviders to match longest prefix first", () => {
		const providers = ["OpenAI", "OpenAI-Pro", "Anthropic"];
		expect(providerFromModelID("OpenAI-Pro/gpt-4", providers)).toBe(
			"OpenAI-Pro",
		);
		expect(providerFromModelID("OpenAI/gpt-4", providers)).toBe("OpenAI");
	});

	it("handles provider names with spaces in knownProviders", () => {
		const providers = ["Ollama Cloud", "Google Cloud"];
		expect(providerFromModelID("Ollama-Cloud/gemma3", providers)).toBe(
			"Ollama Cloud",
		);
		expect(providerFromModelID("Google-Cloud/gemini", providers)).toBe(
			"Google Cloud",
		);
	});

	it("falls back to first segment when no knownProviders match", () => {
		expect(providerFromModelID("UnknownProvider/model-1")).toBe(
			"UnknownProvider",
		);
		expect(providerFromModelID("Foo/Bar/Baz")).toBe("Foo");
	});

	it("returns full string when no slash present", () => {
		expect(providerFromModelID("JustModelName")).toBe("JustModelName");
	});

	it("handles empty string", () => {
		expect(providerFromModelID("")).toBe("");
	});

	it("handles model IDs with multiple slashes", () => {
		expect(providerFromModelID("Provider/model/with/slashes")).toBe("Provider");
	});
});

describe("parseCapabilities", () => {
	it("parses valid JSON capabilities string", () => {
		const json = '{"vision": true, "audio_input": false}';
		expect(parseCapabilities(json)).toEqual({
			vision: true,
			audio_input: false,
		});
	});

	it("returns empty object for empty string", () => {
		expect(parseCapabilities("")).toEqual({});
	});

	it("returns empty object for invalid JSON syntax", () => {
		expect(parseCapabilities("not json")).toEqual({});
		expect(parseCapabilities("{invalid: json}")).toEqual({});
		expect(parseCapabilities('{"unclosed": "string"')).toEqual({});
	});

	it("returns empty object for invalid JSON", () => {
		expect(parseCapabilities("not json")).toEqual({});
		expect(parseCapabilities("{invalid: json}")).toEqual({});
		expect(parseCapabilities('{"unclosed": "string"')).toEqual({});
	});

	it("returns empty object for invalid JSON syntax", () => {
		expect(parseCapabilities("not json")).toEqual({});
		expect(parseCapabilities("{invalid: json}")).toEqual({});
		expect(parseCapabilities('{"unclosed": "string"')).toEqual({});
	});

	it("returns parsed value for valid JSON primitives (not objects)", () => {
		// These are valid JSON, so they parse successfully but aren't objects
		expect(parseCapabilities("null")).toBe(null);
		expect(parseCapabilities("123")).toBe(123);
		expect(parseCapabilities("true")).toBe(true);
	});

	it("parses complex capabilities", () => {
		const json =
			'{"vision": true, "audio_input": true, "function_calling": false, "json_mode": true}';
		expect(parseCapabilities(json)).toEqual({
			vision: true,
			audio_input: true,
			function_calling: false,
			json_mode: true,
		});
	});
});

describe("formatPrice", () => {
	it("returns '-' for null or undefined", () => {
		expect(formatPrice(null)).toBe("-");
		expect(formatPrice(undefined)).toBe("-");
	});

	it("formats whole numbers", () => {
		expect(formatPrice(10)).toBe("10");
		expect(formatPrice(100)).toBe("100");
		expect(formatPrice(0)).toBe("0");
	});

	it("formats decimal numbers, trimming trailing zeros", () => {
		expect(formatPrice(0.0025)).toBe("0.0025");
		expect(formatPrice(0.0025)).toBe("0.0025");
		expect(formatPrice(0.1)).toBe("0.1");
		expect(formatPrice(0.5)).toBe("0.5");
	});

	it("rounds to 4 decimal places", () => {
		expect(formatPrice(0.00005)).toBe("0.0001");
		expect(formatPrice(0.00004)).toBe("0");
		expect(formatPrice(0.00255)).toBe("0.0026");
	});

	it("handles numbers with many decimal places", () => {
		expect(formatPrice(0.123456789)).toBe("0.1235");
		expect(formatPrice(1.99999)).toBe("2");
	});

	it("handles very small numbers", () => {
		expect(formatPrice(0.00001)).toBe("0");
		expect(formatPrice(0.0001)).toBe("0.0001");
	});
});

describe("formatPriceInput", () => {
	it("returns empty string for null or undefined", () => {
		expect(formatPriceInput(null)).toBe("");
		expect(formatPriceInput(undefined)).toBe("");
	});

	it("formats whole numbers", () => {
		expect(formatPriceInput(10)).toBe("10");
		expect(formatPriceInput(100)).toBe("100");
		expect(formatPriceInput(0)).toBe("0");
	});

	it("formats decimal numbers, trimming trailing zeros", () => {
		expect(formatPriceInput(0.0025)).toBe("0.0025");
		expect(formatPriceInput(0.0025)).toBe("0.0025");
		expect(formatPriceInput(0.1)).toBe("0.1");
		expect(formatPriceInput(0.5)).toBe("0.5");
	});

	it("rounds to 4 decimal places", () => {
		expect(formatPriceInput(0.00005)).toBe("0.0001");
		expect(formatPriceInput(0.00004)).toBe("0");
		expect(formatPriceInput(0.00255)).toBe("0.0026");
	});

	it("handles numbers with many decimal places", () => {
		expect(formatPriceInput(0.123456789)).toBe("0.1235");
		expect(formatPriceInput(1.99999)).toBe("2");
	});

	it("handles very small numbers", () => {
		expect(formatPriceInput(0.00001)).toBe("0");
		expect(formatPriceInput(0.0001)).toBe("0.0001");
	});
});

describe("is5xxError", () => {
	it("returns false for null or undefined", () => {
		expect(is5xxError(null)).toBe(false);
		expect(is5xxError(undefined)).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(is5xxError("")).toBe(false);
	});

	it("returns true for 500-level status codes", () => {
		expect(is5xxError("Chat failed: 500 Internal Server Error")).toBe(true);
		expect(is5xxError("Arena failed: 502 Bad Gateway")).toBe(true);
		expect(is5xxError("Request failed: 503 Service Unavailable")).toBe(true);
		expect(is5xxError("Error: 504 Gateway Timeout")).toBe(true);
		expect(is5xxError("Server error 599")).toBe(true);
	});

	it("returns false for non-5xx status codes", () => {
		expect(is5xxError("Chat failed: 400 Bad Request")).toBe(false);
		expect(is5xxError("Request failed: 401 Unauthorized")).toBe(false);
		expect(is5xxError("Error: 403 Forbidden")).toBe(false);
		expect(is5xxError("Not found: 404")).toBe(false);
		expect(is5xxError("Client error 429")).toBe(false);
		expect(is5xxError("Success: 200 OK")).toBe(false);
		expect(is5xxError("Created: 201")).toBe(false);
	});

	it("returns false for strings without status codes", () => {
		expect(is5xxError("Connection timeout")).toBe(false);
		expect(is5xxError("Network error")).toBe(false);
		expect(is5xxError("Something went wrong")).toBe(false);
		expect(is5xxError("API key invalid")).toBe(false);
	});

	it("returns false for numbers that look like 5xx but aren't status codes", () => {
		// These should still match because the regex looks for 5XX pattern
		expect(is5xxError("Error code 51234")).toBe(false); // 512 is matched but 512 is 5xx
		expect(is5xxError("Version 5000")).toBe(false); // 500 is matched
	});

	it("matches status code anywhere in the string", () => {
		expect(is5xxError("500: Internal Server Error")).toBe(true);
		expect(is5xxError("Request failed with status 503")).toBe(true);
		expect(is5xxError("HTTP 502 error")).toBe(true);
	});

	it("handles edge case 500 and 599", () => {
		expect(is5xxError("Error 500")).toBe(true);
		expect(is5xxError("Error 599")).toBe(true);
		expect(is5xxError("Error 499")).toBe(false);
		expect(is5xxError("Error 600")).toBe(false);
	});
});

describe("isChatModel", () => {
	it("returns true for the chat class and legacy chat-capable modalities", () => {
		for (const modality of ["chat", "text", "vision", "audio", "multimodal"]) {
			expect(isChatModel({ modality })).toBe(true);
		}
	});

	it("returns false for non-chat classes", () => {
		for (const modality of [
			"embedding",
			"rerank",
			"image",
			"video",
			"tts",
			"stt",
		]) {
			expect(isChatModel({ modality })).toBe(false);
		}
	});

	it("is case-insensitive", () => {
		expect(isChatModel({ modality: "Embedding" })).toBe(false);
		expect(isChatModel({ modality: "RERANK" })).toBe(false);
	});

	it("default-allows unknown or missing modalities", () => {
		expect(isChatModel({ modality: "future-modality" })).toBe(true);
		expect(isChatModel({ modality: "" })).toBe(true);
		expect(isChatModel({})).toBe(true);
	});

	it("excludes media-generation models by non-text output", () => {
		// Video generator mislabelled "vision" (from image input): still excluded.
		expect(
			isChatModel({ modality: "vision", output_modalities: '["video"]' }),
		).toBe(false);
		expect(isChatModel({ output_modalities: '["image"]' })).toBe(false);
		expect(isChatModel({ output_modalities: '["image","video"]' })).toBe(false);
	});

	it("keeps chat models that also emit media, and video-input chat models", () => {
		// Outputs text alongside images → chat.
		expect(
			isChatModel({
				modality: "vision",
				output_modalities: '["text","image"]',
			}),
		).toBe(true);
		// Video *input* chat model (class chat, outputs text) stays visible.
		expect(
			isChatModel({ modality: "chat", output_modalities: '["text"]' }),
		).toBe(true);
	});

	it("default-allows empty or malformed output_modalities", () => {
		expect(isChatModel({ output_modalities: "" })).toBe(true);
		expect(isChatModel({ output_modalities: "not-json" })).toBe(true);
		expect(isChatModel({ output_modalities: "[]" })).toBe(true);
	});
});

describe("nonTextOutputs", () => {
	it("returns non-text output modalities in stored order", () => {
		expect(nonTextOutputs({ output_modalities: '["text","image"]' })).toEqual([
			"image",
		]);
		expect(nonTextOutputs({ output_modalities: '["image","video"]' })).toEqual([
			"image",
			"video",
		]);
		expect(nonTextOutputs({ output_modalities: '["embedding"]' })).toEqual([
			"embedding",
		]);
	});

	it("returns empty for text-only, missing, or malformed outputs", () => {
		expect(nonTextOutputs({ output_modalities: '["text"]' })).toEqual([]);
		expect(nonTextOutputs({ output_modalities: "" })).toEqual([]);
		expect(nonTextOutputs({ output_modalities: "not-json" })).toEqual([]);
		expect(nonTextOutputs({})).toEqual([]);
	});

	it("lowercases values", () => {
		expect(nonTextOutputs({ output_modalities: '["TEXT","Image"]' })).toEqual([
			"image",
		]);
	});
});
