import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	api,
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

describe("api.stats", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("get", () => {
		it("fetches stats without options", async () => {
			const mockStats = { total_requests: 100, total_tokens: 5000 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStats), { status: 200 }),
			);

			const result = await api.stats.get();

			expect(result).toEqual(mockStats);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("fetches stats with options", async () => {
			const mockStats = { total_requests: 200 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStats), { status: 200 }),
			);

			await api.stats.get({
				period: "7d",
				excludeDeleted: true,
				metric: "tokens",
			});

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats?period=7d&exclude_deleted=true&metric=tokens",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.stats.get()).rejects.toThrow(
				"Failed to fetch stats: 404 not found",
			);
		});
	});

	describe("getTimeSeries", () => {
		it("fetches time series stats", async () => {
			const mockData = { data: [{ timestamp: "2024-01-01", value: 100 }] };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockData), { status: 200 }),
			);

			const result = await api.stats.getTimeSeries();

			expect(result).toEqual(mockData);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/timeseries",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("fetches time series with period option", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [] }), { status: 200 }),
			);

			await api.stats.getTimeSeries({ period: "30d" });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/timeseries?period=30d",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("server error", { status: 500 }),
			);

			await expect(api.stats.getTimeSeries()).rejects.toThrow(
				"Failed to fetch time-series stats: 500 server error",
			);
		});
	});

	describe("getProviderDistribution", () => {
		it("fetches provider distribution stats", async () => {
			const mockData = { distribution: [{ provider: "openai", count: 50 }] };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockData), { status: 200 }),
			);

			const result = await api.stats.getProviderDistribution();

			expect(result).toEqual(mockData);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/provider-distribution",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("fetches with options", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ distribution: [] }), { status: 200 }),
			);

			await api.stats.getProviderDistribution({
				period: "7d",
				metric: "requests",
				excludeDeleted: true,
			});

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/provider-distribution?period=7d&metric=requests&exclude_deleted=true",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("bad request", { status: 400 }),
			);

			await expect(api.stats.getProviderDistribution()).rejects.toThrow(
				"Failed to fetch provider distribution: 400 bad request",
			);
		});
	});
});

describe("api.settings", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("get", () => {
		it("fetches settings", async () => {
			const mockSettings = { theme: "dark", language: "en" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockSettings), { status: 200 }),
			);

			const result = await api.settings.get();

			expect(result).toEqual(mockSettings);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.settings.get()).rejects.toThrow(
				"Failed to fetch settings: 404 not found",
			);
		});
	});

	describe("update", () => {
		it("updates settings", async () => {
			const newSettings = { theme: "light", language: "fr" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(newSettings), { status: 200 }),
			);

			const result = await api.settings.update(newSettings);

			expect(result).toEqual(newSettings);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(newSettings),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("validation failed", { status: 400 }),
			);

			await expect(api.settings.update({})).rejects.toThrow(
				"Failed to update settings: 400 validation failed",
			);
		});
	});
});

describe("api.version", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("getLatest", () => {
		it("fetches latest version", async () => {
			const mockVersion = { tag_name: "v1.2.3" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockVersion), { status: 200 }),
			);

			const result = await api.version.getLatest();

			expect(result).toEqual(mockVersion);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/version/latest",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("passes custom options to fetch", async () => {
			const mockVersion = { tag_name: "v1.2.4" };
			const customOptions: RequestInit = {
				cache: "no-cache",
				signal: AbortSignal.timeout(5000),
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockVersion), { status: 200 }),
			);

			await api.version.getLatest(customOptions);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/version/latest",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					cache: "no-cache",
					signal: expect.any(AbortSignal),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.version.getLatest()).rejects.toThrow(
				"Failed to fetch latest version: 404 not found",
			);
		});
	});
});

