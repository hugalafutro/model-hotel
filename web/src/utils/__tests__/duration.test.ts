import { describe, expect, it } from "vitest";
import { goDurationToSeconds, secondsToGoDuration } from "../duration";

describe("goDurationToSeconds", () => {
	it("parses simple seconds", () => {
		expect(goDurationToSeconds("30s")).toBe(30);
	});

	it("parses minutes", () => {
		expect(goDurationToSeconds("5m")).toBe(300);
	});

	it("parses hours", () => {
		expect(goDurationToSeconds("1h")).toBe(3600);
	});

	it("parses compound durations", () => {
		expect(goDurationToSeconds("1h30m")).toBe(5400);
	});

	it("parses zero", () => {
		expect(goDurationToSeconds("0s")).toBe(0);
	});

	it("parses full compound with all units", () => {
		expect(goDurationToSeconds("1h30m45s")).toBe(5445);
	});

	it("parses minutes and seconds without hours", () => {
		expect(goDurationToSeconds("5m30s")).toBe(330);
	});

	it("returns 0 for unrecognized format", () => {
		expect(goDurationToSeconds("abc")).toBe(0);
	});

	it("handles empty string", () => {
		expect(goDurationToSeconds("")).toBe(0);
	});
});

describe("secondsToGoDuration", () => {
	it("converts 0 to 0s", () => {
		expect(secondsToGoDuration(0)).toBe("0s");
	});

	it("converts negative to 0s", () => {
		expect(secondsToGoDuration(-5)).toBe("0s");
	});

	it("converts 3600 to 1h", () => {
		expect(secondsToGoDuration(3600)).toBe("1h");
	});

	it("converts 90 to 1m30s", () => {
		expect(secondsToGoDuration(90)).toBe("1m30s");
	});

	it("converts 30 to 30s", () => {
		expect(secondsToGoDuration(30)).toBe("30s");
	});

	it("converts 300 to 5m", () => {
		expect(secondsToGoDuration(300)).toBe("5m");
	});

	it("converts 3661 to 1h1m1s", () => {
		expect(secondsToGoDuration(3661)).toBe("1h1m1s");
	});

	it("converts 330 to 5m30s", () => {
		expect(secondsToGoDuration(330)).toBe("5m30s");
	});

	it("converts 86400 to 24h", () => {
		expect(secondsToGoDuration(86400)).toBe("24h");
	});
});

describe("roundtrip", () => {
	it("0s roundtrips", () => {
		expect(secondsToGoDuration(goDurationToSeconds("0s"))).toBe("0s");
	});

	it("30s roundtrips", () => {
		expect(secondsToGoDuration(goDurationToSeconds("30s"))).toBe("30s");
	});

	it("5m roundtrips", () => {
		expect(secondsToGoDuration(goDurationToSeconds("5m"))).toBe("5m");
	});

	it("1h30m roundtrips", () => {
		expect(secondsToGoDuration(goDurationToSeconds("1h30m"))).toBe("1h30m");
	});

	it("1h30m45s roundtrips", () => {
		expect(secondsToGoDuration(goDurationToSeconds("1h30m45s"))).toBe(
			"1h30m45s",
		);
	});
});
