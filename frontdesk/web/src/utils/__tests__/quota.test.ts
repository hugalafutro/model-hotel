import { describe, expect, it } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import {
	barTone,
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
	isQuotaProviderType,
	payloadOf,
	toBadgeModels,
} from "../quota";

function snap(over: Partial<QuotaSnapshot> = {}): QuotaSnapshot {
	return {
		provider_name: "p",
		type: "nanogpt",
		kind: "usage",
		payload: {
			providerStatus: "active",
			limits: {
				weeklyInputTokens: 1000,
				dailyInputTokens: null,
				dailyImages: null,
			},
			weeklyInputTokens: {
				used: 100,
				remaining: 900,
				percentUsed: 0.1,
				resetAt: 0,
			},
		},
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
		...over,
	};
}

describe("isQuotaProviderType", () => {
	it("accepts the eight known types", () => {
		for (const t of [
			"nanogpt",
			"zai-coding",
			"kimi-code",
			"minimax",
			"deepseek",
			"openrouter",
			"ollama-cloud",
			"neuralwatt",
		]) {
			expect(isQuotaProviderType(t)).toBe(true);
		}
	});

	it("rejects anything else", () => {
		expect(isQuotaProviderType("anthropic")).toBe(false);
		expect(isQuotaProviderType("")).toBe(false);
	});
});

describe("payloadOf", () => {
	it("returns the payload for a healthy snapshot", () => {
		expect(payloadOf<{ a: number }>(snap({ payload: { a: 1 } }))).toEqual({
			a: 1,
		});
	});

	it("returns null for a non-200 status", () => {
		expect(payloadOf(snap({ http_status: 424 }))).toBeNull();
	});

	it("returns null for a null or non-object payload", () => {
		expect(payloadOf(snap({ payload: null }))).toBeNull();
		expect(payloadOf(snap({ payload: "nope" }))).toBeNull();
	});

	it("returns null for an array payload", () => {
		// `typeof [] === "object"`, so an array slips past a bare typeof check and
		// becomes a "usable" payload with none of the fields any renderer reads.
		expect(payloadOf(snap({ payload: [] }))).toBeNull();
		expect(payloadOf(snap({ payload: [{ plan: "pro" }] }))).toBeNull();
	});

	it("marks an array payload degraded rather than rendering a bare dash", () => {
		// End-to-end consequence of the guard above: ollama-cloud's visibility rule
		// (suspended_at?.valid !== true) is satisfied by an array, so without the
		// guard this badge renders NON-degraded with a "-" value.
		const out = toBadgeModels([
			snap({ type: "ollama-cloud", payload: [] as unknown as object }),
		]);
		expect(out).toHaveLength(1);
		expect(out[0].degraded).toBe(true);
	});
});

describe("barTone", () => {
	it("grades remaining mode from the remaining share", () => {
		expect(barTone(90, "remaining")).toBe("danger"); // 10% left
		expect(barTone(50, "remaining")).toBe("warn"); // 50% left
		expect(barTone(10, "remaining")).toBe("ok"); // 90% left
	});

	it("grades used mode from the used share", () => {
		expect(barTone(20, "used")).toBe("warn");
		expect(barTone(60, "used")).toBe("high");
		expect(barTone(95, "used")).toBe("danger");
	});

	// Each threshold is exclusive (`<`). The tests below sit exactly on the
	// boundary and one step below it, so flipping any `<` to `<=` flips an
	// assertion; the round numbers used above all sit mid-band and survive that.
	it("puts exactly 20% remaining in warn, not danger", () => {
		expect(barTone(80, "remaining")).toBe("warn"); // 20% left
		expect(barTone(81, "remaining")).toBe("danger"); // 19% left
	});

	it("puts exactly 60% remaining in ok, not warn", () => {
		expect(barTone(40, "remaining")).toBe("ok"); // 60% left
		expect(barTone(41, "remaining")).toBe("warn"); // 59% left
	});

	it("puts exactly 50% used in high, not warn", () => {
		expect(barTone(50, "used")).toBe("high");
		expect(barTone(49, "used")).toBe("warn");
	});

	it("puts exactly 80% used in danger, not high", () => {
		expect(barTone(80, "used")).toBe("danger");
		expect(barTone(79, "used")).toBe("high");
	});
});

