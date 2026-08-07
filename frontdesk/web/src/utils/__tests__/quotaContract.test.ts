import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import type { KimiCodeQuotaResponse } from "../../api/types";
import { getKimiCodeFiveHourLimit, getKimiCodeWeeklyLimit } from "../quota";

// Cross-platform contract fixtures. The same files back the Model Hotel
// dashboard suite and the Bellhop unit tests, so every app that renders a stored
// Kimi snapshot derives the same numbers from it.
const FIXTURE_DIR = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	"../../../../../testdata/quota-contract/kimi",
);

interface KimiContractFixture {
	payload: KimiCodeQuotaResponse;
	expected: { fiveHourUsedPercent: number; weeklyUsedPercent: number };
}

const fixtureFiles = readdirSync(FIXTURE_DIR)
	.filter((f) => f.endsWith(".json"))
	.sort();

describe("Kimi Code quota contract fixtures", () => {
	it("reads the shared fixture directory", () => {
		expect(fixtureFiles.length).toBeGreaterThanOrEqual(3);
	});

	it.each(fixtureFiles)("%s yields the expected percentages", (file) => {
		const fixture = JSON.parse(
			readFileSync(path.join(FIXTURE_DIR, file), "utf8"),
		) as KimiContractFixture;

		expect(getKimiCodeFiveHourLimit(fixture.payload)?.percentage).toBeCloseTo(
			fixture.expected.fiveHourUsedPercent,
			10,
		);
		expect(getKimiCodeWeeklyLimit(fixture.payload)?.percentage).toBeCloseTo(
			fixture.expected.weeklyUsedPercent,
			10,
		);
	});
});
