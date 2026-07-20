import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.demoLogin", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	it("fetches the demo login token from the unauthenticated endpoint", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ token: "demo-abc" }), { status: 200 }),
		);

		const result = await api.demoLogin.get();

		expect(result).toEqual({ token: "demo-abc" });
		expect(globalThis.fetch).toHaveBeenCalledWith("/api/demo-login", {
			credentials: "same-origin",
		});
	});

	it("throws on error response", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response("nope", { status: 500 }),
		);
		await expect(api.demoLogin.get()).rejects.toThrow(
			"Failed to fetch demo login: 500 nope",
		);
	});
});

describe("api.totp.login", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	it("posts the admin token and TOTP code, returning the session token", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify({ token: "session-xyz" }), { status: 200 }),
		);

		const result = await api.totp.login("admin-token", "123456");

		expect(result).toEqual({ token: "session-xyz" });
		expect(globalThis.fetch).toHaveBeenCalledWith(
			"/api/totp/login",
			expect.objectContaining({
				method: "POST",
				body: JSON.stringify({ token: "admin-token", code: "123456" }),
				headers: expect.objectContaining({
					"Content-Type": "application/json",
				}),
			}),
		);
	});

	it("throws on a rejected code", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response("invalid code", { status: 401 }),
		);
		await expect(api.totp.login("admin-token", "000000")).rejects.toThrow(
			"TOTP login failed: 401 invalid code",
		);
	});
});
