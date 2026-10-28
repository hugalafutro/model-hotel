import { render, screen } from "@testing-library/react";
import { formatCount, formatDollars } from "@web-shared/format";
import type {
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OpenRouterBalance,
} from "@web-shared/quota";
import {
	NanoGPTQuotaModal,
	NeuralWattQuotaModal,
	OpenRouterQuotaModal,
} from "@web-shared/quota/modals";
import i18n from "../../i18n";
import { quotaBodyChrome as chrome } from "../../test/quotaKit";

describe("NanoGPTQuotaModal", () => {
	const payload: NanoGPTUsage = {
		active: true,
		provider: "nanogpt",
		providerStatus: "active",
		cancelAtPeriodEnd: false,
		limits: { weeklyInputTokens: 1000, dailyInputTokens: 200, dailyImages: 50 },
		allowOverage: true,
		period: { currentPeriodEnd: "2026-08-01T00:00:00Z" },
		dailyImages: {
			used: 10,
			remaining: 40,
			percentUsed: 0.2,
			resetAt: 1_800_000_000_000,
		},
		dailyInputTokens: {
			used: 50,
			remaining: 150,
			percentUsed: 0.25,
			resetAt: 1_800_000_000_000,
		},
		weeklyInputTokens: {
			used: 400,
			remaining: 600,
			percentUsed: 0.4,
			resetAt: 1_800_000_000_000,
		},
	};

	it("fills the weekly bar from used over limit", () => {
		render(<NanoGPTQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("nano-weekly-fill")).toHaveStyle({
			width: "40%",
		});
	});

	it("groups the digits of large image counts", () => {
		// The counts used to be interpolated raw, so a four-figure allowance
		// rendered as "1200 / 15000". Small numbers are unaffected by the fix,
		// which is why this uses values above the grouping threshold: with the
		// raw interpolation restored, this assertion fails and the small-number
		// cases elsewhere in the file still pass.
		const big: NanoGPTUsage = {
			...payload,
			limits: { ...payload.limits, dailyImages: 15_000 },
			dailyImages: {
				used: 1_200,
				remaining: 13_800,
				percentUsed: 0.08,
				resetAt: 1_800_000_000_000,
			},
		};
		render(<NanoGPTQuotaModal {...chrome} payload={big} barMode="used" />);
		expect(
			screen.getByTestId("nano-images-bar").closest(".ui-quota-bar-block"),
		).toHaveTextContent("1,200 / 15,000");
	});

	it("does not abbreviate an image count the way token counts are abbreviated", () => {
		// formatTokens would render 1200 as "1.2K", which hides the difference
		// between 1,200 and 1,249 on a figure the operator reads exactly.
		const big: NanoGPTUsage = {
			...payload,
			limits: { ...payload.limits, dailyImages: 15_000 },
			dailyImages: {
				used: 1_249,
				remaining: 13_751,
				percentUsed: 0.083,
				resetAt: 1_800_000_000_000,
			},
		};
		render(<NanoGPTQuotaModal {...chrome} payload={big} barMode="used" />);
		const text = screen
			.getByTestId("nano-images-bar")
			.closest(".ui-quota-bar-block")?.textContent;
		expect(text).toContain("1,249");
		expect(text).not.toContain("1.2K");
	});

	// NanoGPT documents percentUsed as a fraction that may exceed 1
	// (docs.nano-gpt.com, subscription usage endpoint), so 0.2 is 20% used and
	// 1.3 is an allowance overrun that draws as a full bar.
	it("reads percentUsed as a fraction, capping an overrun at a full bar", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{
					...payload,
					dailyImages: {
						used: 65,
						remaining: 0,
						percentUsed: 1.3,
						resetAt: 1_800_000_000_000,
					},
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nano-images-fill")).toHaveStyle({
			width: "100%",
		});
	});

	it("labels an unconfigured daily limit as unlimited", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{
					...payload,
					limits: {
						...payload.limits,
						dailyImages: null,
						dailyInputTokens: null,
					},
				}}
				barMode="used"
			/>,
		);
		const unlimited = i18n.t("components.providerModals.unlimited");
		expect(
			screen.getByTestId("nano-images-bar").closest(".ui-quota-bar-block"),
		).toHaveTextContent(`10 / ${unlimited}`);
		expect(
			screen
				.getByTestId("nano-daily-tokens-bar")
				.closest(".ui-quota-bar-block"),
		).toHaveTextContent(unlimited);
	});

	it("scales the fractional percentUsed to a percentage", () => {
		render(<NanoGPTQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("nano-images-fill")).toHaveStyle({
			width: "20%",
		});
		expect(screen.getByTestId("nano-daily-tokens-fill")).toHaveStyle({
			width: "25%",
		});
	});

	it("omits the optional bars when the provider reports none", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{ ...payload, dailyImages: null, dailyInputTokens: null }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("nano-images-fill")).toBeNull();
		expect(screen.queryByTestId("nano-daily-tokens-fill")).toBeNull();
	});

	it("treats a zero weekly limit as zero percent used rather than dividing by zero", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{
					...payload,
					limits: { ...payload.limits, weeklyInputTokens: 0 },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nano-weekly-fill")).toHaveStyle({ width: "0%" });
	});

	it("warns when the subscription is set to cancel", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{ ...payload, cancelAtPeriodEnd: true }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nano-cancel-notice")).toBeInTheDocument();
	});

	it("marks an inactive subscription in the status dot", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{ ...payload, active: false }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nano-status-inactive")).toBeInTheDocument();
	});

	it("marks an active subscription in the status dot", () => {
		render(
			<NanoGPTQuotaModal
				{...chrome}
				payload={{ ...payload, active: true }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nano-status-active")).toBeInTheDocument();
		expect(screen.queryByTestId("nano-status-inactive")).toBeNull();
	});

	it("falls back to literal zeros and swaps labelled values for a sparse payload", () => {
		const sparse: NanoGPTUsage = {
			...payload,
			allowOverage: false,
			weeklyInputTokens: null,
			limits: {
				weeklyInputTokens: null,
				dailyInputTokens: null,
				dailyImages: null,
			},
		};
		const { unmount } = render(
			<NanoGPTQuotaModal {...chrome} payload={sparse} barMode="used" />,
		);
		// Both `?? 0` fallbacks (weeklyLimit, weeklyUsed) resolve to a literal
		// zero, not a blank "-" placeholder. The testid sits on the bar track,
		// not the header, so walk up to the block to reach the rightText.
		expect(
			screen.getByTestId("nano-weekly-bar").closest(".ui-quota-bar-block"),
		).toHaveTextContent("0 / 0");
		expect(screen.getByTestId("nano-weekly-fill")).toHaveStyle({ width: "0%" });
		// The optional bars still render even though their configured limit is null.
		expect(screen.getByTestId("nano-images-fill")).toBeInTheDocument();
		expect(screen.getByTestId("nano-daily-tokens-fill")).toBeInTheDocument();

		const sparseImagesText = screen
			.getByTestId("nano-images-bar")
			.closest(".ui-quota-bar-block")?.textContent;
		const sparseTokensText = screen
			.getByTestId("nano-daily-tokens-bar")
			.closest(".ui-quota-bar-block")?.textContent;
		const sparseOverage = screen.getByTestId("nano-allow-overage").textContent;
		unmount();

		render(<NanoGPTQuotaModal {...chrome} payload={payload} barMode="used" />);
		const baseImagesText = screen
			.getByTestId("nano-images-bar")
			.closest(".ui-quota-bar-block")?.textContent;
		const baseTokensText = screen
			.getByTestId("nano-daily-tokens-bar")
			.closest(".ui-quota-bar-block")?.textContent;
		const baseOverage = screen.getByTestId("nano-allow-overage").textContent;

		// Locale-independent: compares two live renders rather than asserting on
		// translated text, but still proves each fallback/ternary is wired to its
		// source field instead of being hardcoded to one arm.
		expect(sparseImagesText).not.toBe(baseImagesText);
		expect(sparseTokensText).not.toBe(baseTokensText);
		expect(sparseOverage).not.toBe(baseOverage);
	});
});

