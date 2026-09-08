import { describe, expect, it } from "vitest";
import { sortByName } from "../sort";

describe("sortByName", () => {
	it("sorts by name", () => {
		const items = [{ name: "beta" }, { name: "alpha" }];
		expect(sortByName(items).map((i) => i.name)).toEqual(["alpha", "beta"]);
	});

	it("does not mutate the input", () => {
		const items = [{ name: "beta" }, { name: "alpha" }];
		sortByName(items);
		expect(items[0].name).toBe("beta");
	});

	it("treats undefined as empty", () => {
		expect(sortByName(undefined)).toEqual([]);
	});
});