describe("api.virtualKeys", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches virtual keys list", async () => {
			const mockKeys = [
				{ id: "1", name: "key1", key: "vk_abc123" },
				{ id: "2", name: "key2", key: "vk_def456" },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockKeys), { status: 200 }),
			);

			const result = await api.virtualKeys.list();

			expect(result).toEqual(mockKeys);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.virtualKeys.list()).rejects.toThrow(
				"Failed to fetch virtual keys: 401 unauthorized",
			);
		});
	});

	describe("create", () => {
		it("creates a virtual key", async () => {
			const requestBody = {
				name: "new-key",
				rate_limit_rps: 10,
				rate_limit_burst: 20,
			};
			const mockResponse = {
				id: "3",
				...requestBody,
				key: "vk_new123",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.virtualKeys.create(
				requestBody.name,
				requestBody.rate_limit_rps,
				requestBody.rate_limit_burst,
			);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(requestBody),
				}),
			);
		});

		it("creates with null rate limits", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "4", name: "unlimited" }), {
					status: 200,
				}),
			);

			await api.virtualKeys.create("unlimited", null, null);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					body: JSON.stringify({
						name: "unlimited",
						rate_limit_rps: null,
						rate_limit_burst: null,
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("duplicate name", { status: 409 }),
			);

			await expect(api.virtualKeys.create("dup")).rejects.toThrow(
				"Failed to create virtual key: 409 duplicate name",
			);
		});
	});

	describe("get", () => {
		it("fetches a virtual key by id", async () => {
			const mockKey = { id: "1", name: "key1", key: "vk_abc123" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockKey), { status: 200 }),
			);

			const result = await api.virtualKeys.get("1");

			expect(result).toEqual(mockKey);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.virtualKeys.get("nonexistent")).rejects.toThrow(
				"Failed to fetch virtual key: 404 not found",
			);
		});
	});

	describe("update", () => {
		it("updates a virtual key", async () => {
			const updateData = {
				name: "updated-key",
				rate_limit_rps: 50,
			};
			const mockResponse = { id: "1", ...updateData, key: "vk_abc123" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.virtualKeys.update("1", updateData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(updateData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(
				api.virtualKeys.update("1", { name: "test" }),
			).rejects.toThrow("Failed to update virtual key: 404 not found");
		});
	});

	describe("delete", () => {
		it("deletes a virtual key", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.virtualKeys.delete("1");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.virtualKeys.delete("1")).rejects.toThrow(
				"Failed to delete virtual key",
			);
		});
	});
});

describe("api.system", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2024-01-15T10:30:00Z"));
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("fetches system stats with since parameter", async () => {
		const mockStats = {
			cpu_usage: 45.2,
			memory_usage: 67.8,
			disk_usage: 32.1,
		};
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify(mockStats), { status: 200 }),
		);

		const result = await api.system.get();

		// Compute expected midnight using the same local-timezone logic
		// as the implementation (new Date(year, month, date))
		const now = new Date();
		const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate());
		const expectedSince = encodeURIComponent(midnight.toISOString());

		expect(result).toEqual(mockStats);
		expect(globalThis.fetch).toHaveBeenCalledWith(
			`/api/system?since=${expectedSince}`,
			expect.objectContaining({
				headers: expect.objectContaining({
					Authorization: "Bearer test-token",
				}),
			}),
		);
	});

	it("throws on error response", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response("internal error", { status: 500 }),
		);

		await expect(api.system.get()).rejects.toThrow(
			"Failed to fetch system stats: 500 internal error",
		);
	});
});

