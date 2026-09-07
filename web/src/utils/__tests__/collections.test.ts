import { describe, expect, it } from "vitest";
import { toggleInSet } from "../collections";

describe("toggleInSet", () => {
	it("adds a missing key", () => {
		expect([...toggleInSet(new Set(["a"]), "b")]).toEqual(["a", "b"]);
	});

	it("removes a present key", () => {
		expect([...toggleInSet(new Set(["a", "b"]), "a")]).toEqual(["b"]);
	});

	it("forces membership when `on` is given", () => {
		expect(toggleInSet(new Set(["a"]), "a", true).has("a")).toBe(true);
		expect(toggleInSet(new Set<string>(), "a", false).has("a")).toBe(false);
	});

	it("returns a new set, leaving the input untouched", () => {
		const original = new Set(["a"]);
		const next = toggleInSet(original, "b");
		expect(next).not.toBe(original);
		expect([...original]).toEqual(["a"]);
	});
});
