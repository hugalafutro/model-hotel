import {
	isNeuralWattQuotaVisible,
	isQuotaPayloadVisible,
	type QuotaProviderType,
} from "@web-shared/quota";
import { describe, expect, it } from "vitest";

// isQuotaPayloadVisible is the one entry point Front Desk uses: it receives a
// provider type as a value (the fleet primary stamps it on every snapshot)
// rather than sniffing a base URL. Every arm has to reach its family's rule,
// because a wrong arm shows one provider's badge under another's name, or
// hides a badge that has data behind it.
//
// Each case pairs a payload that must show with one that must not, so an arm
// wired to a rule that always answers the same way fails here.
describe("isQuotaPayloadVisible", () => {
	const cases: Array<{
		type: QuotaProviderType;
		visible: object;
		hidden: object;
		why: string;
	}> = [
		{
			type: "nanogpt",
			why: "a cancelled plan has nothing left to meter",
			visible: {
				providerStatus: "active",
				limits: { weeklyInputTokens: 1_000_000 },
				weeklyInputTokens: { used: 12 },
			},
			hidden: {
				providerStatus: "cancelled",
				limits: { weeklyInputTokens: 1_000_000 },
				weeklyInputTokens: { used: 12 },
			},
		},
		{
			type: "zai-coding",
			why: "a failed call carries no windows",
			visible: {
				success: true,
				data: { limits: [{ type: "TOKENS_LIMIT", unit: 3 }] },
			},
			hidden: {
				success: false,
				data: { limits: [{ type: "TOKENS_LIMIT", unit: 3 }] },
			},
		},
		{
			type: "kimi-code",
			why: "no window entry means no badge",
			visible: {
				limits: [
					{
						window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
						detail: { limit: "100" },
					},
				],
			},
			hidden: { limits: [] },
		},
		{
			type: "minimax",
			why: "a non-zero base_resp status means no active plan",
			visible: {
				base_resp: { status_code: 0, status_msg: "ok" },
				model_remains: [
					{
						model_name: "general",
						current_interval_status: 1,
						remains_time: 0,
						weekly_remains_time: 0,
						current_weekly_status: 1,
						current_interval_remaining_percent: 50,
						current_weekly_remaining_percent: 60,
					},
				],
			},
			hidden: {
				base_resp: { status_code: 2062, status_msg: "no active token plan" },
				model_remains: [
					{
						model_name: "general",
						current_interval_status: 1,
						remains_time: 0,
						weekly_remains_time: 0,
						current_weekly_status: 1,
						current_interval_remaining_percent: 50,
						current_weekly_remaining_percent: 60,
					},
				],
			},
		},
		{
			type: "deepseek",
			why: "an unavailable balance is not a number to show",
			visible: { is_available: true },
			hidden: { is_available: false },
		},
		{
			type: "openrouter",
			why: "a null credit figure has nothing to render",
			visible: { credits_remaining: 4.2 },
			hidden: { credits_remaining: null },
		},
		{
			type: "ollama-cloud",
			why: "a suspended account has no live quota",
			visible: { suspended_at: { valid: false } },
			hidden: { suspended_at: { valid: true } },
		},
		{
			type: "neuralwatt",
			why: "the low tiers are excluded from badges",
			visible: {
				balance: { credits_remaining_usd: 5 },
				subscription: { plan: "pro" },
			},
			hidden: {
				balance: { credits_remaining_usd: 5 },
				subscription: { plan: "starter" },
			},
		},
	];

	for (const c of cases) {
		it(`${c.type}: shows a live payload and hides one where ${c.why}`, () => {
			expect(isQuotaPayloadVisible(c.type, c.visible)).toBe(true);
			expect(isQuotaPayloadVisible(c.type, c.hidden)).toBe(false);
		});
	}

	it("hides a payload of a type it does not know", () => {
		expect(
			isQuotaPayloadVisible("not-a-provider" as QuotaProviderType, {
				credits_remaining: 9,
			}),
		).toBeFalsy();
	});
});

// NeuralWatt bills against a prepaid balance, so a badge needs both a readable
// balance and a plan worth showing one for. The excluded tiers are matched
// case-insensitively: the API has returned both "Free" and "free".
describe("isNeuralWattQuotaVisible", () => {
	it("needs a balance figure", () => {
		expect(
			isNeuralWattQuotaVisible({
				balance: { credits_remaining_usd: 0 },
				subscription: { plan: "pro" },
			}),
		).toBe(true);
		expect(
			isNeuralWattQuotaVisible({
				balance: { credits_remaining_usd: null },
				subscription: { plan: "pro" },
			}),
		).toBe(false);
		expect(isNeuralWattQuotaVisible({ subscription: { plan: "pro" } })).toBe(
			false,
		);
	});

	it("excludes the free and starter tiers whatever their casing", () => {
		for (const plan of ["free", "Free", "STARTER", "starter"]) {
			expect(
				isNeuralWattQuotaVisible({
					balance: { credits_remaining_usd: 5 },
					subscription: { plan },
				}),
			).toBe(false);
		}
		for (const plan of ["pro", "Team", "enterprise"]) {
			expect(
				isNeuralWattQuotaVisible({
					balance: { credits_remaining_usd: 5 },
					subscription: { plan },
				}),
			).toBe(true);
		}
	});

	it("treats a missing plan as not excluded", () => {
		expect(
			isNeuralWattQuotaVisible({ balance: { credits_remaining_usd: 5 } }),
		).toBe(true);
	});
});
