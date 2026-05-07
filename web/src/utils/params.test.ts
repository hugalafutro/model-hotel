import { describe, expect, it } from "vitest";
import type { GenerationParams } from "../api/types";
import { hasAnyParam } from "./params";

describe("hasAnyParam", () => {
	it("returns false for empty params", () => {
		expect(hasAnyParam({})).toBe(false);
	});

	it("returns false when all fields are undefined", () => {
		const p: GenerationParams = {
			temperature: undefined,
			max_tokens: undefined,
			top_p: undefined,
		};
		expect(hasAnyParam(p)).toBe(false);
	});

	it("returns true when temperature is set", () => {
		expect(hasAnyParam({ temperature: 0.7 })).toBe(true);
	});

	it("returns true when max_tokens is set", () => {
		expect(hasAnyParam({ max_tokens: 1024 })).toBe(true);
	});

	it("returns true when top_p is set", () => {
		expect(hasAnyParam({ top_p: 0.9 })).toBe(true);
	});

	it("returns true when min_p is set", () => {
		expect(hasAnyParam({ min_p: 0.1 })).toBe(true);
	});

	it("returns true when top_k is set", () => {
		expect(hasAnyParam({ top_k: 50 })).toBe(true);
	});

	it("returns true when frequency_penalty is set", () => {
		expect(hasAnyParam({ frequency_penalty: 0.5 })).toBe(true);
	});

	it("returns true when presence_penalty is set", () => {
		expect(hasAnyParam({ presence_penalty: 0.3 })).toBe(true);
	});

	it("returns true when multiple params are set", () => {
		expect(
			hasAnyParam({ temperature: 0.7, max_tokens: 2048, top_p: 0.9 }),
		).toBe(true);
	});
});
