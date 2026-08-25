import { describe, expect, it } from "vitest";
import { bucketLabel } from "../bucketLabel";

describe("bucketLabel", () => {
	// Local time on purpose: the labels describe the reader's clock, so the
	// dates are built the same way the chart's Date(p.bucket) ends up.
	const at = (h: number, m: number) => new Date(2024, 5, 15, h, m);

	it("labels a week bucket with the day", () => {
		const label = bucketLabel(at(14, 35), "1w");
		// Matches both en-GB ("15 Jun") and en-US ("Jun 15")
		expect(label).toMatch(/15.*Jun|Jun.*15/);
	});

	it("labels an hour bucket with minutes", () => {
		expect(bucketLabel(at(14, 35), "1h")).toBe("14:35");
	});

	it("labels a day bucket with the hour alone", () => {
		expect(bucketLabel(at(14, 35), "24h")).toBe("14:00");
	});

	it("zero-pads single-digit hours and minutes", () => {
		expect(bucketLabel(at(9, 5), "1h")).toBe("09:05");
		expect(bucketLabel(at(0, 0), "24h")).toBe("00:00");
	});
});
