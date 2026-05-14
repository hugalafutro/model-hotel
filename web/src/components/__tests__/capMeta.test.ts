import { describe, expect, it } from "vitest";
import type { ModelCapabilities } from "../../api/types";
import { CAP_META, type CapKey, hasCap, matchesAllCaps } from "../capMeta";

describe("CAP_META", () => {
	it("has 8 entries", () => {
		expect(CAP_META).toHaveLength(8);
	});

	it("each entry has key, label, style, muted, disabled strings", () => {
		CAP_META.forEach((meta) => {
			expect(typeof meta.key).toBe("string");
			expect(typeof meta.label).toBe("string");
			expect(typeof meta.style).toBe("string");
			expect(typeof meta.muted).toBe("string");
			expect(typeof meta.disabled).toBe("string");
		});
	});

	it("keys match all CapKey union values", () => {
		const capKeys = CAP_META.map((m) => m.key);
		expect(capKeys).toContain("vision");
		expect(capKeys).toContain("reasoning");
		expect(capKeys).toContain("tool_calling");
		expect(capKeys).toContain("structured_output");
		expect(capKeys).toContain("pdf_upload");
		expect(capKeys).toContain("video_input");
		expect(capKeys).toContain("audio_input");
		expect(capKeys).toContain("parallel_tool_calls");
	});
});

describe("hasCap", () => {
	it("returns false when caps is null", () => {
		expect(hasCap(null, "vision")).toBe(false);
		expect(hasCap(null, "reasoning")).toBe(false);
		expect(hasCap(null, "tool_calling")).toBe(false);
	});

	it("returns true for truthy cap values", () => {
		const caps: ModelCapabilities = {
			vision: true,
			reasoning: true,
			tool_calling: true,
			structured_output: true,
			pdf_upload: true,
			video_input: true,
			audio_input: true,
			parallel_tool_calls: true,
		};
		expect(hasCap(caps, "vision")).toBe(true);
		expect(hasCap(caps, "reasoning")).toBe(true);
		expect(hasCap(caps, "tool_calling")).toBe(true);
		expect(hasCap(caps, "structured_output")).toBe(true);
		expect(hasCap(caps, "pdf_upload")).toBe(true);
		expect(hasCap(caps, "video_input")).toBe(true);
		expect(hasCap(caps, "audio_input")).toBe(true);
		expect(hasCap(caps, "parallel_tool_calls")).toBe(true);
	});

	it("returns false for falsy cap values", () => {
		const caps: ModelCapabilities = {
			vision: false,
			reasoning: false,
			tool_calling: false,
			structured_output: false,
			pdf_upload: false,
			video_input: false,
			audio_input: false,
			parallel_tool_calls: false,
		};
		expect(hasCap(caps, "vision")).toBe(false);
		expect(hasCap(caps, "reasoning")).toBe(false);
		expect(hasCap(caps, "tool_calling")).toBe(false);
		expect(hasCap(caps, "structured_output")).toBe(false);
		expect(hasCap(caps, "pdf_upload")).toBe(false);
		expect(hasCap(caps, "video_input")).toBe(false);
		expect(hasCap(caps, "audio_input")).toBe(false);
		expect(hasCap(caps, "parallel_tool_calls")).toBe(false);
	});
});

describe("matchesAllCaps", () => {
	it("returns true when keys set is empty", () => {
		expect(matchesAllCaps(null, new Set())).toBe(true);
		expect(matchesAllCaps({}, new Set())).toBe(true);
		expect(matchesAllCaps({ vision: true, reasoning: true }, new Set())).toBe(
			true,
		);
	});

	it("returns true when all caps are present", () => {
		const caps: ModelCapabilities = {
			vision: true,
			reasoning: true,
			tool_calling: true,
		};
		const keys = new Set<CapKey>(["vision", "reasoning", "tool_calling"]);
		expect(matchesAllCaps(caps, keys)).toBe(true);
	});

	it("returns false when one cap is missing", () => {
		const caps: ModelCapabilities = {
			vision: true,
			reasoning: false,
			tool_calling: true,
		};
		const keys = new Set<CapKey>(["vision", "reasoning", "tool_calling"]);
		expect(matchesAllCaps(caps, keys)).toBe(false);
	});

	it("returns false when caps is null and keys set is non-empty", () => {
		const keys = new Set<CapKey>(["vision", "reasoning"]);
		expect(matchesAllCaps(null, keys)).toBe(false);
	});

	it("returns false when some caps are missing from the set", () => {
		const caps: ModelCapabilities = {
			vision: true,
			tool_calling: true,
		};
		const keys = new Set<CapKey>(["vision", "reasoning", "tool_calling"]);
		expect(matchesAllCaps(caps, keys)).toBe(false);
	});

	it("returns true for single cap match", () => {
		const caps: ModelCapabilities = { vision: true };
		const keys = new Set<CapKey>(["vision"]);
		expect(matchesAllCaps(caps, keys)).toBe(true);
	});

	it("returns false for single cap mismatch", () => {
		const caps: ModelCapabilities = { vision: false };
		const keys = new Set<CapKey>(["vision"]);
		expect(matchesAllCaps(caps, keys)).toBe(false);
	});

	it("handles all 8 capability keys", () => {
		const caps: ModelCapabilities = {
			vision: true,
			reasoning: true,
			tool_calling: true,
			structured_output: true,
			pdf_upload: true,
			video_input: true,
			audio_input: true,
			parallel_tool_calls: true,
		};
		const keys = new Set<CapKey>(CAP_META.map((m) => m.key));
		expect(matchesAllCaps(caps, keys)).toBe(true);
	});

	it("returns false when one of 8 capabilities is missing", () => {
		const caps: ModelCapabilities = {
			vision: true,
			reasoning: true,
			tool_calling: true,
			structured_output: true,
			pdf_upload: true,
			video_input: true,
			audio_input: false,
			parallel_tool_calls: true,
		};
		const keys = new Set<CapKey>(CAP_META.map((m) => m.key));
		expect(matchesAllCaps(caps, keys)).toBe(false);
	});
});
