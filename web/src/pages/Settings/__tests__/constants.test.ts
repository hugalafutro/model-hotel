import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	clearProviderCache,
	DISCOVERY_INTERVALS,
	getProviderCacheCount,
	UI_STYLES,
} from "../constants";

describe("DISCOVERY_INTERVALS", () => {
	it("has 6 entries", () => {
		expect(DISCOVERY_INTERVALS).toHaveLength(6);
	});

	it("has 30 minutes option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "30m");
		expect(interval).toEqual({ value: "30m", label: "30 minutes" });
	});

	it("has 1 hour option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "1h");
		expect(interval).toEqual({ value: "1h", label: "1 hour" });
	});

	it("has 6 hours option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "6h");
		expect(interval).toEqual({ value: "6h", label: "6 hours" });
	});

	it("has 12 hours option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "12h");
		expect(interval).toEqual({ value: "12h", label: "12 hours" });
	});

	it("has 24 hours option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "24h");
		expect(interval).toEqual({ value: "24h", label: "24 hours" });
	});

	it("has disabled option", () => {
		const interval = DISCOVERY_INTERVALS.find((i) => i.value === "0");
		expect(interval).toEqual({ value: "0", label: "Disabled" });
	});

	it("all entries have value and label properties", () => {
		DISCOVERY_INTERVALS.forEach((interval) => {
			expect(interval).toHaveProperty("value");
			expect(interval).toHaveProperty("label");
			expect(typeof interval.value).toBe("string");
			expect(typeof interval.label).toBe("string");
		});
	});
});

describe("UI_STYLES", () => {
	it("has 3 entries", () => {
		expect(UI_STYLES).toHaveLength(3);
	});

	it("has clean-saas style", () => {
		const style = UI_STYLES.find((s) => s.id === "clean-saas");
		expect(style).toBeDefined();
		expect(style?.label).toBe("Clean SaaS");
		expect(style?.description).toBe("Refined, professional, minimal");
		expect(style?.icon).toBeDefined();
	});

	it("has cyber-terminal style", () => {
		const style = UI_STYLES.find((s) => s.id === "cyber-terminal");
		expect(style).toBeDefined();
		expect(style?.label).toBe("Cyber Terminal");
		expect(style?.description).toBe("Developer-centric, high-contrast");
		expect(style?.icon).toBeDefined();
	});

	it("has glassmorphism-lite style", () => {
		const style = UI_STYLES.find((s) => s.id === "glassmorphism-lite");
		expect(style).toBeDefined();
		expect(style?.label).toBe("Glassmorphism");
		expect(style?.description).toBe("Slick, translucent surfaces");
		expect(style?.icon).toBeDefined();
	});

	it("all entries have required properties", () => {
		UI_STYLES.forEach((style) => {
			expect(style).toHaveProperty("id");
			expect(style).toHaveProperty("label");
			expect(style).toHaveProperty("description");
			expect(style).toHaveProperty("icon");
			expect(typeof style.id).toBe("string");
			expect(typeof style.label).toBe("string");
			expect(typeof style.description).toBe("string");
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
	beforeEach(() => {
		vi.spyOn(localStorage, "getItem").mockReturnValue(null);
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns 0 when no cache keys exist", () => {
		const count = getProviderCacheCount();
		expect(count).toBe(0);
	});

	it("returns count of existing cache keys", () => {
		vi.spyOn(localStorage, "getItem").mockImplementation((key: string) => {
			if (key === "model-hotel:nanogpt-usage") return '{"tokens": 100}';
			if (key === "model-hotel:deepseek-balance") return '{"balance": 50}';
			return null;
		});

		const count = getProviderCacheCount();

		expect(count).toBe(2);
	});

	it("returns 4 when all cache keys exist", () => {
		vi.spyOn(localStorage, "getItem").mockReturnValue("some-value");

		const count = getProviderCacheCount();

		expect(count).toBe(4);
	});

	it("handles localStorage errors gracefully", () => {
		vi.spyOn(localStorage, "getItem").mockImplementation(() => {
			throw new Error("localStorage not available");
		});

		const count = getProviderCacheCount();

		expect(count).toBe(0);
	});
});

describe("clearProviderCache", () => {
	beforeEach(() => {
		vi.spyOn(localStorage, "removeItem");
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("removes all provider cache keys", () => {
		clearProviderCache();

		expect(localStorage.removeItem).toHaveBeenCalledWith(
			"model-hotel:nanogpt-usage",
		);
		expect(localStorage.removeItem).toHaveBeenCalledWith(
			"model-hotel:zai-coding-usage",
		);
		expect(localStorage.removeItem).toHaveBeenCalledWith(
			"model-hotel:deepseek-balance",
		);
		expect(localStorage.removeItem).toHaveBeenCalledWith(
			"model-hotel:ollama-cloud-account",
		);
		expect(localStorage.removeItem).toHaveBeenCalledTimes(4);
	});

	it("handles localStorage errors gracefully", () => {
		vi.spyOn(localStorage, "removeItem").mockImplementation(() => {
			throw new Error("localStorage not available");
		});

		expect(() => clearProviderCache()).not.toThrow();
		expect(localStorage.removeItem).toHaveBeenCalledTimes(4);
	});
});
