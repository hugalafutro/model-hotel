import { describe, expect, it } from "vitest";
import { generateTopic } from "../ntfy";

describe("generateTopic", () => {
	it("returns 20 url-safe alphanumerics, different each call", () => {
		const a = generateTopic();
		const b = generateTopic();
		expect(a).toMatch(/^[A-Za-z0-9]{20}$/);
		expect(a).not.toBe(b);
	});
});
