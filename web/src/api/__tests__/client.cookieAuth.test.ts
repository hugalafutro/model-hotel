import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	api,
	clearAuth,
	getAuthHeaders,
	getCsrfToken,
	isAuthenticated,
} from "../client";

// Clear every cookie so each test starts from a known auth state.
function clearAllCookies() {
	for (const part of document.cookie.split(";")) {
		const name = part.split("=")[0]?.trim();
		if (name) document.cookie = `${name}=; path=/; max-age=0`;
	}
}

describe("client cookie auth", () => {
	beforeEach(() => {
		clearAllCookies();
		vi.restoreAllMocks();
	});

	it("reads the CSRF token from the mh_csrf cookie", () => {
		document.cookie = "mh_csrf=csrf-abc; path=/";
		expect(getCsrfToken()).toBe("csrf-abc");
		expect(isAuthenticated()).toBe(true);
	});

	it("reports unauthenticated when the mh_csrf cookie is absent", () => {
		expect(getCsrfToken()).toBeNull();
		expect(isAuthenticated()).toBe(false);
	});

	it("clearAuth expires the mh_csrf cookie", () => {
		document.cookie = "mh_csrf=csrf-abc; path=/";
		expect(isAuthenticated()).toBe(true);
		clearAuth();
		expect(getCsrfToken()).toBeNull();
		expect(isAuthenticated()).toBe(false);
	});

	it("getAuthHeaders carries the CSRF token, never an Authorization bearer", () => {
		document.cookie = "mh_csrf=csrf-abc; path=/";
		const headers = getAuthHeaders();
		expect(headers["X-CSRF-Token"]).toBe("csrf-abc");
		expect(headers["Content-Type"]).toBe("application/json");
		expect(headers.Authorization).toBeUndefined();
	});

	it("sends X-CSRF-Token on mutating requests and same-origin credentials, never a bearer token", async () => {
		document.cookie = "mh_csrf=csrf-abc; path=/";
		const seen: Array<{ method: string; init?: RequestInit }> = [];
		vi.spyOn(globalThis, "fetch").mockImplementation((async (
			_url: string,
			init?: RequestInit,
		) => {
			seen.push({ method: init?.method ?? "GET", init });
			return new Response("{}", { status: 200 });
		}) as typeof fetch);

		await api.providers.list(); // GET
		await api.providers.create({
			name: "x",
			base_url: "https://api.example.com",
			api_key: "sk-1",
		}); // POST

		const get = seen.find((s) => s.method === "GET");
		const post = seen.find((s) => s.method === "POST");
		expect(get?.init?.credentials).toBe("same-origin");
		expect(post?.init?.credentials).toBe("same-origin");
		const getHeaders = new Headers(get?.init?.headers);
		const postHeaders = new Headers(post?.init?.headers);
		expect(getHeaders.get("Authorization")).toBeNull();
		expect(postHeaders.get("Authorization")).toBeNull();
		expect(postHeaders.get("X-CSRF-Token")).toBe("csrf-abc");
	});
});
