import { afterEach, describe, expect, it, vi } from "vitest";
import {
	clearProviderCache,
	getProviderCacheCount,
	UI_STYLES,
} from "../constants";

describe("UI_STYLES", () => {
	it("has 3 entries", () => {
		expect(UI_STYLES).toHaveLength(3);
	});

	it("all entries have required properties", () => {
		UI_STYLES.forEach((style) => {
			expect(style).toHaveProperty("id");
			expect(style).toHaveProperty("i18nKey");
			expect(style).toHaveProperty("icon");
			expect(typeof style.id).toBe("string");
			expect(typeof style.i18nKey).toBe("string");
		});
	});

	it("icon properties are valid component references", () => {
		UI_STYLES.forEach((style) => {
			expect(style.icon).toBeDefined();
			expect(style.icon).toHaveProperty("render");
		});
	});
});

describe("getProviderCacheCount", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns 0 when no cache keys exist", () => {
		const getItemSpy = vi.fn().mockReturnValue(null);
		vi.stubGlobal("localStorage", { getItem: getItemSpy });

		const count = getProviderCacheCount();
		expect(count).toBe(0);

		vi.unstubAllGlobals();
	});

	it("returns count of existing cache keys", () => {
		const getItemSpy = vi.fn().mockImplementation((key: string) => {
			if (key === "model-hotel:nanogpt-usage") return '{"tokens": 100}';
			if (key === "model-hotel:deepseek-balance") return '{"balance": 50}';
			return null;
		});
		vi.stubGlobal("localStorage", { getItem: getItemSpy });

		const count = getProviderCacheCount();

		expect(count).toBe(2);

		vi.unstubAllGlobals();
	});

	it("returns 9 when all cache keys exist", () => {
		const getItemSpy = vi.fn().mockReturnValue("some-value");
		vi.stubGlobal("localStorage", { getItem: getItemSpy });

		const count = getProviderCacheCount();

		expect(count).toBe(9);

		vi.unstubAllGlobals();
	});

	it("handles localStorage errors gracefully", () => {
		const getItemSpy = vi.fn().mockImplementation(() => {
			throw new Error("localStorage not available");
		});
		vi.stubGlobal("localStorage", { getItem: getItemSpy });

		const count = getProviderCacheCount();

		expect(count).toBe(0);

		vi.unstubAllGlobals();
	});
});

describe("clearProviderCache", () => {
	it("removes all provider cache keys", () => {
		const removeItemSpy = vi.fn();
		vi.stubGlobal("localStorage", { removeItem: removeItemSpy });

		clearProviderCache();

		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:nanogpt-usage");
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:zai-coding-usage");
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:kimi-code-usage");
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:minimax-usage");
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:deepseek-balance");
		expect(removeItemSpy).toHaveBeenCalledWith(
			"model-hotel:openrouter-balance",
		);
		expect(removeItemSpy).toHaveBeenCalledWith(
			"model-hotel:ollama-cloud-account",
		);
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:neuralwatt-quota");
		expect(removeItemSpy).toHaveBeenCalledWith("model-hotel:opencode-go-usage");
		expect(removeItemSpy).toHaveBeenCalledTimes(9);

		vi.unstubAllGlobals();
	});

	it("handles localStorage errors gracefully", () => {
		const removeItemSpy = vi.fn().mockImplementation(() => {
			throw new Error("localStorage not available");
		});
		vi.stubGlobal("localStorage", { removeItem: removeItemSpy });

		expect(() => clearProviderCache()).not.toThrow();
		expect(removeItemSpy).toHaveBeenCalledTimes(9);

		vi.unstubAllGlobals();
	});
});
