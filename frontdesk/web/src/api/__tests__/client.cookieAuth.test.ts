import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	api,
	getCsrfToken,
	hasSession,
	notifyUnauthorized,
	onUnauthorized,
} from "../client";

const fetchMock = vi.fn();

beforeEach(() => {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockClear();
	// mockImplementation (not mockResolvedValue) so every call gets its own
	// Response instance; a Response body can only be read once, and a test that
	// issues two calls would otherwise throw on the second .json().
	fetchMock.mockImplementation(
		async () =>
			new Response("{}", {
				status: 200,
				headers: { "content-type": "application/json" },
			}),
	);
	document.cookie = "fd_csrf=csrf123; path=/";
});

afterEach(() => {
	vi.unstubAllGlobals();
	document.cookie = "fd_csrf=; path=/; max-age=0";
});

describe("cookie auth client", () => {
	it("reads the fd_csrf cookie and reports a session", () => {
		expect(getCsrfToken()).toBe("csrf123");
		expect(hasSession()).toBe(true);
	});

	it("sends same-origin credentials and no Authorization header", async () => {
		await api.listMembers();
		const [, init] = fetchMock.mock.calls[0];
		expect(init.credentials).toBe("same-origin");
		expect(new Headers(init.headers).get("Authorization")).toBeNull();
	});

	it("attaches X-CSRF-Token on mutations but not on reads", async () => {
		await api.listMembers();
		expect(
			new Headers(fetchMock.mock.calls[0][1].headers).get("X-CSRF-Token"),
		).toBeNull();
		await api.refreshQuota();
		expect(
			new Headers(fetchMock.mock.calls[1][1].headers).get("X-CSRF-Token"),
		).toBe("csrf123");
	});

	it("adminExchange posts the admin token", async () => {
		await api.adminExchange("tok");
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("/api/auth/admin-exchange");
		expect(JSON.parse(init.body)).toEqual({ admin_token: "tok" });
	});

	it("a 401 clears the csrf hint and notifies listeners", async () => {
		const seen = vi.fn();
		onUnauthorized(seen);
		fetchMock.mockResolvedValueOnce(new Response("nope", { status: 401 }));
		await expect(api.listMembers()).rejects.toMatchObject({ status: 401 });
		expect(seen).toHaveBeenCalled();
		expect(hasSession()).toBe(false);
	});

	it("notifyUnauthorized is callable directly (SSE path)", () => {
		notifyUnauthorized();
		expect(hasSession()).toBe(false);
	});
});
