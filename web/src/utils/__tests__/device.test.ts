import { deviceSummary } from "@web-shared/device";
import { describe, expect, it } from "vitest";

describe("deviceSummary", () => {
	it("pairs browser and system when both are recognizable", () => {
		expect(
			deviceSummary(
				"Mozilla/5.0 (X11; Linux x86_64; rv:141.0) Gecko/20100101 Firefox/141.0",
			),
		).toBe("Firefox · Linux");
	});

	// Engines impersonate their ancestors: Edge carries "Chrome", Chrome
	// carries "Safari", Android carries "Linux". The specific token must win.
	it("prefers the impersonating engine's own token", () => {
		expect(
			deviceSummary(
				"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36 Edg/126.0",
			),
		).toBe("Edge · Windows");
		expect(
			deviceSummary(
				"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Mobile Safari/537.36",
			),
		).toBe("Chrome · Android");
	});

	it("falls back to the half it recognizes", () => {
		expect(deviceSummary("something Firefox/141.0 something")).toBe("Firefox");
		expect(deviceSummary("curl-ish thing on Windows")).toBe("Windows");
	});

	it("returns null for empty or unrecognizable strings", () => {
		expect(deviceSummary("")).toBeNull();
		expect(deviceSummary("curl/8.9")).toBeNull();
	});
});
