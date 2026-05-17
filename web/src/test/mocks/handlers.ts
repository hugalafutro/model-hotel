import { HttpResponse, http, type RequestHandler } from "msw";
import type {
	BackupEntry,
	FailoverGroup,
	FailoverListResponse,
	LogsResponse,
	Model,
	Provider,
	VirtualKey,
} from "../../api/types";
import {
	mockBackupEntry,
	mockFailoverGroup,
	mockModel,
	mockProvider,
	mockStats,
	mockSystemStats,
	mockVirtualKey,
} from "./data";

// Helper to check for valid auth header
function hasValidAuth(request: Request): boolean {
	const auth = request.headers.get("Authorization");
	return auth?.startsWith("Bearer ") ?? false;
}

// Factory function to create fresh mock data arrays for each test.
// This prevents state leaking between tests - any mutation (e.g. POST /api/providers
// pushing to the array) is isolated to a single test run.
function createStore() {
	return {
		providers: [mockProvider] as Provider[],
		models: [mockModel] as Model[],
		virtualKeys: [mockVirtualKey] as VirtualKey[],
		failoverGroups: [mockFailoverGroup] as FailoverGroup[],
		backups: [mockBackupEntry] as BackupEntry[],
	};
}

// Current store instance - reset between tests via resetStore()
let store = createStore();

export function resetStore(): void {
	store = createStore();
}

