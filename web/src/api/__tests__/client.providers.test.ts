import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, setAdminToken } from "../client";

describe("api.providers", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches all providers", async () => {
			const mockProviders = [{ id: "1", name: "Test" }];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockProviders), { status: 200 }),
			);

			const result = await api.providers.list();
			expect(result).toEqual(mockProviders);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.providers.list()).rejects.toThrow(
				"Failed to fetch providers: 404 not found",
			);
		});
	});

	describe("create", () => {
		it("creates a provider with POST request", async () => {
			const mockProvider = { id: "1", name: "Created" };
			const data = {
				name: "Test",
				base_url: "https://api.example.com",
				api_key: "sk-123",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockProvider), { status: 201 }),
			);

			const result = await api.providers.create(data);
			expect(result).toEqual(mockProvider);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(data),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("bad request", { status: 400 }),
			);
			await expect(
				api.providers.create({
					name: "Test",
					base_url: "https://api.example.com",
					api_key: "sk-123",
				}),
			).rejects.toThrow("Failed to create provider: 400 bad request");
		});
	});

	describe("delete", () => {
		it("deletes a provider with DELETE request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.providers.delete("123");
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);
			await expect(api.providers.delete("123")).rejects.toThrow(
				"Failed to delete provider",
			);
		});
	});

	describe("update", () => {
		it("updates a provider with PUT request", async () => {
			const mockProvider = { id: "1", name: "Updated" };
			const data = { name: "New Name", enabled: true };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockProvider), { status: 200 }),
			);

			const result = await api.providers.update("123", data);
			expect(result).toEqual(mockProvider);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(data),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(
				api.providers.update("123", { name: "Test" }),
			).rejects.toThrow("Failed to update provider: 404 not found");
		});
	});

	describe("discover", () => {
		it("discovers models for a provider", async () => {
			const mockResult = { discovered: 42 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.providers.discover("123");
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123/discover",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.providers.discover("123")).rejects.toThrow(
				"Failed to discover models: 500 error",
			);
		});
	});

	describe("discoverAll", () => {
		it("discovers models for all providers", async () => {
			const mockResult = {
				succeeded: 2,
				failed: 1,
				discovered: 10,
				results: [
					{ provider_name: "Test", discovered: 10 },
					{ provider_name: "Fail", error: "timeout" },
				],
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.providers.discoverAll();
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/discover-all",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.providers.discoverAll()).rejects.toThrow(
				"Failed to discover all: 500 error",
			);
		});
	});

	describe("refreshQuotas", () => {
		it("refreshes quotas for all providers", async () => {
			const mockResult = {
				refreshed: 2,
				failed: 1,
				skipped: 0,
				results: [
					{ provider_name: "Test", provider_type: "openai", refreshed: true },
				],
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.providers.refreshQuotas();
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/refresh-quotas",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.providers.refreshQuotas()).rejects.toThrow(
				"Failed to refresh quotas: 500 error",
			);
		});
	});

	describe("getUsage", () => {
		it("fetches usage for a provider", async () => {
			const mockUsage = { remaining_credits: 100 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockUsage), { status: 200 }),
			);

			const result = await api.providers.getUsage("123");
			expect(result).toEqual(mockUsage);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123/usage",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.providers.getUsage("123")).rejects.toThrow(
				"Failed to fetch usage: 404 not found",
			);
		});
	});

	describe("getBalance", () => {
		it("fetches balance for a provider", async () => {
			const mockBalance = { balance: 50.0, currency: "USD" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBalance), { status: 200 }),
			);

			const result = await api.providers.getBalance("123");
			expect(result).toEqual(mockBalance);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123/balance",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.providers.getBalance("123")).rejects.toThrow(
				"Failed to fetch balance: 404 not found",
			);
		});
	});

	describe("getOpenRouterBalance", () => {
		it("fetches OpenRouter balance for a provider", async () => {
			const mockBalance = { total_credits: 100, used_credits: 25 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBalance), { status: 200 }),
			);

			const result = await api.providers.getOpenRouterBalance("123");
			expect(result).toEqual(mockBalance);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123/usage",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.providers.getOpenRouterBalance("123")).rejects.toThrow(
				"Failed to fetch OpenRouter balance: 404 not found",
			);
		});
	});

	describe("getOllamaCloudAccount", () => {
		it("fetches Ollama Cloud account for a provider", async () => {
			const mockAccount = { account_id: "acc-123", status: "active" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockAccount), { status: 200 }),
			);

			const result = await api.providers.getOllamaCloudAccount("123");
			expect(result).toEqual(mockAccount);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/providers/123/account",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.providers.getOllamaCloudAccount("123")).rejects.toThrow(
				"Failed to fetch Ollama Cloud account: 404 not found",
			);
		});
	});
});