describe("OpenRouterQuotaModal", () => {
	const payload: OpenRouterBalance = {
		label: "k",
		limit: 100,
		limit_reset: "2026-08-01T00:00:00Z",
		limit_remaining: 25,
		usage: 40,
		usage_daily: 1,
		usage_weekly: 5,
		usage_monthly: 20,
		credits_total: 200,
		credits_used: 50,
		credits_remaining: 150,
		is_free_tier: false,
	};

	it("fills the credit bar from credits used over total", () => {
		render(
			<OpenRouterQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("or-credits-fill")).toHaveStyle({ width: "25%" });
	});

	it("fills the key limit bar from the remaining allowance", () => {
		render(
			<OpenRouterQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("or-limit-fill")).toHaveStyle({ width: "75%" });
	});

	it("omits the credit bar when the account has no credit total", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, credits_total: 0 }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("or-credits-fill")).toBeNull();
	});

	it("omits the key limit block when no limit is set", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, limit: null }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("or-limit-fill")).toBeNull();
	});

	it("says the usage tiles are this key's spend, not the account's", () => {
		render(
			<OpenRouterQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("or-key-usage-hint")).toHaveTextContent(
			i18n.t("components.providerModals.spendingByThisKey"),
		);
	});

	it("renders the four spending tiles", () => {
		render(
			<OpenRouterQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("or-usage-daily")).toHaveTextContent("$1.00");
		expect(screen.getByTestId("or-usage-weekly")).toHaveTextContent("$5.00");
		expect(screen.getByTestId("or-usage-monthly")).toHaveTextContent("$20.00");
		expect(screen.getByTestId("or-usage-all")).toHaveTextContent("$40.00");
	});

	// Regression: the visibility gate passes any snapshot that carries
	// credits_remaining, and formatDollars throws on undefined, so a body that
	// formatted every usage field took the modal out behind its error boundary.
	it("renders a snapshot holding only credits_remaining as the balance alone", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ credits_remaining: 5 }}
				barMode="used"
			/>,
		);
		expect(screen.getByRole("dialog")).toHaveTextContent(formatDollars(5));
		expect(screen.queryByTestId("or-credits-bar")).toBeNull();
		expect(screen.queryByTestId("or-limit-bar")).toBeNull();
		expect(screen.queryByTestId("or-key-usage-hint")).toBeNull();
		expect(screen.queryByTestId("or-usage-daily")).toBeNull();
		expect(screen.queryByTestId("or-usage-weekly")).toBeNull();
		expect(screen.queryByTestId("or-usage-monthly")).toBeNull();
		expect(screen.queryByTestId("or-usage-all")).toBeNull();
	});

	it("renders only the spending tiles the snapshot carries", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ credits_remaining: 5, usage_daily: 1, credits_total: 10 }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("or-usage-daily")).toHaveTextContent(
			formatDollars(1),
		);
		expect(screen.queryByTestId("or-usage-weekly")).toBeNull();
		expect(screen.queryByTestId("or-usage-monthly")).toBeNull();
		expect(screen.queryByTestId("or-usage-all")).toBeNull();
		// A credit total without credits_used has no bar to fill.
		expect(screen.queryByTestId("or-credits-bar")).toBeNull();
	});

	it("marks a free-tier account", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, is_free_tier: true }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("or-tier-free")).toBeInTheDocument();
		expect(screen.queryByTestId("or-tier-paid")).toBeNull();
	});

	it("marks a paid-tier account", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, is_free_tier: false }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("or-tier-paid")).toBeInTheDocument();
		expect(screen.queryByTestId("or-tier-free")).toBeNull();
	});

	// A missing remainder is unknown, not spent: no limit bar rather than a
	// fabricated 100%-used one.
	it("omits the key limit bar when the remainder is null", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, limit_remaining: null }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("or-limit-bar")).toBeNull();
	});

	it("omits the key limit bar when the remainder is absent", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ credits_remaining: 5, limit: 100 }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("or-limit-bar")).toBeNull();
		expect(screen.getByRole("dialog")).toHaveTextContent(formatDollars(5));
	});

	it("still renders a zero-value key limit bar at 0 percent used", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, limit: 0 }}
				barMode="used"
			/>,
		);
		// Pinning current behaviour (matches the brief and Model Hotel): a
		// zero-value limit is still a finite figure, so the block renders
		// rather than being omitted like `limit: null`, with a 0% fill since
		// limitPctUsed's `limit > 0` guard is false.
		expect(screen.getByTestId("or-limit-fill")).toHaveStyle({ width: "0%" });
	});

	it("shows the reset-time sublabel for a positive limit, wired to the actual reset date", () => {
		const { unmount } = render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{
					...payload,
					limit: 100,
					limit_reset: "2026-08-01T00:00:00Z",
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("or-limit-reset")).toBeInTheDocument();
		expect(screen.queryByTestId("or-limit-reached")).toBeNull();
		const augustText = screen.getByTestId("or-limit-reset").textContent;
		unmount();

		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{
					...payload,
					limit: 100,
					limit_reset: "2026-12-25T00:00:00Z",
				}}
				barMode="used"
			/>,
		);
		const decemberText = screen.getByTestId("or-limit-reset").textContent;

		// Locale-independent: compares two live renders driven by different raw
		// `limit_reset` timestamps rather than asserting on translated text, but
		// still proves the sublabel is derived from the payload field instead of
		// being a hardcoded string.
		expect(decemberText).not.toBe(augustText);
	});

	it("draws a zero limit as an empty warning bar in both modes, dated by its reset", () => {
		// 0% used would otherwise derive a full, healthy bar in remaining mode
		// under a sublabel saying spending is blocked.
		for (const barMode of ["used", "remaining"] as const) {
			const { unmount } = render(
				<OpenRouterQuotaModal
					{...chrome}
					payload={{ ...payload, limit: 0 }}
					barMode={barMode}
				/>,
			);
			const fill = screen.getByTestId("or-limit-fill");
			expect(fill).toHaveStyle({ width: "0%" });
			expect(fill).toHaveClass("ui-quota-fill-warn");
			const reached = screen.getByTestId("or-limit-reached");
			expect(reached).toHaveTextContent(
				`${formatDollars(0)} ${i18n.t("components.providerModals.limitReset")}`,
			);
			expect(reached).toHaveTextContent(
				i18n.t("components.providerModals.resets", {
					when: `at ${payload.limit_reset}`,
				}),
			);
			unmount();
		}
	});

	it("shows the limit-reached sublabel testid for a zero-value limit", () => {
		render(
			<OpenRouterQuotaModal
				{...chrome}
				payload={{ ...payload, limit: 0 }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("or-limit-reached")).toBeInTheDocument();
		expect(screen.queryByTestId("or-limit-reset")).toBeNull();
	});
});

