import { beforeEach, describe, expect, it } from "vitest";
import {
	buildQueryString,
	buildUrl,
	getAdminToken,
	getAuthHeaders,
	setAdminToken,
} from "../client";

describe("buildQueryString", () => {
	it("returns empty string for empty object", () => {
		expect(buildQueryString({})).toBe("");
	});

	it("handles string values", () => {
		expect(buildQueryString({ name: "test", foo: "bar" })).toBe(
			"name=test&foo=bar",
		);
	});

	it("handles number values", () => {
		expect(buildQueryString({ page: 1, per_page: 10 })).toBe(
			"page=1&per_page=10",
		);
	});

	it("handles boolean values", () => {
		expect(buildQueryString({ enabled: true, disabled: false })).toBe(
			"enabled=true&disabled=false",
		);
	});

	it("filters out undefined values", () => {
		expect(buildQueryString({ name: "test", skip: undefined })).toBe(
			"name=test",
		);
	});

	it("filters out null values", () => {
		expect(
			buildQueryString({ name: "test", skip: null } as unknown as Parameters<
				typeof buildQueryString
			>[0]),
		).toBe("name=test");
	});

	it("handles mixed types", () => {
		expect(
			buildQueryString({
				name: "test",
				page: 1,
				enabled: true,
				skip: undefined,
			}),
		).toBe("name=test&page=1&enabled=true");
	});

	it("handles special characters with proper encoding", () => {
		expect(buildQueryString({ query: "hello world", tag: "a&b" })).toBe(
			"query=hello+world&tag=a%26b",
		);
	});
});

describe("buildUrl", () => {
	it("returns path only when no params", () => {
		expect(buildUrl("/api/providers")).toBe("/api/providers");
	});

	it("appends query string when params provided", () => {
		expect(buildUrl("/api/models", { provider_id: "123" })).toBe(
			"/api/models?provider_id=123",
		);
	});

	it("returns path only when all params are undefined", () => {
		expect(buildUrl("/api/logs", { page: undefined, limit: undefined })).toBe(
			"/api/logs",
		);
	});

	it("handles special characters in path and params", () => {
		expect(buildUrl("/api/test", { query: "hello world", tag: "a&b" })).toBe(
			"/api/test?query=hello+world&tag=a%26b",
		);
	});

	it("handles multiple params", () => {
		expect(
			buildUrl("/api/stats", {
				period: "7d",
				exclude_deleted: true,
				metric: "tokens",
			}),
		).toBe("/api/stats?period=7d&exclude_deleted=true&metric=tokens");
	});
});

describe("setAdminToken", () => {
	beforeEach(() => {
		localStorage.clear();
		// Reset in-memory token by setting empty string
		setAdminToken("");
	});

	it("stores token in memory", () => {
		setAdminToken("my-test-token");
		expect(getAdminToken()).toBe("my-test-token");
	});

	it("overwrites previous token", () => {
		setAdminToken("first-token");
		setAdminToken("second-token");
		expect(getAdminToken()).toBe("second-token");
	});
});

describe("getAdminToken", () => {
	beforeEach(() => {
		localStorage.clear();
		setAdminToken("");
	});

	it("returns empty string when token set to empty", () => {
		setAdminToken("");
		expect(getAdminToken()).toBe("");
	});

	it("returns token after setAdminToken", () => {
		setAdminToken("stored-token");
		expect(getAdminToken()).toBe("stored-token");
	});

	it("returns in-memory value only (no localStorage fallback)", () => {
		// getAdminToken() returns only in-memory value
		// localStorage fallback is only in getAuthHeaders()
		localStorage.setItem("adminToken", "localStorage-token");
		setAdminToken("");
		expect(getAdminToken()).toBe("");
	});

	it("prefers memory over localStorage", () => {
		localStorage.setItem("adminToken", "storage-token");
		setAdminToken("memory-token");
		expect(getAdminToken()).toBe("memory-token");
	});
});

describe("getAuthHeaders", () => {
	beforeEach(() => {
		localStorage.clear();
		setAdminToken("");
	});

	it("throws error when no token set in memory or localStorage", () => {
		setAdminToken("");
		expect(() => getAuthHeaders()).toThrow("Admin token not set");
	});

	it("returns Authorization header with Bearer token when set", () => {
		setAdminToken("test-token");
		expect(getAuthHeaders()).toEqual({
			Authorization: "Bearer test-token",
			"Content-Type": "application/json",
		});
	});

	it("uses localStorage token when memory not set", () => {
		setAdminToken("");
		localStorage.setItem("adminToken", "storage-token");
		expect(getAuthHeaders()).toEqual({
			Authorization: "Bearer storage-token",
			"Content-Type": "application/json",
		});
	});

	it("prefers memory over localStorage", () => {
		localStorage.setItem("adminToken", "storage-token");
		setAdminToken("memory-token");
		expect(getAuthHeaders()).toEqual({
			Authorization: "Bearer memory-token",
			"Content-Type": "application/json",
		});
	});

	it("includes Content-Type header", () => {
		setAdminToken("token");
		const headers = getAuthHeaders();
		expect(headers["Content-Type"]).toBe("application/json");
	});
});

describe("Integration: setAdminToken + getAuthHeaders", () => {
	beforeEach(() => {
		localStorage.clear();
		setAdminToken("");
	});

	it("setAdminToken then getAuthHeaders returns correct header", () => {
		setAdminToken("integration-test-token");
		const headers = getAuthHeaders();
		expect(headers.Authorization).toBe("Bearer integration-test-token");
		expect(headers["Content-Type"]).toBe("application/json");
	});

	it("works with complex token values", () => {
		const complexToken =
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0";
		setAdminToken(complexToken);
		const headers = getAuthHeaders();
		expect(headers.Authorization).toBe(`Bearer ${complexToken}`);
	});
});