describe("api.chat", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	const chatBody = {
		model: "gpt-4",
		stream: false,
		messages: [{ role: "user", content: "Hello" }],
		temperature: 0.7,
	};

	describe("completions", () => {
		it("sends chat completions request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ choices: [] }), { status: 200 }),
			);

			const result = await api.chat.completions(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/completions",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("model not found", { status: 404 }),
			);

			await expect(api.chat.completions(chatBody)).rejects.toThrow(
				"Chat failed: 404 model not found",
			);
		});
	});

	describe("chat", () => {
		it("sends chat request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "chat-123" }), { status: 200 }),
			);

			const result = await api.chat.chat(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/chat",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("passes signal to fetch when provided", async () => {
			const abortController = new AbortController();
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 200 }),
			);

			await api.chat.chat({ ...chatBody, signal: abortController.signal });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/chat",
				expect.objectContaining({
					signal: abortController.signal,
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("rate limited", { status: 429 }),
			);

			await expect(api.chat.chat(chatBody)).rejects.toThrow(
				"Chat failed: 429 rate limited",
			);
		});
	});

	describe("arena", () => {
		it("sends arena request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ winner: "model-a" }), { status: 200 }),
			);

			const result = await api.chat.arena(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/arena",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("passes signal to fetch when provided", async () => {
			const abortController = new AbortController();
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 200 }),
			);

			await api.chat.arena({ ...chatBody, signal: abortController.signal });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/arena",
				expect.objectContaining({
					signal: abortController.signal,
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("arena unavailable", { status: 503 }),
			);

			await expect(api.chat.arena(chatBody)).rejects.toThrow(
				"Arena failed: 503 arena unavailable",
			);
		});
	});
});

describe("api.failoverGroups", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches failover groups list", async () => {
			const mockGroups = [
				{ id: "1", name: "group1", models: ["model-a"] },
				{ id: "2", name: "group2", models: ["model-b"] },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockGroups), { status: 200 }),
			);

			const result = await api.failoverGroups.list();

			expect(result).toEqual(mockGroups);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.failoverGroups.list()).rejects.toThrow(
				"Failed to fetch failover groups: 401 unauthorized",
			);
		});
	});

	describe("get", () => {
		it("fetches a failover group by id", async () => {
			const mockGroup = {
				id: "1",
				name: "primary-group",
				models: ["openai/gpt-4", "anthropic/claude-3"],
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockGroup), { status: 200 }),
			);

			const result = await api.failoverGroups.get("1");

			expect(result).toEqual(mockGroup);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.failoverGroups.get("nonexistent")).rejects.toThrow(
				"Failed to fetch failover group: 404 not found",
			);
		});
	});

	describe("create", () => {
		it("creates a failover group", async () => {
			const createData = {
				display_model: "hotel/my-group",
				display_name: "new-group",
				description: "Test failover group",
				entry_ids: ["model-a", "model-b"],
			};
			const mockResponse = { id: "3", ...createData };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.failoverGroups.create(createData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(createData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("duplicate name", { status: 409 }),
			);

			await expect(
				api.failoverGroups.create({
					display_model: "hotel/dup",
					entry_ids: [],
				}),
			).rejects.toThrow("Failed to create failover group: 409 duplicate name");
		});
	});

	describe("update", () => {
		it("updates a failover group", async () => {
			const updateData = {
				display_name: "updated-group",
				description: "Updated description",
				group_enabled: true,
				priority_order: ["model-c"],
			};
			const mockResponse = { id: "1", ...updateData };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.failoverGroups.update("1", updateData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(updateData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(
				api.failoverGroups.update("1", {
					display_name: "test",
					group_enabled: false,
				}),
			).rejects.toThrow("Failed to update failover group: 404 not found");
		});
	});

	describe("delete", () => {
		it("deletes a failover group", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.failoverGroups.delete("1");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.failoverGroups.delete("1")).rejects.toThrow(
				"Failed to delete failover group",
			);
		});
	});

	describe("sync", () => {
		it("syncs failover groups", async () => {
			const mockResult = { synced: 5, failed: 0 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.failoverGroups.sync();

			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/sync",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("sync failed", { status: 500 }),
			);

			await expect(api.failoverGroups.sync()).rejects.toThrow(
				"Failed to sync failover groups: 500 sync failed",
			);
		});
	});

	describe("candidates", () => {
		it("fetches candidate models", async () => {
			const mockCandidates = [
				{ model_id: "gpt-4", provider_id: "openai" },
				{ model_id: "claude-3", provider_id: "anthropic" },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockCandidates), { status: 200 }),
			);

			const result = await api.failoverGroups.candidates();

			expect(result).toEqual(mockCandidates);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/candidates",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("service unavailable", { status: 503 }),
			);

			await expect(api.failoverGroups.candidates()).rejects.toThrow(
				"Failed to fetch candidates: 503 service unavailable",
			);
		});
	});
});

