import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, api } from "../client";

describe("api.alert", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("test", () => {
		it("sends no body and no Content-Type header when called with no arguments", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ ok: true }), { status: 200 }),
			);

			const result = await api.alert.test();

			expect(result).toEqual({ ok: true });
			const [url, init] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
				.calls[0] as [string, RequestInit];
			expect(url).toBe("/api/alert/test");
			expect(init.method).toBe("POST");
			expect("body" in init).toBe(false);
			expect(
				(init.headers as Record<string, string>)["Content-Type"],
			).toBeUndefined();
			expect((init.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
				"test-csrf",
			);
		});

		it("posts an explicit api_url/targets body with a JSON Content-Type", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ ok: true }), { status: 200 }),
			);

			await api.alert.test({ api_url: "http://a", targets: ["x://y"] });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/alert/test",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify({ api_url: "http://a", targets: ["x://y"] }),
				}),
			);
		});

		it("rejects with a coded ApiError on a 502 {code, error} body", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ code: "deliver_failed", error: "x" }), {
					status: 502,
				}),
			);

			try {
				await api.alert.test({ api_url: "http://a", targets: ["x://y"] });
				expect.unreachable();
			} catch (err) {
				expect(err).toBeInstanceOf(ApiError);
				expect((err as ApiError).code).toBe("deliver_failed");
				expect((err as ApiError).status).toBe(502);
				expect((err as ApiError).message).toBe(
					"Test notification failed: 502 x",
				);
			}
		});

		it("leaves code undefined on a plain-text error body", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("internal error", { status: 500 }),
			);

			await expect(api.alert.test()).rejects.toMatchObject({
				code: undefined,
				status: 500,
			});
		});
	});

	// fetchOK is shared by every api.* call; these exercise its coded-body
	// fallbacks through api.alert.test() since alert already has full coverage
	// of the happy path above.
	describe("fetchOK error-body fallbacks", () => {
		it("keeps the raw JSON in the message and leaves code undefined for a JSON body with no string code (e.g. a 401 auth body)", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ totp_required: true }), {
					status: 401,
				}),
			);

			await expect(api.alert.test()).rejects.toMatchObject({
				code: undefined,
				status: 401,
				message: 'Test notification failed: 401 {"totp_required":true}',
			});
		});

		it("falls back to the raw text and leaves code undefined for a body that merely starts with '{' but isn't valid JSON", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("{not json", { status: 500 }),
			);

			await expect(api.alert.test()).rejects.toMatchObject({
				code: undefined,
				status: 500,
				message: "Test notification failed: 500 {not json",
			});
		});

		it("falls back to the raw text (not an empty string) when a coded body omits `error`", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ code: "x" }), { status: 502 }),
			);

			await expect(api.alert.test()).rejects.toMatchObject({
				code: "x",
				status: 502,
				message: 'Test notification failed: 502 {"code":"x"}',
			});
		});
	});

	describe("probe", () => {
		it("posts api_url and returns the AlertStatus", async () => {
			const status = {
				configured: true,
				reachable: true,
				healthy: true,
				detail: "ok",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(status), { status: 200 }),
			);

			const result = await api.alert.probe("http://a");

			expect(result).toEqual(status);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/alert/probe",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify({ api_url: "http://a" }),
				}),
			);
		});

		it("surfaces a reason code from a 400 response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(
					JSON.stringify({ code: "invalid_url", error: "bad url" }),
					{ status: 400 },
				),
			);

			await expect(api.alert.probe(" ")).rejects.toMatchObject({
				code: "invalid_url",
				status: 400,
			});
		});
	});

	describe("targets", () => {
		it("fetches the saved destinations", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ targets: ["ntfys://a/b"] }), {
					status: 200,
				}),
			);

			const result = await api.alert.targets();

			expect(result).toEqual({ targets: ["ntfys://a/b"] });
			// GET is a safe method, so fetchOK strips X-CSRF-Token before the call
			// even though getAuthHeaders() set it.
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/alert/targets",
				expect.objectContaining({
					headers: expect.not.objectContaining({
						"X-CSRF-Token": expect.anything(),
					}),
				}),
			);
		});

		it("rejects with an undecryptable code on a 500", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ code: "undecryptable", error: "boom" }), {
					status: 500,
				}),
			);

			await expect(api.alert.targets()).rejects.toMatchObject({
				code: "undecryptable",
				status: 500,
			});
		});
	});
});
