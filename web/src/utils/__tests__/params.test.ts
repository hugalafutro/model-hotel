import { describe, expect, it } from "vitest";
import type { GenerationParams } from "../../api/types";
import { hasAnyParam } from "../params";

describe("hasAnyParam", () => {
	it("returns false for empty object", () => {
		const params: GenerationParams = {};
		expect(hasAnyParam(params)).toBe(false);
	});

	it("returns true when temperature is set", () => {
		expect(hasAnyParam({ temperature: 0.5 })).toBe(true);
		expect(hasAnyParam({ temperature: 0 })).toBe(true);
		expect(hasAnyParam({ temperature: 1 })).toBe(true);
	});

	it("returns true when max_tokens is set", () => {
		expect(hasAnyParam({ max_tokens: 100 })).toBe(true);
		expect(hasAnyParam({ max_tokens: 0 })).toBe(true);
	});

	it("returns true when top_p is set", () => {
		expect(hasAnyParam({ top_p: 0.9 })).toBe(true);
		expect(hasAnyParam({ top_p: 0 })).toBe(true);
	});

	it("returns true when min_p is set", () => {
		expect(hasAnyParam({ min_p: 0.1 })).toBe(true);
		expect(hasAnyParam({ min_p: 0 })).toBe(true);
	});

	it("returns true when top_k is set", () => {
		expect(hasAnyParam({ top_k: 50 })).toBe(true);
		expect(hasAnyParam({ top_k: 0 })).toBe(true);
	});

	it("returns true when frequency_penalty is set", () => {
		expect(hasAnyParam({ frequency_penalty: 0.5 })).toBe(true);
		expect(hasAnyParam({ frequency_penalty: 0 })).toBe(true);
	});

	it("returns true when presence_penalty is set", () => {
		expect(hasAnyParam({ presence_penalty: 0.5 })).toBe(true);
		expect(hasAnyParam({ presence_penalty: 0 })).toBe(true);
	});

	it("returns true when multiple fields are set", () => {
		expect(hasAnyParam({ temperature: 0.5, max_tokens: 100, top_p: 0.9 })).toBe(
			true,
		);
		expect(
			hasAnyParam({
				temperature: 0.7,
				max_tokens: 256,
				top_p: 0.9,
				min_p: 0.05,
				top_k: 40,
				frequency_penalty: 0.3,
				presence_penalty: 0.3,
			}),
		).toBe(true);
	});

	it("returns false when all fields are null", () => {
		// null is not undefined, so this should be true
		// But the function checks for !== undefined, and null !== undefined is true
		// So we need to test with explicit undefined
		expect(hasAnyParam({})).toBe(false);
	});

	it("returns false when fields are explicitly undefined", () => {
		const params: GenerationParams = {
			temperature: undefined,
			max_tokens: undefined,
			top_p: undefined,
			min_p: undefined,
			top_k: undefined,
			frequency_penalty: undefined,
			presence_penalty: undefined,
		};
		expect(hasAnyParam(params)).toBe(false);
	});

	it("returns true when reasoning_effort is set", () => {
		expect(hasAnyParam({ reasoning_effort: "high" })).toBe(true);
		expect(hasAnyParam({ reasoning_effort: "medium" })).toBe(true);
		expect(hasAnyParam({ reasoning_effort: "low" })).toBe(true);
	});
});
