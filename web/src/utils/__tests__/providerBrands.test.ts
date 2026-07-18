import { describe, expect, it } from "vitest";
import type { ProviderBrand } from "../providerBrands";
import { PROVIDER_BRAND_COLORS, PROVIDER_PREFIXES } from "../providerBrands";

describe("PROVIDER_BRAND_COLORS", () => {
	const HEX_COLOR_REGEX = /^#[0-9A-Fa-f]{6}$/;

	it("has valid hex colors for all ProviderBrand keys", () => {
		// Derived from the map itself so newly added brands (bedrock,
		// neuralwatt, ...) are regression-guarded without editing this list;
		// Record<ProviderBrand, string> already enforces completeness.
		const brands = Object.keys(PROVIDER_BRAND_COLORS) as ProviderBrand[];
		expect(brands.length).toBeGreaterThanOrEqual(16);

		for (const brand of brands) {
			expect(PROVIDER_BRAND_COLORS[brand]).toBeDefined();
			expect(PROVIDER_BRAND_COLORS[brand]).toMatch(HEX_COLOR_REGEX);
		}
	});

	it("has correct spot checks for known colors", () => {
		expect(PROVIDER_BRAND_COLORS.anthropic).toBe("#D97757");
		expect(PROVIDER_BRAND_COLORS.openai).toBe("#000000");
		expect(PROVIDER_BRAND_COLORS.xai).toBe("#1A1A1A");
		expect(PROVIDER_BRAND_COLORS.google).toBe("#4285F4");
		expect(PROVIDER_BRAND_COLORS.deepseek).toBe("#4D6BFE");
		expect(PROVIDER_BRAND_COLORS.nanogpt).toBe("#0EA5B0");
		expect(PROVIDER_BRAND_COLORS.azure).toBe("#0078D4");
		expect(PROVIDER_BRAND_COLORS["vertex-express"]).toBe("#4285F4");
	});

	it("has consistent colors for ollama variants", () => {
		expect(PROVIDER_BRAND_COLORS.ollama).toBe("#3D3D3D");
		expect(PROVIDER_BRAND_COLORS["ollama-cloud"]).toBe("#3D3D3D");
	});

	it("has consistent colors for opencode variants", () => {
		expect(PROVIDER_BRAND_COLORS.opencode).toBe("#2D2D2D");
		expect(PROVIDER_BRAND_COLORS["zai-coding"]).toBe("#2D2D2D");
	});
});

describe("PROVIDER_PREFIXES", () => {
	it("has prefix for all ProviderBrand keys", () => {
		const brands: ProviderBrand[] = [
			"anthropic",
			"openai",
			"google",
			"deepseek",
			"xai",
			"ollama",
			"ollama-cloud",
			"openrouter",
			"cohere",
			"zai-coding",
			"nanogpt",
			"lmstudio",
			"koboldcpp",
			"opencode",
		];

		for (const brand of brands) {
			expect(PROVIDER_PREFIXES[brand]).toBeDefined();
		}
	});

	it("has 2-3 character prefixes", () => {
		const brands: ProviderBrand[] = [
			"anthropic",
			"openai",
			"google",
			"deepseek",
			"xai",
			"ollama",
			"ollama-cloud",
			"openrouter",
			"cohere",
			"zai-coding",
			"nanogpt",
			"lmstudio",
			"koboldcpp",
			"opencode",
		];

		for (const brand of brands) {
			const prefix = PROVIDER_PREFIXES[brand];
			expect(prefix.length).toBeGreaterThanOrEqual(2);
			expect(prefix.length).toBeLessThanOrEqual(3);
		}
	});

	it("has correct spot checks for known prefixes", () => {
		expect(PROVIDER_PREFIXES.anthropic).toBe("AC");
		expect(PROVIDER_PREFIXES.openai).toBe("OA");
		expect(PROVIDER_PREFIXES.xai).toBe("XAI");
		expect(PROVIDER_PREFIXES.google).toBe("GEM");
		expect(PROVIDER_PREFIXES.deepseek).toBe("DS");
		expect(PROVIDER_PREFIXES.nanogpt).toBe("NG");
		expect(PROVIDER_PREFIXES.openrouter).toBe("OR");
	});

	it("has consistent prefixes for ollama variants", () => {
		expect(PROVIDER_PREFIXES.ollama).toBe("OLL");
		expect(PROVIDER_PREFIXES["ollama-cloud"]).toBe("OLC");
	});
});