describe("NeuralWattQuotaModal", () => {
	// The blocks the tests below spread from are declared on their own, as
	// NonNullable, because the response type marks them optional (the payload is
	// an upstream provider body relayed by the fleet primary, so a block can be
	// absent). Spreading `payload.subscription` instead would widen every field
	// to `| undefined` and no longer typecheck.
	const limits: NonNullable<NeuralWattQuotaResponse["limits"]> = {
		overage_limit_usd: 25,
		rate_limit_tier: "standard",
	};
	const subscription: NonNullable<NeuralWattQuotaResponse["subscription"]> = {
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
	};
	const payload: NeuralWattQuotaResponse = {
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
				// Deliberately not a round thousand: toLocaleString("en-US") gives
				// "1,234" while formatCompact (used for the tokens slot) would give
				// "1.2K" for the same number, so a requests/tokens field swap at
				// render time produces a different, catchable string.
				requests: 1234,
				tokens: 1_000_000,
				energy_kwh: 3.21,
			},
		},
		limits,
		subscription,
		key: { name: "k", allowance: null },
	};

	it("renders the balance as a plain number with no credits bar", () => {
		// The credits bar is gone for good: total re-bases to remaining as
		// spend settles, so the bar could only ever render as untouched.
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByText("$30.00")).toBeInTheDocument();
		expect(screen.queryByTestId("nw-credits-fill")).toBeNull();
	});

	it("fills the energy bar from kWh used over included", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-kwh-fill")).toHaveStyle({ width: "25%" });
	});

	it("draws the remaining bar from the reported kWh left, not the complement of used", () => {
		// 5 used and 10 remaining of 20 included: the provider's two figures do
		// not add up, and remaining mode must show its own 50%, not 100 - 25.
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...subscription, kwh_remaining: 10 },
				}}
				barMode="remaining"
			/>,
		);
		expect(screen.getByTestId("nw-kwh-fill")).toHaveStyle({ width: "50%" });
	});

	it("omits the energy bar when the plan includes no kWh", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...subscription, kwh_included: 0 },
				}}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("nw-kwh-fill")).toBeNull();
	});

	it("renders unlimited when the key has no allowance", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-allowance")).toBeInTheDocument();
		expect(screen.getByTestId("nw-allowance")).not.toHaveTextContent("$");
	});

	it("renders a dollar allowance when the key has one", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{ ...payload, key: { name: "k", allowance: 25 } }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-allowance")).toHaveTextContent("$25.00");
	});

	it("flags an account in overage", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...subscription, in_overage: true },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-status-overage")).toBeInTheDocument();
	});

	it("shows the overage note only while in overage", () => {
		const { unmount } = render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...subscription, in_overage: true },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-overage-note")).toBeInTheDocument();
		unmount();

		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.queryByTestId("nw-overage-note")).toBeNull();
	});

	it("renders a dash when the balance is absent, never a fabricated $0.00", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					balance: { ...payload.balance, credits_remaining_usd: undefined },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByText("-")).toBeInTheDocument();
		expect(screen.queryByText("$0.00")).toBeNull();
	});

	it("never renders a spent-total caption", () => {
		// No cumulative draw exists in the payload (credits_used_usd is a
		// hardwired 0, total re-bases to remaining as spend settles), so the
		// caption slot under the credits bar stays empty.
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.queryByText(/spent in total/)).toBeNull();
	});

	it("renders both usage rows", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		// "1,234" (toLocaleString) is distinguishable from "1.2K" (formatCompact),
		// which is what a requests/tokens field swap would render instead.
		expect(screen.getByTestId("nw-usage-current")).toHaveTextContent("1,234");
		expect(screen.getByTestId("nw-usage-lifetime")).toHaveTextContent("1,200");
	});

	it("shows the plain subscription status when not in overage", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-status")).toHaveTextContent("active");
		expect(screen.queryByTestId("nw-status-overage")).toBeNull();
	});

	it("differs its auto-renew, accounting-method and overage-limit labels for a sparse payload", () => {
		const sparse: NeuralWattQuotaResponse = {
			...payload,
			balance: { ...payload.balance, accounting_method: "" },
			limits: { ...limits, overage_limit_usd: null },
			subscription: { ...subscription, auto_renew: false },
		};
		const { unmount } = render(
			<NeuralWattQuotaModal {...chrome} payload={sparse} barMode="used" />,
		);
		const sparseAutoRenew = screen.getByTestId("nw-auto-renew").textContent;
		const sparseAccounting = screen.getByTestId(
			"nw-accounting-method",
		).textContent;
		const sparseOverageLimit =
			screen.getByTestId("nw-overage-limit").textContent;
		unmount();

		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		const baseAutoRenew = screen.getByTestId("nw-auto-renew").textContent;
		const baseAccounting = screen.getByTestId(
			"nw-accounting-method",
		).textContent;
		const baseOverageLimit = screen.getByTestId("nw-overage-limit").textContent;

		// Locale-independent: compares two live renders rather than asserting on
		// translated text, but still proves each fallback/ternary is wired to its
		// source field instead of being hardcoded to one arm.
		expect(sparseAutoRenew).not.toBe(baseAutoRenew);
		expect(sparseAccounting).not.toBe(baseAccounting);
		expect(sparseOverageLimit).not.toBe(baseOverageLimit);
	});

	// Regression: a 200 whose body stops after balance and subscription passes
	// payloadOf and the badge visibility gate (which only requires
	// balance.credits_remaining_usd), so the badge renders. A modal that throws
	// on usage.current_month.cost_usd disappears, and the error boundary that
	// wraps only the modal stays latched until a new snapshot or another
	// provider resets it; the badges survive.
	it("renders the credits it has when usage, limits and key are absent", () => {
		const partial: NeuralWattQuotaResponse = {
			balance: payload.balance,
			subscription,
		};
		render(
			<NeuralWattQuotaModal {...chrome} payload={partial} barMode="used" />,
		);
		// What it does have is still shown: the $30 balance.
		expect(screen.getByText("$30.00")).toBeInTheDocument();
		expect(screen.getByTestId("nw-status")).toHaveTextContent("active");
		// What it does not have is left out entirely, rather than rendered as
		// rows of dashes.
		expect(screen.queryByTestId("nw-usage-current")).toBeNull();
		expect(screen.queryByTestId("nw-usage-lifetime")).toBeNull();
		expect(screen.queryByTestId("nw-overage-limit")).toBeNull();
		expect(screen.queryByTestId("nw-allowance")).toBeNull();
		// Their containers go with them: an empty row block or an empty
		// three-column grid would still occupy layout in the dialog.
		expect(document.querySelector(".ui-quota-rows")).toBeNull();
		expect(document.querySelector(".ui-quota-detail-grid-3")).toBeNull();
	});

	it("renders on balance alone, with no subscription block", () => {
		const balanceOnly: NeuralWattQuotaResponse = { balance: payload.balance };
		render(
			<NeuralWattQuotaModal {...chrome} payload={balanceOnly} barMode="used" />,
		);
		expect(screen.getByText("$30.00")).toBeInTheDocument();
		// The accounting method rides on `balance`, so it survives on its own.
		expect(screen.getByTestId("nw-accounting-method")).toBeInTheDocument();
		expect(screen.queryByTestId("nw-status")).toBeNull();
		expect(screen.queryByTestId("nw-status-overage")).toBeNull();
		expect(screen.queryByTestId("nw-kwh-fill")).toBeNull();
		expect(screen.queryByTestId("nw-auto-renew")).toBeNull();
	});

	// A block that is present but incomplete is its own case: the container
	// existing says nothing about its siblings, so each row has to stand on the
	// object it actually reads.
	it("keeps whichever usage period is reported and drops the other", () => {
		const currentOnly: NeuralWattQuotaResponse = {
			balance: payload.balance,
			usage: { current_month: payload.usage?.current_month },
		};
		const { unmount } = render(
			<NeuralWattQuotaModal {...chrome} payload={currentOnly} barMode="used" />,
		);
		expect(screen.getByTestId("nw-usage-current")).toHaveTextContent("1,234");
		expect(screen.queryByTestId("nw-usage-lifetime")).toBeNull();
		unmount();

		const lifetimeOnly: NeuralWattQuotaResponse = {
			balance: payload.balance,
			usage: { lifetime: payload.usage?.lifetime },
		};
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={lifetimeOnly}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-usage-lifetime")).toHaveTextContent("1,200");
		expect(screen.queryByTestId("nw-usage-current")).toBeNull();
	});

	it("keeps the limits rows without a key block, and the allowance without limits", () => {
		const limitsOnly: NeuralWattQuotaResponse = {
			balance: payload.balance,
			limits,
		};
		const { unmount } = render(
			<NeuralWattQuotaModal {...chrome} payload={limitsOnly} barMode="used" />,
		);
		expect(screen.getByTestId("nw-overage-limit")).toHaveTextContent("$25.00");
		expect(screen.queryByTestId("nw-allowance")).toBeNull();
		unmount();

		const keyOnly: NeuralWattQuotaResponse = {
			balance: payload.balance,
			key: { name: "k", allowance: 40 },
		};
		render(
			<NeuralWattQuotaModal {...chrome} payload={keyOnly} barMode="used" />,
		);
		expect(screen.getByTestId("nw-allowance")).toHaveTextContent("$40.00");
		expect(screen.queryByTestId("nw-overage-limit")).toBeNull();
	});

	// A block that is present but missing fields: each figure needs its own
	// numbers, or its formatter would throw and take the modal down.
	it("renders a limits block that reports only its tier", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					balance: payload.balance,
					limits: { rate_limit_tier: "standard" },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-rate-limit-tier")).toHaveTextContent(
			"standard",
		);
		expect(screen.queryByTestId("nw-overage-limit")).toBeNull();
	});

	it("leaves out a key block that reports no allowance", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{ balance: payload.balance, key: { name: "k" } }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("nw-allowance")).toBeNull();
		expect(document.querySelector(".ui-quota-detail-grid-3")).toBeNull();
	});

	it("summarises a usage period from the figures it reports, dropping an empty one", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					balance: payload.balance,
					usage: { current_month: { requests: 1234 }, lifetime: {} },
				}}
				barMode="used"
			/>,
		);
		const row = screen.getByTestId("nw-usage-current");
		expect(row).toHaveTextContent(formatCount(1234));
		expect(row).not.toHaveTextContent("kWh");
		expect(row).not.toHaveTextContent("·");
		expect(screen.queryByTestId("nw-usage-lifetime")).toBeNull();
	});

	it("draws the energy bar from a subscription that reports only used and included kWh", () => {
		const sparse: NeuralWattQuotaResponse = {
			balance: payload.balance,
			subscription: { kwh_included: 20, kwh_used: 5 },
		};
		const { unmount } = render(
			<NeuralWattQuotaModal {...chrome} payload={sparse} barMode="remaining" />,
		);
		// No remaining figure came, so remaining mode falls back to the complement.
		expect(screen.getByTestId("nw-kwh-fill")).toHaveStyle({ width: "75%" });
		expect(screen.queryByTestId("nw-kwh-remaining")).toBeNull();
		expect(screen.queryByTestId("nw-kwh-reset")).toBeNull();
		expect(screen.queryByTestId("nw-status")).toBeNull();
		expect(screen.queryByTestId("nw-auto-renew")).toBeNull();
		unmount();

		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					balance: payload.balance,
					subscription: { kwh_included: 20 },
				}}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("nw-kwh-fill")).toBeNull();
	});

	it("dates the energy bar's reset from the billing period end", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-kwh-reset")).toHaveTextContent(
			i18n.t("components.providerModals.resets", {
				when: `at ${subscription.current_period_end}`,
			}),
		);
		expect(screen.getByTestId("nw-kwh-remaining")).toBeInTheDocument();
	});
});
