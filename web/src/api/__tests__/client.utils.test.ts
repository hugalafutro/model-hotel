import { beforeEach, describe, expect, it } from "vitest";
import {
	buildQueryString,
	buildUrl,
	clearAuth,
	getAuthHeaders,
	getCsrfToken,
	isAuthenticated,
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

function clearCsrfCookie() {
	document.cookie = "mh_csrf=; path=/; max-age=0";
}

describe("getCsrfToken / isAuthenticated", () => {
	beforeEach(clearCsrfCookie);

	it("reads the CSRF token from the mh_csrf cookie", () => {
		document.cookie = "mh_csrf=my-test-csrf; path=/";
		expect(getCsrfToken()).toBe("my-test-csrf");
		expect(isAuthenticated()).toBe(true);
	});

	it("reports unauthenticated with no cookie", () => {
		expect(getCsrfToken()).toBeNull();
		expect(isAuthenticated()).toBe(false);
	});

	it("decodes URL-encoded cookie values", () => {
		document.cookie = "mh_csrf=a%2Bb; path=/";
		expect(getCsrfToken()).toBe("a+b");
	});
});

describe("clearAuth", () => {
	beforeEach(clearCsrfCookie);

	it("expires the mh_csrf cookie so isAuthenticated flips false", () => {
		document.cookie = "mh_csrf=stored-csrf; path=/";
		expect(isAuthenticated()).toBe(true);
		clearAuth();
		expect(getCsrfToken()).toBeNull();
		expect(isAuthenticated()).toBe(false);
	});
});

describe("getAuthHeaders", () => {
	beforeEach(clearCsrfCookie);

	it("returns Content-Type plus the CSRF header, never a bearer token", () => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		expect(getAuthHeaders()).toEqual({
			"Content-Type": "application/json",
			"X-CSRF-Token": "test-csrf",
		});
	});

	it("omits the CSRF header when logged out", () => {
		expect(getAuthHeaders()).toEqual({ "Content-Type": "application/json" });
		expect(getAuthHeaders().Authorization).toBeUndefined();
	});

	it("works with complex CSRF token values", () => {
		const complex =
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0";
		document.cookie = `mh_csrf=${complex}; path=/`;
		expect(getAuthHeaders()["X-CSRF-Token"]).toBe(complex);
	});
});