describe("api.backups", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches backups list", async () => {
			const mockBackups = [
				{ filename: "backup-2024-01-01.sql", size: 1024 },
				{ filename: "backup-2024-01-02.sql", size: 2048 },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBackups), { status: 200 }),
			);

			const result = await api.backups.list();

			expect(result).toEqual(mockBackups);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.backups.list()).rejects.toThrow(
				"Failed to fetch backups: 401 unauthorized",
			);
		});
	});

	describe("create", () => {
		it("creates a backup", async () => {
			const mockBackup = {
				filename: "backup-2024-01-15.sql",
				size: 0,
				created_at: "2024-01-15T10:00:00Z",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockBackup), { status: 200 }),
			);

			const result = await api.backups.create();

			expect(result).toEqual(mockBackup);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("disk full", { status: 507 }),
			);

			await expect(api.backups.create()).rejects.toThrow(
				"Failed to create backup: 507 disk full",
			);
		});
	});

	describe("downloadUrl", () => {
		it("returns encoded download URL", () => {
			const filename = "backup with spaces.sql";
			const url = api.backups.downloadUrl(filename);

			expect(url).toBe("/api/backups/backup%20with%20spaces.sql");
		});

		it("encodes special characters in filename", () => {
			const filename = "backup&2024.sql";
			const url = api.backups.downloadUrl(filename);

			expect(url).toBe("/api/backups/backup%262024.sql");
		});
	});

	describe("delete", () => {
		it("deletes a backup", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.backups.delete("backup-2024-01-01.sql");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/backup-2024-01-01.sql",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("encodes filename in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.backups.delete("backup with spaces.sql");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/backup%20with%20spaces.sql",
				expect.anything(),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.backups.delete("backup.sql")).rejects.toThrow(
				"Failed to delete backup",
			);
		});
	});

	describe("restore", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("restores from backup file using FormData", async () => {
			const mockFile = new File(["dummy content"], "backup.sql", {
				type: "application/sql",
			});
			const adminToken = "restore-token-123";
			localStorage.setItem("adminToken", adminToken);

			const mockResponse = { migration_count: 5, known_count: 10 };
			const fetchSpy = vi
				.spyOn(globalThis, "fetch")
				.mockImplementation(
					async () =>
						new Response(JSON.stringify(mockResponse), { status: 200 }),
				);

			const result = await api.backups.restore(mockFile, adminToken);

			expect(result).toEqual(mockResponse);

			const callArgs = fetchSpy.mock.calls[0];
			expect(callArgs[0]).toBe("/api/backups/restore");

			const options = callArgs[1] as RequestInit;
			expect(options.method).toBe("POST");
			expect(options.headers).toEqual({
				Authorization: `Bearer ${adminToken}`,
			});
			expect(options.body).toBeInstanceOf(FormData);

			const formData = options.body as FormData;
			expect(formData.get("dump")).toBe(mockFile);
			expect(formData.get("admin_token")).toBe(adminToken);
		});

		it("uses localStorage token for Authorization", async () => {
			const mockFile = new File(["content"], "test.sql");
			const storedToken = "stored-restore-token";
			localStorage.setItem("adminToken", storedToken);

			vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 1, known_count: 1 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, storedToken);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/backups/restore",
				expect.objectContaining({
					headers: {
						Authorization: `Bearer ${storedToken}`,
					},
				}),
			);
		});

		it("uses localStorage token for Authorization, not the parameter", async () => {
			const mockFile = new File(["content"], "test.sql");
			const localStorageToken = "local-storage-token";
			const paramToken = "param-token";
			localStorage.setItem("adminToken", localStorageToken);

			const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 1, known_count: 1 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, paramToken);

			// Authorization header uses localStorage, not the parameter
			const options = fetchSpy.mock.calls[0][1] as RequestInit;
			expect(options.headers).toEqual({
				Authorization: `Bearer ${localStorageToken}`,
			});

			// FormData admin_token uses the parameter
			const formData = options.body as FormData;
			expect(formData.get("admin_token")).toBe(paramToken);
		});

		it("does not set Content-Type header", async () => {
			const mockFile = new File(["content"], "test.sql");
			vi.spyOn(globalThis, "fetch").mockImplementation(
				async () =>
					new Response(JSON.stringify({ migration_count: 0, known_count: 0 }), {
						status: 200,
					}),
			);

			await api.backups.restore(mockFile, "token");

			const callArgs = vi.mocked(globalThis.fetch).mock.calls[0];
			const options = callArgs[1] as RequestInit;
			expect(options.headers).not.toHaveProperty("Content-Type");
		});

		it("throws on error response", async () => {
			const mockFile = new File(["content"], "test.sql");
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("invalid dump", { status: 400 }),
			);

			await expect(api.backups.restore(mockFile, "token")).rejects.toThrow(
				"Restore failed: 400 invalid dump",
			);
		});
	});
});

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