export const handlers: RequestHandler[] = [
	// ── Providers ─────────────────────────────────────────────────────────
	http.get("/api/providers", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(store.providers);
	}),

	http.post("/api/providers", async ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = await request.json();
		const newProvider: Provider = {
			...mockProvider,
			id: `provider-${Date.now()}`,
			name: (body as { name?: string }).name ?? "New Provider",
			base_url:
				(body as { base_url?: string }).base_url ??
				"https://api.example.com/v1",
			created_at: new Date().toISOString(),
			updated_at: new Date().toISOString(),
		};
		store.providers.push(newProvider);
		return HttpResponse.json(newProvider, { status: 201 });
	}),

	http.delete("/api/providers/:id", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return new HttpResponse(null, { status: 204 });
	}),

	http.put("/api/providers/:id", async ({ request, params }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = (await request.json()) as Partial<Provider>;
		const provider = store.providers.find((p) => p.id === params.id);
		if (!provider) {
			return HttpResponse.json({ error: "Not found" }, { status: 404 });
		}
		const updated: Provider = {
			...provider,
			...body,
			updated_at: new Date().toISOString(),
		};
		return HttpResponse.json(updated);
	}),

	http.post("/api/providers/:id/discover", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ discovered: 0 });
	}),

	http.post("/api/providers/discover-all", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({
			succeeded: 0,
			failed: 0,
			discovered: 0,
			results: [],
		});
	}),

	http.post("/api/providers/refresh-quotas", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({
			refreshed: 0,
			failed: 0,
			skipped: 0,
			results: [],
		});
	}),

	http.get("/api/providers/:id/usage", ({ request, params }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		// Return different mock responses based on provider type
		const providerId = params.id as string;

		// Check if it's a Z.ai provider (for testing purposes, check if ID contains 'zai')
		if (providerId.includes("zai")) {
			return HttpResponse.json({
				code: 0,
				msg: "success",
				success: true,
				data: {
					level: "basic",
					limits: [
						{
							type: "TOKENS_LIMIT",
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
						{
							type: "TOKENS_LIMIT",
							unit: 6,
							number: 50000,
							usage: 25000,
							currentValue: 25000,
							remaining: 25000,
							percentage: 50,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
					],
				},
			});
		}

		// Check if it's an OpenRouter provider (for testing purposes, check if ID contains 'openrouter')
		if (providerId.includes("openrouter")) {
			return HttpResponse.json({
				label: "OpenRouter",
				limit: null,
				limit_reset: null,
				limit_remaining: null,
				usage: 100000,
				usage_daily: 10000,
				usage_weekly: 50000,
				usage_monthly: 100000,
				credits_total: 1000000,
				credits_used: 100000,
				credits_remaining: 900000,
				is_free_tier: false,
			});
		}

		// Return a mock NanoGPTUsage response for other providers
		return HttpResponse.json({
			active: true,
			provider: "nanogpt",
			providerStatus: "active",
			providerStatusRaw: "active",
			stripeSubscriptionId: "sub_test123",
			cancellationReason: null,
			canceledAt: null,
			endedAt: null,
			cancelAt: null,
			cancelAtPeriodEnd: false,
			limits: {
				weeklyInputTokens: 1000000,
				dailyInputTokens: 200000,
				dailyImages: 100,
			},
			allowOverage: false,
			period: {
				currentPeriodEnd: new Date(
					Date.now() + 7 * 24 * 60 * 60 * 1000,
				).toISOString(),
			},
			dailyImages: {
				used: 10,
				remaining: 90,
				percentUsed: 10,
				resetAt: Date.now() + 24 * 60 * 60 * 1000,
			},
			dailyInputTokens: {
				used: 50000,
				remaining: 150000,
				percentUsed: 25,
				resetAt: Date.now() + 24 * 60 * 60 * 1000,
			},
			weeklyInputTokens: {
				used: 200000,
				remaining: 800000,
				percentUsed: 20,
				resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
			},
			state: "active",
			graceUntil: null,
		});
	}),

	http.get("/api/providers/:id/balance", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		// Return a mock DeepSeekBalance response
		return HttpResponse.json({
			is_available: true,
			balance_infos: [
				{
					currency: "CNY",
					total_balance: "100.00",
					granted_balance: "50.00",
					topped_up_balance: "50.00",
				},
			],
		});
	}),

	http.get("/api/providers/:id/account", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		// Return a mock OllamaCloudAccount response
		return HttpResponse.json({
			id: "ollama-account-1",
			email: "test@example.com",
			name: "Test User",
			plan: "pro",
			customer_id: { string: "cus_test123", valid: true },
			subscription_id: { string: "sub_test123", valid: true },
			subscription_period_start: {
				time: new Date().toISOString(),
				valid: true,
			},
			subscription_period_end: {
				time: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString(),
				valid: true,
			},
			suspended_at: { time: "", valid: false },
		});
	}),

	// ── Models ────────────────────────────────────────────────────────────
	http.get("/api/models", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(store.models);
	}),

	http.patch("/api/models/:id", async ({ request, params }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = (await request.json()) as Partial<Model>;
		const model = store.models.find((m) => m.id === params.id);
		if (!model) {
			return HttpResponse.json({ error: "Not found" }, { status: 404 });
		}
		const updated: Model = {
			...model,
			...body,
		};
		return HttpResponse.json(updated);
	}),

	http.delete("/api/models/:id", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return new HttpResponse(null, { status: 204 });
	}),

	// ── Logs ──────────────────────────────────────────────────────────────
	http.get("/api/logs", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const response: LogsResponse = {
			entries: [],
			total: 0,
			page: 1,
			per_page: 25,
		};
		return HttpResponse.json(response);
	}),

	http.delete("/api/logs/purge", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return new HttpResponse(null, { status: 204 });
	}),

	http.get("/api/logs/app", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const url = new URL(request.url);
		if (url.searchParams.get("history") === "true") {
			return HttpResponse.json({
				entries: [],
				total: 0,
				page: 1,
				per_page: 25,
				level_counts: {},
				source_counts: {},
			});
		}
		return HttpResponse.json([]);
	}),

	http.delete("/api/logs/app", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ deleted: 0 });
	}),

	// ── Stats ─────────────────────────────────────────────────────────────
	http.get("/api/stats", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(mockStats);
	}),

	http.get("/api/stats/timeseries", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ points: [] });
	}),

	http.get("/api/stats/provider-distribution", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ items: [] });
	}),

	// ── Settings ──────────────────────────────────────────────────────────
	http.get("/api/settings", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ app_version: "v0.0.0-test" });
	}),

	http.put("/api/settings", async ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = await request.json();
		return HttpResponse.json(body as Record<string, string>);
	}),

	// ── System ────────────────────────────────────────────────────────────
	http.get("/api/system", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(mockSystemStats);
	}),

	// ── Virtual Keys ──────────────────────────────────────────────────────
	http.get("/api/virtual-keys", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(store.virtualKeys);
	}),

	http.post("/api/virtual-keys", async ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = await request.json();
		const newKey: VirtualKey = {
			...mockVirtualKey,
			id: `vk-${Date.now()}`,
			name: (body as { name?: string }).name || "New Key",
			created_at: new Date().toISOString(),
		};
		store.virtualKeys.push(newKey);
		return HttpResponse.json(newKey, { status: 201 });
	}),

	http.put("/api/virtual-keys/:id", async ({ request, params }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = (await request.json()) as Partial<VirtualKey>;
		const key = store.virtualKeys.find((k) => k.id === params.id);
		if (!key) {
			return HttpResponse.json({ error: "Not found" }, { status: 404 });
		}
		const updated: VirtualKey = {
			...key,
			...body,
		};
		return HttpResponse.json(updated);
	}),

	http.delete("/api/virtual-keys/:id", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return new HttpResponse(null, { status: 204 });
	}),

	// ── Failover Groups ───────────────────────────────────────────────────
	http.get("/api/failover-groups", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const response: FailoverListResponse = {
			groups: store.failoverGroups,
			last_synced_at: null,
		};
		return HttpResponse.json(response);
	}),

	http.post("/api/failover-groups", async ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const body = await request.json();
		const newGroup: FailoverGroup = {
			...mockFailoverGroup,
			id: `fg-${Date.now()}`,
			display_model:
				(body as { display_model?: string }).display_model || "hotel/new-model",
			display_name:
				(body as { display_name?: string }).display_name || "New Group",
			created_at: new Date().toISOString(),
			updated_at: new Date().toISOString(),
		};
		store.failoverGroups.push(newGroup);
		return HttpResponse.json(newGroup, { status: 201 });
	}),

	http.get("/api/failover-groups/candidates", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json([]);
	}),

	http.post("/api/failover-groups/sync", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json({ synced: 0 });
	}),

	// ── Backups ───────────────────────────────────────────────────────────
	http.get("/api/backups", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return HttpResponse.json(store.backups);
	}),

	http.post("/api/backups", async ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		const newBackup: BackupEntry = {
			...mockBackupEntry,
			filename: `backup-${new Date().toISOString()}.sql.gz`,
			created_at: new Date().toISOString(),
		};
		store.backups.push(newBackup);
		return HttpResponse.json(newBackup, { status: 201 });
	}),

	http.delete("/api/backups/:filename", ({ request }) => {
		if (!hasValidAuth(request)) {
			return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
		}
		return new HttpResponse(null, { status: 204 });
	}),

	// ── SSE Events ────────────────────────────────────────────────────────
	http.get("/api/events", () => {
		// SSE endpoint - return empty response to suppress unhandled request warnings
		return new HttpResponse(null, { status: 200 });
	}),

	// ── GitHub API (version check) ────────────────────────────────────────
	http.get(
		"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest",
		() => {
			return HttpResponse.json({ tag_name: "v0.2" });
		},
	),

	// ── Catch-all handlers (must be last: MSW is first-match-wins) ────────
	// These return 503 for chat/arena endpoints when no specific handler is
	// registered via server.use(). They suppress MSW "unhandled request" warnings.
	// Specific handlers from mockChatStream/mockArenaStream are prepended by
	// server.use() and will match before these catch-alls.
	http.post("/api/chat/chat", () =>
		HttpResponse.json(
			{ error: "No chat handler configured for this test" },
			{ status: 503 },
		),
	),
	http.post("/api/chat/arena", () =>
		HttpResponse.json(
			{ error: "No arena handler configured for this test" },
			{ status: 503 },
		),
	),
];
