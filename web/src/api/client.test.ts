import { describe, expect, it } from "vitest";
import { buildQueryString, buildUrl } from "../api/client";

describe("buildQueryString", () => {
	it("returns empty string for empty params", () => {
		expect(buildQueryString({})).toBe("");
	});

	it("serializes string values", () => {
		const qs = buildQueryString({ key: "value" });
		expect(qs).toBe("key=value");
	});

	it("serializes number values", () => {
		const qs = buildQueryString({ page: 1 });
		expect(qs).toBe("page=1");
	});

	it("serializes boolean values", () => {
		const qs = buildQueryString({ active: true });
		expect(qs).toBe("active=true");
	});

	it("skips undefined values", () => {
		const qs = buildQueryString({ key: undefined });
		expect(qs).toBe("");
	});

	it("skips null values", () => {
		const qs = buildQueryString({ key: null as unknown as undefined });
		expect(qs).toBe("");
	});

	it("serializes multiple params", () => {
		const qs = buildQueryString({ a: "1", b: 2, c: true });
		expect(qs).toContain("a=1");
		expect(qs).toContain("b=2");
		expect(qs).toContain("c=true");
	});

	it("skips undefined in mixed params", () => {
		const qs = buildQueryString({ a: "1", b: undefined, c: "3" });
		expect(qs).toContain("a=1");
		expect(qs).toContain("c=3");
		expect(qs).not.toContain("b=");
	});
});

describe("buildUrl", () => {
	it("returns path with API_BASE prefix when no params", () => {
		expect(buildUrl("/api/test")).toBe("/api/test");
	});

	it("appends query string for non-empty params", () => {
		const url = buildUrl("/api/test", { page: 1 });
		expect(url).toBe("/api/test?page=1");
	});

	it("omits query string when all params are undefined", () => {
		expect(buildUrl("/api/test", { page: undefined })).toBe("/api/test");
	});

	it("builds URL with multiple params", () => {
		const url = buildUrl("/api/logs", { page: 2, per_page: 50 });
		expect(url).toContain("/api/logs?");
		expect(url).toContain("page=2");
		expect(url).toContain("per_page=50");
	});
});