describe("Z.ai limit extraction", () => {
	const usage = {
		success: true,
		data: {
			limits: [
				{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 },
				{ type: "TOKENS_LIMIT", unit: 6, percentage: 12 },
				{ type: "TIME_LIMIT", unit: 5, percentage: 5 },
			],
		},
	};

	it("finds the 5 hour window by unit 3", () => {
		expect(getZaiCodingFiveHourLimit(usage)?.percentage).toBe(40);
	});

	it("finds the weekly window by unit 6", () => {
		expect(getZaiCodingWeeklyLimit(usage)?.percentage).toBe(12);
	});

	it("returns undefined for missing data", () => {
		expect(getZaiCodingFiveHourLimit(null)).toBeUndefined();
		expect(getZaiCodingWeeklyLimit({ success: true })).toBeUndefined();
	});
});

describe("Kimi Code limit extraction", () => {
	const usage = {
		limits: [
			{
				window: { timeUnit: "TIME_UNIT_MINUTE", duration: 300 },
				detail: {
					limit: "1000",
					remaining: "250",
					resetTime: "2026-07-26T15:00:00Z",
				},
			},
		],
		usage: {
			limit: "5000",
			remaining: "5000",
			resetTime: "2026-08-01T00:00:00Z",
		},
	};

	it("computes percent used from string limit and remaining", () => {
		const w = getKimiCodeFiveHourLimit(usage);
		expect(w?.limit).toBe(1000);
		expect(w?.remaining).toBe(250);
		expect(w?.percentage).toBe(75);
	});

	it("reads the weekly window from the top-level usage block", () => {
		expect(getKimiCodeWeeklyLimit(usage)?.percentage).toBe(0);
	});

	it("returns undefined when the numbers are not finite", () => {
		expect(
			getKimiCodeFiveHourLimit({
				limits: [
					{
						window: { timeUnit: "TIME_UNIT_MINUTE", duration: 300 },
						detail: { limit: "abc", remaining: "1" },
					},
				],
			}),
		).toBeUndefined();
	});

	it("treats a zero limit as zero percent used rather than dividing by zero", () => {
		expect(
			getKimiCodeFiveHourLimit({
				limits: [
					{
						window: { timeUnit: "TIME_UNIT_MINUTE", duration: 300 },
						detail: { limit: "0", remaining: "0" },
					},
				],
			})?.percentage,
		).toBe(0);
	});
});

describe("MiniMax limit extraction", () => {
	const usage = {
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
	};

	it("finds the active general entry", () => {
		expect(getMiniMaxGeneralEntry(usage)?.model_name).toBe("general");
	});

	it("converts remaining percent into percent used", () => {
		expect(getMiniMaxFiveHourLimit(usage)?.percentage).toBe(30);
		expect(getMiniMaxWeeklyLimit(usage)?.percentage).toBe(70);
	});

	it("ignores the payload when base_resp reports an error", () => {
		expect(
			getMiniMaxGeneralEntry({
				...usage,
				base_resp: { status_code: 2062, status_msg: "no plan" },
			}),
		).toBeUndefined();
	});

	it("ignores an inactive general entry", () => {
		expect(
			getMiniMaxGeneralEntry({
				...usage,
				model_remains: [
					{ ...usage.model_remains[0], current_interval_status: 3 },
				],
			}),
		).toBeUndefined();
	});

	it("tolerates a null model_remains", () => {
		expect(
			getMiniMaxGeneralEntry({ ...usage, model_remains: null }),
		).toBeUndefined();
	});
});

