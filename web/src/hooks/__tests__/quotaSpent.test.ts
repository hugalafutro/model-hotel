import {
	isDeepSeekQuotaSpent,
	isOpenRouterQuotaSpent,
	isQuotaPayloadSpent,
	type QuotaProviderType,
} from "@web-shared/quota";
import { describe, expect, it } from "vitest";

// Each arm pairs a payload that must read as spent with one that must not, so
// an arm wired to a rule that always answers the same way fails here. The
// healthy twin is the spent one with a single field changed, so the test also
// says which field decides.
describe("isQuotaPayloadSpent", () => {
	const kimiWindow = (remaining: string) => ({
		limit: "1000",
		remaining,
		resetTime: "2026-09-05T00:00:00Z",
	});
	const minimaxEntry = (interval: number, weekly: number) => ({
		base_resp: { status_code: 0, status_msg: "ok" },
		model_remains: [
			{
				model_name: "general",
				current_interval_status: 1,
				current_interval_remaining_percent: interval,
				remains_time: 1000,
				current_weekly_status: 1,
				current_weekly_remaining_percent: weekly,
				weekly_remains_time: 1000,
			},
		],
	});
	const cases: Array<{
		type: QuotaProviderType;
		spent: object;
		healthy: object;
		why: string;
	}> = [
		{
			type: "nanogpt",
			why: "the weekly allowance is used up",
			spent: {
				limits: { weeklyInputTokens: 1000 },
				weeklyInputTokens: { used: 1000 },
			},
			healthy: {
				limits: { weeklyInputTokens: 1000 },
				weeklyInputTokens: { used: 999 },
			},
		},
		{
			type: "zai-coding",
			why: "a window at 100 percent is spent even with a residue remaining",
			spent: {
				success: true,
				data: {
					limits: [
						{ type: "TOKENS_LIMIT", unit: 6, percentage: 100, remaining: 3 },
					],
				},
			},
			healthy: {
				success: true,
				data: {
					limits: [
						{ type: "TOKENS_LIMIT", unit: 6, percentage: 99, remaining: 0 },
					],
				},
			},
		},
		{
			type: "zai-coding",
			why: "without a sane percentage, remaining at zero decides",
			spent: {
				success: true,
				data: {
					limits: [
						{ type: "TOKENS_LIMIT", unit: 3, percentage: 250, remaining: 0 },
					],
				},
			},
			healthy: {
				success: true,
				data: {
					limits: [
						{ type: "TOKENS_LIMIT", unit: 3, percentage: 250, remaining: 1 },
					],
				},
			},
		},
		{
			type: "kimi-code",
			why: "either window spent blocks the account",
			spent: {
				usage: kimiWindow("5"),
				limits: [
					{
						window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
						detail: kimiWindow("0"),
					},
				],
			},
			healthy: {
				usage: kimiWindow("5"),
				limits: [
					{
						window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
						detail: kimiWindow("1"),
					},
				],
			},
		},
		{
			type: "minimax",
			why: "a weekly window with nothing remaining is spent",
			spent: minimaxEntry(40, 0),
			healthy: minimaxEntry(40, 1),
		},
		{
			type: "deepseek",
			why: "an available account whose balance reads zero",
			spent: {
				is_available: true,
				balance_infos: [{ currency: "USD", total_balance: "0.00" }],
			},
			healthy: {
				is_available: true,
				balance_infos: [{ currency: "USD", total_balance: "0.01" }],
			},
		},
		{
			type: "openrouter",
			why: "credits at zero",
			spent: { credits_remaining: 0, limit_remaining: null },
			healthy: { credits_remaining: 0.5, limit_remaining: null },
		},
		{
			type: "neuralwatt",
			why: "in overage with the credits below the sub-cent floor",
			spent: {
				subscription: { plan: "basic", in_overage: true },
				balance: { credits_remaining_usd: 0.0035 },
			},
			healthy: {
				subscription: { plan: "basic", in_overage: true },
				balance: { credits_remaining_usd: 0.01 },
			},
		},
	];

	for (const c of cases) {
		it(`${c.type}: ${c.why}`, () => {
			expect(isQuotaPayloadSpent(c.type, c.spent)).toBe(true);
			expect(isQuotaPayloadSpent(c.type, c.healthy)).toBe(false);
		});
	}

	it("ollama-cloud never reads as spent: the account payload names no usage", () => {
		expect(isQuotaPayloadSpent("ollama-cloud", { plan: "pro" })).toBe(false);
	});

	it("neuralwatt: an absent balance is unknown, not spent", () => {
		expect(
			isQuotaPayloadSpent("neuralwatt", {
				subscription: { in_overage: true },
				balance: {},
			}),
		).toBe(false);
	});

	it("deepseek: unavailable is spent, and an empty balance list is not", () => {
		expect(isDeepSeekQuotaSpent({ is_available: false })).toBe(true);
		expect(
			isDeepSeekQuotaSpent({ is_available: true, balance_infos: [] }),
		).toBe(false);
	});

	it("openrouter: a spent per-key cap counts even with credits left", () => {
		expect(
			isOpenRouterQuotaSpent({ credits_remaining: 12, limit_remaining: 0 }),
		).toBe(true);
	});
});
