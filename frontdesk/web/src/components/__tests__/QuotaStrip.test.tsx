import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { QuotaStrip } from "../QuotaStrip";

const nano: QuotaSnapshot = {
	provider_name: "nano",
	type: "nanogpt",
	kind: "usage",
	payload: {
		active: true,
		provider: "nanogpt",
		providerStatus: "active",
		cancelAtPeriodEnd: false,
		limits: {
			weeklyInputTokens: 1000,
			dailyInputTokens: null,
			dailyImages: null,
		},
		allowOverage: false,
		period: { currentPeriodEnd: "2026-08-01T00:00:00Z" },
		dailyImages: null,
		dailyInputTokens: null,
		weeklyInputTokens: {
			used: 400,
			remaining: 600,
			percentUsed: 0.4,
			resetAt: 0,
		},
	},
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

const deepseek: QuotaSnapshot = {
	provider_name: "ds",
	type: "deepseek",
	kind: "balance",
	payload: {
		is_available: true,
		balance_infos: [{ currency: "USD", total_balance: "5.00" }],
	},
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

function renderStrip() {
	render(
		<ToastProvider>
			<QuotaStrip />
		</ToastProvider>,
	);
}

describe("QuotaStrip", () => {
	it("renders a badge per snapshot once loaded", async () => {
		server.use(
			http.get("/api/quota", () =>
				HttpResponse.json({ quota: [nano, deepseek] }),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-strip")).toBeInTheDocument(),
		);
		expect(screen.getByTestId("quota-badge-nanogpt:nano")).toBeInTheDocument();
		expect(screen.getByTestId("quota-badge-deepseek:ds")).toBeInTheDocument();
	});

	it("renders nothing on an authoritative empty list", async () => {
		// Gated on the GET actually landing (not merely on the pre-fetch
		// render, where `models.length === 0` is ALSO true because useQuota
		// seeds from cleared localStorage): otherwise this assertion would
		// pass against the loading state without ever observing the response,
		// and would not catch a wrong implementation like
		// `models.length === 0 && loading` that renders an empty bar once
		// loaded instead of staying hidden.
		let hits = 0;
		server.use(
			http.get("/api/quota", () => {
				hits++;
				return HttpResponse.json({ quota: [] });
			}),
		);
		renderStrip();
		await waitFor(() => expect(hits).toBe(1));
		expect(screen.queryByTestId("quota-strip")).toBeNull();
	});

	it("renders nothing when the first read fails with nothing cached", async () => {
		// Same reasoning as the empty-list test above: wait for the failing
		// response to actually land before asserting, rather than observing
		// the (also-null) pre-fetch loading state.
		let hits = 0;
		server.use(
			http.get("/api/quota", () => {
				hits++;
				return HttpResponse.json({ error: "x" }, { status: 502 });
			}),
		);
		renderStrip();
		await waitFor(() => expect(hits).toBe(1));
		expect(screen.queryByTestId("quota-strip")).toBeNull();
	});

	it("keeps cached badges and shows a stale marker when the read fails", async () => {
		localStorage.setItem(
			"fdQuotaSnapshots",
			JSON.stringify({
				snapshots: [nano],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		server.use(
			http.get("/api/quota", () =>
				HttpResponse.json({ error: "x" }, { status: 502 }),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-stale")).toBeInTheDocument(),
		);
		expect(screen.getByTestId("quota-badge-nanogpt:nano")).toBeInTheDocument();
	});

	it("opens a modal for a provider that has one", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		expect(screen.getByTestId("nano-weekly-fill")).toBeInTheDocument();
	});

	it("refreshes instead of opening a modal for DeepSeek", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [deepseek] })),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-badge-deepseek:ds")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-deepseek:ds"));
		await waitFor(() => expect(posted).toBe(1));
		expect(screen.queryByRole("dialog")).toBeNull();
	});

	it("toggles bar mode from inside the modal and shares it back to the badge", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		// Default is remaining: 1000 limit minus 400 used.
		expect(screen.getByTestId("quota-badge-nanogpt:nano")).toHaveTextContent(
			"600/1K",
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(screen.getByTestId("quota-badge-nanogpt:nano")).toHaveTextContent(
			"400/1K",
		);
		expect(localStorage.getItem("fdQuotaBarMode")).toBe("used");
	});

	it("collapses and remembers it", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-collapse"));
		expect(screen.queryByTestId("quota-badge-nanogpt:nano")).toBeNull();
		expect(localStorage.getItem("fdQuotaCollapsed")).toBe("true");
	});

	it("starts collapsed when that was the stored preference", async () => {
		localStorage.setItem("fdQuotaCollapsed", "true");
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-strip")).toBeInTheDocument(),
		);
		expect(screen.queryByTestId("quota-badge-nanogpt:nano")).toBeNull();
	});

	it("hides the refresh button while collapsed", async () => {
		localStorage.setItem("fdQuotaCollapsed", "true");
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-strip")).toBeInTheDocument(),
		);
		expect(screen.queryByTestId("quota-refresh")).toBeNull();
	});

	it("refreshes from the strip button", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-refresh")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() => expect(posted).toBe(1));
	});

	it("drops a snapshot whose type this build does not know", async () => {
		server.use(
			http.get("/api/quota", () =>
				HttpResponse.json({
					quota: [
						nano,
						{ ...nano, type: "anthropic", provider_name: "claude" },
					],
				}),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		expect(screen.queryByTestId("quota-badge-anthropic:claude")).toBeNull();
	});

	it("refreshes rather than opening a modal for a degraded badge", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () =>
				HttpResponse.json({ quota: [{ ...nano, http_status: 502 }] }),
			),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 0,
					failed: 1,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		await waitFor(() => expect(posted).toBe(1));
		expect(screen.queryByRole("dialog")).toBeNull();
	});

	// The tests below this line are additions beyond the brief's verbatim
	// suite, written to close a diff-coverage gap: the brief's 13 tests only
	// exercise the NanoGPT and DeepSeek branches of QuotaModalFor's six-way
	// switch, never open a modal from the strip and press refresh or close
	// inside it, never hit the ollama-cloud arm of the no-modal set, and never
	// reach the refresh-cooldown/refresh-failed toast branches.

	const zai: QuotaSnapshot = {
		provider_name: "zai",
		type: "zai-coding",
		kind: "usage",
		payload: {
			success: true,
			data: {
				level: "pro",
				limits: [{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 }],
			},
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	const kimi: QuotaSnapshot = {
		provider_name: "kimi",
		type: "kimi-code",
		kind: "usage",
		payload: {
			usage: {
				limit: "5000",
				remaining: "4000",
				resetTime: "2026-08-01T00:00:00Z",
			},
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	const minimax: QuotaSnapshot = {
		provider_name: "mmx",
		type: "minimax",
		kind: "usage",
		payload: {
			base_resp: { status_code: 0, status_msg: "ok" },
			model_remains: [
				{
					model_name: "general",
					current_interval_status: 1,
					current_interval_remaining_percent: 70,
					remains_time: 3_600_000,
					current_weekly_status: 1,
					current_weekly_remaining_percent: 30,
					weekly_remains_time: 86_400_000,
				},
			],
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	const openrouter: QuotaSnapshot = {
		provider_name: "or",
		type: "openrouter",
		kind: "balance",
		payload: {
			label: "k",
			limit: null,
			limit_reset: null,
			limit_remaining: null,
			usage: 40,
			usage_daily: 1,
			usage_weekly: 5,
			usage_monthly: 20,
			credits_total: 200,
			credits_used: 50,
			credits_remaining: 150,
			is_free_tier: false,
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	const neuralwatt: QuotaSnapshot = {
		provider_name: "nw",
		type: "neuralwatt",
		kind: "balance",
		payload: {
			balance: {
				credits_remaining_usd: 30,
				total_credits_usd: 100,
				credits_used_usd: 70,
				accounting_method: "metered",
			},
			usage: {
				lifetime: {
					cost_usd: 90,
					requests: 1200,
					tokens: 5_000_000,
					energy_kwh: 12.345,
				},
				current_month: {
					cost_usd: 20,
					requests: 1234,
					tokens: 1_000_000,
					energy_kwh: 3.21,
				},
			},
			limits: { overage_limit_usd: 25, rate_limit_tier: "standard" },
			subscription: {
				plan: "pro",
				status: "active",
				billing_interval: "month",
				current_period_start: "2026-07-01T00:00:00Z",
				current_period_end: "2026-08-01T00:00:00Z",
				auto_renew: true,
				kwh_included: 20,
				kwh_used: 5,
				kwh_remaining: 15,
				in_overage: false,
			},
			key: { name: "k", allowance: null },
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	const ollamaCloud: QuotaSnapshot = {
		provider_name: "olc",
		type: "ollama-cloud",
		kind: "account",
		payload: { plan: "pro", suspended_at: { valid: false } },
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	it.each([
		["zai-coding", zai, "zai-5h-fill"],
		["kimi-code", kimi, "kimi-weekly-fill"],
		["minimax", minimax, "minimax-general-5h-fill"],
		["openrouter", openrouter, "or-credits-fill"],
		["neuralwatt", neuralwatt, "nw-credits-fill"],
	] as const)("opens the %s modal for its badge and no other provider's", async (_label, snapshot, ownTestId) => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [snapshot] })),
		);
		renderStrip();
		const badgeTestId = `quota-badge-${snapshot.type}:${snapshot.provider_name}`;
		await waitFor(() =>
			expect(screen.getByTestId(badgeTestId)).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId(badgeTestId));
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		expect(screen.getByTestId(ownTestId)).toBeInTheDocument();
		// Guard against the dispatch opening a DIFFERENT provider's modal: only
		// one dialog exists, and it carries this provider's own content.
		expect(screen.getAllByRole("dialog")).toHaveLength(1);
	});

	it("refreshes instead of opening a modal for Ollama Cloud", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [ollamaCloud] })),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-ollama-cloud:olc"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-ollama-cloud:olc"));
		await waitFor(() => expect(posted).toBe(1));
		expect(screen.queryByRole("dialog")).toBeNull();
	});

	it("closes the open modal on Escape", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		await userEvent.keyboard("{Escape}");
		expect(screen.queryByRole("dialog")).toBeNull();
	});

	// M-3 regression: collapsing while a modal is open must close it (rather
	// than leaving a dialog floating over an empty strip), and expanding
	// again afterwards must not resurrect it.
	it("dismisses an open modal on collapse and does not reopen it on expand", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByRole("dialog")).toBeInTheDocument();

		await userEvent.click(screen.getByTestId("quota-collapse"));
		expect(screen.queryByRole("dialog")).toBeNull();
		expect(screen.queryByTestId("quota-badge-nanogpt:nano")).toBeNull();

		await userEvent.click(screen.getByTestId("quota-collapse"));
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		expect(screen.queryByRole("dialog")).toBeNull();
	});

	it("refreshes from the button inside an open modal", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		await userEvent.click(screen.getByTestId("quota-modal-refresh"));
		await waitFor(() => expect(posted).toBe(1));
	});

	it("shows an error toast when a refresh call fails", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post(
				"/api/quota/refresh",
				() => new HttpResponse(null, { status: 500 }),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-refresh")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() =>
			expect(document.querySelector(".fd-toast-error")).toBeInTheDocument(),
		);
	});

	it("shows an error toast, not a success one, when a 200 refresh reports a failed provider", async () => {
		// HTTP 200, so the request itself worked; the body says one provider did
		// not answer. Toasting "refreshed" here would tell the operator their data
		// is current when it is not.
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 1,
					skipped: 0,
				}),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-refresh")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() =>
			expect(document.querySelector(".fd-toast-error")).toBeInTheDocument(),
		);
		expect(document.querySelector(".fd-toast-success")).toBeNull();
	});

	it("shows a success toast when a 200 refresh reports no failures", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({
					results: [],
					refreshed: 2,
					failed: 0,
					skipped: 0,
				}),
			),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-refresh")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() =>
			expect(document.querySelector(".fd-toast-success")).toBeInTheDocument(),
		);
		expect(document.querySelector(".fd-toast-error")).toBeNull();
	});

	it("shows a cooldown toast instead of a second refresh call made right away", async () => {
		let posted = 0;
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [nano] })),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(screen.getByTestId("quota-refresh")).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() => expect(posted).toBe(1));
		await userEvent.click(screen.getByTestId("quota-refresh"));
		await waitFor(() =>
			expect(document.querySelector(".fd-toast-info")).toBeInTheDocument(),
		);
		// The cooldown outcome returns before calling the endpoint again.
		expect(posted).toBe(1);
	});
});