describe("api.models", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches all models without provider_id", async () => {
			const mockModels = [{ id: "1", name: "gpt-4" }];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModels), { status: 200 }),
			);

			const result = await api.models.list();
			expect(result).toEqual(mockModels);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches models filtered by provider_id", async () => {
			const mockModels = [{ id: "1", name: "gpt-4" }];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModels), { status: 200 }),
			);

			const result = await api.models.list("provider-123");
			expect(result).toEqual(mockModels);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models?provider_id=provider-123",
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
			await expect(api.models.list()).rejects.toThrow(
				"Failed to fetch models: 404 not found",
			);
		});
	});

	describe("cursor", () => {
		it("fetches models with cursor pagination", async () => {
			const mockResponse = {
				data: [{ id: "1", name: "gpt-4" }],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.models.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/models/cursor?"),
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("includes optional params in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [], has_more: false }), {
					status: 200,
				}),
			);

			await api.models.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				sort_by: "name",
				sort_dir: "desc",
				provider_id: "prov-1",
				search: "gpt",
				capabilities: "vision",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/models/cursor?cursor=abc123&direction=before&limit=20&sort_by=name&sort_dir=desc&provider_id=prov-1&search=gpt&capabilities=vision",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.models.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch models (cursor): 500 error");
		});
	});

	describe("update", () => {
		it("updates a model with PATCH request", async () => {
			const mockModel = { id: "1", display_name: "Updated Model" };
			const data = { display_name: "Updated Model", enabled: true };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModel), { status: 200 }),
			);

			const result = await api.models.update("123", data);
			expect(result).toEqual(mockModel);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123",
				expect.objectContaining({
					method: "PATCH",
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
				api.models.update("123", { display_name: "Test" }),
			).rejects.toThrow("Failed to update model: 404 not found");
		});
	});

	describe("test", () => {
		it("tests a model with POST request", async () => {
			const mockResult = {
				success: true,
				ttft_ms: 150,
				duration_ms: 500,
				response: "Hello!",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.models.test("123");
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123/test",
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
				new Response("test failed", { status: 500 }),
			);
			await expect(api.models.test("123")).rejects.toThrow(
				"Test failed: 500 test failed",
			);
		});
	});

	describe("delete", () => {
		it("deletes a model with DELETE request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.models.delete("123");
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123",
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
			await expect(api.models.delete("123")).rejects.toThrow(
				"Failed to delete model",
			);
		});
	});
});

