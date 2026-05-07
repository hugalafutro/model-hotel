import { describe, expect, it } from "vitest";
import {
	formatPrice,
	formatPriceInput,
	normalizeProviderName,
	parseCapabilities,
	providerFromModelID,
	proxyModelID,
} from "./model";

describe("normalizeProviderName", () => {
	it("replaces spaces with hyphens", () => {
		expect(normalizeProviderName("Open AI")).toBe("Open-AI");
	});

	it("leaves names without spaces unchanged", () => {
		expect(normalizeProviderName("OpenAI")).toBe("OpenAI");
	});

	it("replaces multiple spaces", () => {
		expect(normalizeProviderName("My Cool Provider")).toBe("My-Cool-Provider");
	});
});

describe("proxyModelID", () => {
	it("combines provider and model with slash", () => {
		expect(proxyModelID("OpenAI", "gpt-4o")).toBe("OpenAI/gpt-4o");
	});

	it("normalizes provider name with spaces", () => {
		expect(proxyModelID("Open AI", "gpt-4o")).toBe("Open-AI/gpt-4o");
	});
});

describe("providerFromModelID", () => {
	it("extracts provider from known provider list", () => {
		expect(providerFromModelID("OpenAI/gpt-4o", ["OpenAI", "Anthropic"])).toBe(
			"OpenAI",
		);
	});

	it("matches longest provider name first (avoids false splits)", () => {
		expect(
			providerFromModelID("My-Provider/gpt-4o", ["My", "My-Provider"]),
		).toBe("My-Provider");
	});

	it("falls back to first slash segment when no known providers", () => {
		expect(providerFromModelID("SomeProvider/model-name", [])).toBe(
			"SomeProvider",
		);
	});

	it("returns full string when no slash present", () => {
		expect(providerFromModelID("no-slash-model")).toBe("no-slash-model");
	});

	it("handles normalized provider names with spaces", () => {
		expect(providerFromModelID("Open-AI/gpt-4o", ["Open AI"])).toBe("Open AI");
	});
});

describe("parseCapabilities", () => {
	it("parses valid JSON", () => {
		expect(parseCapabilities('{"vision":true,"audio":false}')).toEqual({
			vision: true,
			audio: false,
		});
	});

	it("returns empty object for invalid JSON", () => {
		expect(parseCapabilities("not-json")).toEqual({});
	});

	it("returns empty object for empty string", () => {
		expect(parseCapabilities("")).toEqual({});
	});
});

describe("formatPrice", () => {
	it("returns '-' for null", () => {
		expect(formatPrice(null)).toBe("-");
	});

	it("returns '-' for undefined", () => {
		expect(formatPrice(undefined)).toBe("-");
	});

	it("formats integer prices", () => {
		expect(formatPrice(5)).toBe("5");
	});

	it("formats decimal prices, trimming trailing zeros", () => {
		expect(formatPrice(1.5)).toBe("1.5");
		expect(formatPrice(0.03)).toBe("0.03");
	});

	it("rounds to 4 decimal places", () => {
		expect(formatPrice(1.23456)).toBe("1.2346");
	});

	it("removes trailing zeros after decimal", () => {
		expect(formatPrice(2.1)).toBe("2.1");
		expect(formatPrice(2.0)).toBe("2");
	});
});

describe("formatPriceInput", () => {
	it("returns empty string for null", () => {
		expect(formatPriceInput(null)).toBe("");
	});

	it("returns empty string for undefined", () => {
		expect(formatPriceInput(undefined)).toBe("");
	});

	it("formats prices the same as formatPrice for valid numbers", () => {
		expect(formatPriceInput(1.5)).toBe("1.5");
		expect(formatPriceInput(0)).toBe("0");
	});
});
