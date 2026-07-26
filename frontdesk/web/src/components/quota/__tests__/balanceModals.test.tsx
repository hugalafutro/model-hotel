import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OpenRouterBalance,
} from "../../../api/types";
import { NanoGPTQuotaModal } from "../NanoGPTQuotaModal";
import { NeuralWattQuotaModal } from "../NeuralWattQuotaModal";
import { OpenRouterQuotaModal } from "../OpenRouterQuotaModal";

const chrome = {
	providerName: "acct",
	fetchedAt: "2026-07-26T10:00:00Z",
	onToggleBarMode: vi.fn(),
	onRefresh: vi.fn(),
	isRefreshing: false,
	onClose: vi.fn(),
};

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

	it("renders the four spending tiles", () => {
		render(
			<OpenRouterQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("or-usage-daily")).toHaveTextContent("$1.00");
		expect(screen.getByTestId("or-usage-weekly")).toHaveTextContent("$5.00");
		expect(screen.getByTestId("or-usage-monthly")).toHaveTextContent("$20.00");
		expect(screen.getByTestId("or-usage-all")).toHaveTextContent("$40.00");
	});
});

describe("NeuralWattQuotaModal", () => {
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
				requests: 300,
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
	};

	it("fills the credit bar from credits used over total", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-credits-fill")).toHaveStyle({ width: "70%" });
	});

	it("fills the energy bar from kWh used over included", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-kwh-fill")).toHaveStyle({ width: "25%" });
	});

	it("omits the energy bar when the plan includes no kWh", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...payload.subscription, kwh_included: 0 },
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
	});

	it("flags an account in overage", () => {
		render(
			<NeuralWattQuotaModal
				{...chrome}
				payload={{
					...payload,
					subscription: { ...payload.subscription, in_overage: true },
				}}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("nw-status-overage")).toBeInTheDocument();
	});

	it("renders both usage rows", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-usage-current")).toHaveTextContent("300");
		expect(screen.getByTestId("nw-usage-lifetime")).toHaveTextContent("1,200");
	});

	it("shows the plain subscription status when not in overage", () => {
		render(
			<NeuralWattQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("nw-status")).toHaveTextContent("active");
		expect(screen.queryByTestId("nw-status-overage")).toBeNull();
	});
});