describe("api.logs", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches logs without params", async () => {
			const mockLogs = { entries: [], total: 0, page: 1, per_page: 20 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.logs.list();
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches logs with optional params", async () => {
			const mockLogs = { entries: [], total: 0, page: 1, per_page: 20 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.logs.list({
				page: 2,
				per_page: 50,
				model_id: "model-1",
				provider_id: "prov-1",
				status_code: "200",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_by: "timestamp",
				sort_dir: "desc",
			});
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs?"),
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
				new Response("error", { status: 500 }),
			);
			await expect(api.logs.list()).rejects.toThrow(
				"Failed to fetch logs: 500 error",
			);
		});
	});

	describe("purge", () => {
		it("purges logs older than specified date", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.logs.purge("2024-01-01");
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/purge",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify({ older_than: "2024-01-01" }),
				}),
			);
		});

		it("throws error with response text on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("database locked", { status: 500 }),
			);
			await expect(api.logs.purge("2024-01-01")).rejects.toThrow(
				"Failed to purge logs: 500 database locked",
			);
		});
	});

	describe("cursor", () => {
		it("fetches logs with cursor pagination", async () => {
			const mockResponse = {
				data: [],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.logs.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/cursor?"),
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("includes optional params in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [], has_more: false }), {
					status: 200,
				}),
			);

			await api.logs.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				model_id: "model-1",
				provider_id: "prov-1",
				status_code: "200",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_dir: "desc",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/logs/cursor?cursor=abc123&direction=before&limit=20",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.logs.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch logs (cursor): 500 error");
		});
	});

	describe("get", () => {
		it("fetches a log entry by id", async () => {
			const mockEntry = { id: "log-123", model_id: "gpt-4", status_code: 200 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockEntry), { status: 200 }),
			);

			const result = await api.logs.get("log-123");

			expect(result).toEqual(mockEntry);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/log-123",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("encodes the id in the URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "uuid-with/special" }), {
					status: 200,
				}),
			);

			await api.logs.get("uuid-with/special");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/uuid-with%2Fspecial",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.logs.get("nonexistent")).rejects.toThrow(
				"Failed to fetch log: 404 not found",
			);
		});
	});
});

describe("api.appLogs", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches app logs without params", async () => {
			const mockLogs = [
				{
					timestamp: "2024-01-01",
					level: "info",
					source: "test",
					message: "OK",
				},
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.appLogs.list();
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches app logs with optional params", async () => {
			const mockLogs = [
				{
					timestamp: "2024-01-01",
					level: "info",
					source: "test",
					message: "OK",
				},
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.appLogs.list({ limit: 50, after: "abc123" });
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app?limit=50&after=abc123",
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
				new Response("error", { status: 500 }),
			);
			await expect(api.appLogs.list()).rejects.toThrow(
				"Failed to fetch app logs: 500 error",
			);
		});
	});

	describe("purge", () => {
		it("purges app logs", async () => {
			const mockResult = { deleted: 100 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.appLogs.purge();
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app",
				expect.objectContaining({
					method: "DELETE",
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
			await expect(api.appLogs.purge()).rejects.toThrow(
				"Failed to purge app logs: 500 error",
			);
		});
	});

	describe("history", () => {
		it("fetches app log history without params", async () => {
			const mockHistory = {
				entries: [],
				total: 0,
				page: 1,
				per_page: 20,
				level_counts: { info: 10, error: 5 },
				source_counts: { api: 15 },
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockHistory), { status: 200 }),
			);

			const result = await api.appLogs.history();
			expect(result).toEqual(mockHistory);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app?history=true",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches app log history with optional params", async () => {
			const mockHistory = {
				entries: [],
				total: 0,
				page: 1,
				per_page: 20,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockHistory), { status: 200 }),
			);

			const result = await api.appLogs.history({
				level: "error",
				source: "api",
				search: "timeout",
				from: "2024-01-01",
				to: "2024-01-02",
				page: 2,
				per_page: 50,
				sort_by: "timestamp",
				sort_dir: "desc",
			});
			expect(result).toEqual(mockHistory);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/app?history=true&"),
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
				new Response("error", { status: 500 }),
			);
			await expect(api.appLogs.history()).rejects.toThrow(
				"Failed to fetch app log history: 500 error",
			);
		});
	});

	describe("cursor", () => {
		it("fetches app logs with cursor pagination", async () => {
			const mockResponse = {
				data: [],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.appLogs.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/app/cursor?"),
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("includes optional params in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [], has_more: false }), {
					status: 200,
				}),
			);

			await api.appLogs.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				level: "error",
				source: "api",
				search: "timeout",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_dir: "desc",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/logs/app/cursor?cursor=abc123&direction=before&limit=20",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.appLogs.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch app logs (cursor): 500 error");
		});
	});
});
