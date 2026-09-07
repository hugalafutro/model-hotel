import { describe, expect, it } from "vitest";
import { getRoundLabel } from "../arenaRounds";

describe("getRoundLabel", () => {
	it("returns Generation for compare mode", () => {
		expect(getRoundLabel(0, 3, "compare")).toBe("Generation");
		expect(getRoundLabel(1, 3, "compare")).toBe("Generation");
		expect(getRoundLabel(2, 3, "compare")).toBe("Generation");
	});

	it("returns Match for single round bracket", () => {
		expect(getRoundLabel(0, 1, "bracket")).toBe("Match");
	});

	it("returns Final for last round", () => {
		expect(getRoundLabel(2, 3, "bracket")).toBe("Final");
		expect(getRoundLabel(1, 2, "bracket")).toBe("Final");
	});

	it("returns Semifinals for second-to-last round", () => {
		expect(getRoundLabel(1, 3, "bracket")).toBe("Semifinals");
		expect(getRoundLabel(0, 2, "bracket")).toBe("Semifinals");
	});

	it("returns Quarterfinals for third-to-last round", () => {
		expect(getRoundLabel(0, 3, "bracket")).toBe("Quarterfinals");
	});

	it("returns Round N for earlier rounds", () => {
		expect(getRoundLabel(0, 4, "bracket")).toBe("Round 1");
		expect(getRoundLabel(1, 4, "bracket")).toBe("Quarterfinals");
		expect(getRoundLabel(2, 4, "bracket")).toBe("Semifinals");
	});
});
