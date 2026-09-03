import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.discovery", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("status", () => {
		// The 60s badge poll must call status() with no args and hit the plain
		// URL: the server only stamps the last-reviewed marker on the ?review=1
		// variant, so a poll that accidentally sent it would collapse
		// flap_since_review to zero every 60 seconds.
		it("omits the review param by default", async () => {
			const mockStatus = {
				claims: [],
				group_claims: [],
				informational: [],
				claim_count: 0,
				informational_unseen: 0,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStatus), { status: 200 }),
			);

			const result = await api.discovery.status();
			expect(result).toEqual(mockStatus);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/discovery/status",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		// The modal-open fetch must explicitly opt in to stamping.
		it("sends review=1 when review is true", async () => {
			const mockStatus = {
				claims: [],
				group_claims: [],
				informational: [],
				claim_count: 0,
				informational_unseen: 0,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStatus), { status: 200 }),
			);

			const result = await api.discovery.status(true);
			expect(result).toEqual(mockStatus);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/discovery/status?review=1",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});
	});

	describe("dismiss", () => {
		it("posts model_ids to the provider's dismiss route", async () => {
			// No `dismissed` flag: the endpoint only stamps. A dismissal is reversed by
			// discovery sighting the model again, not by a second call.
			const mockResult = { dismissed: ["model-a", "model-b"], updated: 2 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.discovery.dismiss("prov-1", [
				"model-a",
				"model-b",
			]);
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/discovery/prov-1/dismiss",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify({ model_ids: ["model-a", "model-b"] }),
				}),
			);
		});
	});
});