describe("toBadgeModels", () => {
	it("drops snapshots whose type this build does not know", () => {
		const out = toBadgeModels([
			snap(),
			snap({ type: "anthropic", provider_name: "claude" }),
		]);
		expect(out).toHaveLength(1);
		expect(out[0].type).toBe("nanogpt");
	});

	it("drops an unknown type even when its fetch also failed", () => {
		// The discriminating case for rule ordering: the type guard must run
		// BEFORE the degraded check, or this would render a badge whose type is
		// not a key of QUOTA_PREFIXES/QUOTA_BRAND_COLORS.
		expect(
			toBadgeModels([snap({ type: "anthropic", http_status: 502 })]),
		).toEqual([]);
	});

	it("marks a non-200 snapshot degraded instead of hiding it", () => {
		const out = toBadgeModels([snap({ http_status: 502 })]);
		expect(out).toHaveLength(1);
		expect(out[0].degraded).toBe(true);
	});

	it("marks an unparseable payload degraded", () => {
		const out = toBadgeModels([snap({ payload: null })]);
		expect(out).toHaveLength(1);
		expect(out[0].degraded).toBe(true);
	});

	it("hides a cancelled NanoGPT subscription", () => {
		const out = toBadgeModels([
			snap({
				payload: {
					providerStatus: "canceled",
					limits: {},
					weeklyInputTokens: null,
				},
			}),
		]);
		expect(out).toEqual([]);
	});

	it("hides a suspended Ollama Cloud account", () => {
		const out = toBadgeModels([
			snap({
				type: "ollama-cloud",
				payload: {
					plan: "pro",
					suspended_at: { time: "x", valid: true },
					subscription_period_end: { time: "", valid: false },
				},
			}),
		]);
		expect(out).toEqual([]);
	});

	it("hides a NeuralWatt free tier account", () => {
		const out = toBadgeModels([
			snap({
				type: "neuralwatt",
				payload: {
					balance: { credits_remaining_usd: 1 },
					subscription: { plan: "Free" },
				},
			}),
		]);
		expect(out).toEqual([]);
	});

	it("hides an unavailable DeepSeek balance", () => {
		const out = toBadgeModels([
			snap({
				type: "deepseek",
				payload: { is_available: false, balance_infos: [] },
			}),
		]);
		expect(out).toEqual([]);
	});

	it("keys badges by provider name and flags same-type collisions", () => {
		const out = toBadgeModels([
			snap({ provider_name: "nano-b" }),
			snap({ provider_name: "nano-a" }),
		]);
		expect(out.map((m) => m.key)).toEqual(["nanogpt:nano-a", "nanogpt:nano-b"]);
		expect(out.every((m) => m.showProviderName)).toBe(true);
	});

	it("does not show the provider name when a type is unambiguous", () => {
		const out = toBadgeModels([
			snap({ provider_name: "nano" }),
			snap({
				type: "deepseek",
				provider_name: "ds",
				payload: { is_available: true, balance_infos: [] },
			}),
		]);
		// Exact, not `.every(...) === false`: that form passes when a SINGLE badge
		// is false, so it would not catch one of the two being relabelled.
		expect(out.map((m) => m.showProviderName)).toEqual([false, false]);
	});

	it("does not relabel one type because another type collided", () => {
		const out = toBadgeModels([
			snap({ provider_name: "nano-a" }),
			snap({ provider_name: "nano-b" }),
			snap({
				type: "deepseek",
				provider_name: "ds",
				payload: { is_available: true, balance_infos: [] },
			}),
		]);
		const ds = out.find((m) => m.type === "deepseek");
		expect(ds?.showProviderName).toBe(false);
	});

	it("orders deterministically by type then provider name", () => {
		const out = toBadgeModels([
			snap({
				type: "openrouter",
				provider_name: "or",
				payload: { credits_remaining: 5 },
			}),
			snap({ provider_name: "nano" }),
		]);
		expect(out.map((m) => m.type)).toEqual(["nanogpt", "openrouter"]);
	});
});