// M-4 regression: a provider dropping out of a LATER, genuinely loaded poll
// must close its own open modal, and the provider reappearing on a
// subsequent poll must not silently reopen it. Uses fake timers to drive the
// 60 second poll interval directly (matching the pattern in
// hooks/__tests__/useQuota.test.ts's "useQuota polling" describe), rather
// than the strip's own refresh button, which has a 10 second cooldown that
// would make two rapid manual refreshes indistinguishable from one.
describe("QuotaStrip polling", () => {
	beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
	afterEach(() => vi.useRealTimers());

	const zaiForPolling: QuotaSnapshot = {
		provider_name: "zai",
		type: "zai-coding",
		kind: "usage",
		payload: {
			success: true,
			data: {
				level: "pro",
				limits: [{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 }],
			},
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};

	it("closes a stale open modal when its provider drops from a later poll, and does not reopen it when the provider returns", async () => {
		let call = 0;
		server.use(
			http.get("/api/quota", () => {
				call++;
				// Poll 1 (mount): both providers present. Poll 2: nano drops out
				// of the fleet primary's export, but zai stays, so the strip
				// itself stays mounted. Poll 3: nano comes back.
				if (call === 2) {
					return HttpResponse.json({ quota: [zaiForPolling] });
				}
				return HttpResponse.json({ quota: [nano, zaiForPolling] });
			}),
		);
		renderStrip();
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:nano"));
		expect(screen.getByTestId("nano-weekly-fill")).toBeInTheDocument();

		// Poll 2 lands: nano is gone, so its modal must close on its own.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(call).toBe(2));
		await waitFor(() =>
			expect(screen.queryByTestId("quota-badge-nanogpt:nano")).toBeNull(),
		);
		expect(screen.queryByRole("dialog")).toBeNull();
		// The strip stays up: zai's badge is still there.
		expect(screen.getByTestId("quota-strip")).toBeInTheDocument();
		expect(
			screen.getByTestId("quota-badge-zai-coding:zai"),
		).toBeInTheDocument();

		// Poll 3 lands: nano is back. The dismissed modal must NOT reopen on
		// its own; a badge reappearing is not the operator asking to see it.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(call).toBe(3));
		await waitFor(() =>
			expect(
				screen.getByTestId("quota-badge-nanogpt:nano"),
			).toBeInTheDocument(),
		);
		expect(screen.queryByRole("dialog")).toBeNull();
	});
});
