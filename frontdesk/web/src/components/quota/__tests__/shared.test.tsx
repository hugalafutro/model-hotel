import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as Body from "@web-shared/quota/modals";
import i18next from "i18next";
import type { ComponentType } from "react";
import { describe, expect, it, vi } from "vitest";
import { OpenCodeGoQuotaModal } from "../modals";
import { QuotaModalShell } from "../shared";

describe("QuotaModalShell", () => {
	function renderShell(
		over: Partial<Parameters<typeof QuotaModalShell>[0]> = {},
	) {
		const props = {
			title: "NanoGPT",
			barMode: "remaining" as const,
			onToggleBarMode: vi.fn(),
			onRefresh: vi.fn(),
			isRefreshing: false,
			fetchedAt: "2026-07-26T10:00:00Z",
			onClose: vi.fn(),
			children: <p data-testid="body">body</p>,
			...over,
		};
		render(<QuotaModalShell {...props} />);
		return props;
	}

	it("renders the title, body and snapshot stamp", () => {
		renderShell();
		expect(screen.getByRole("dialog")).toHaveAttribute("aria-label", "NanoGPT");
		expect(screen.getByTestId("body")).toBeInTheDocument();
		expect(screen.getByTestId("quota-modal-fetched-at")).toBeInTheDocument();
		// The default fixture renders with isRefreshing: false, so the refresh
		// icon must not carry the spin class here. getAttribute returns null
		// when isRefreshing is false (className is undefined, not "undefined"),
		// so fall back to "" before asserting.
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class") ?? "").not.toContain("fd-spin");
	});

	it("toggles bar mode when the toggle is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(props.onToggleBarMode).toHaveBeenCalledTimes(1);
	});

	it("refreshes when the refresh button is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-refresh"));
		expect(props.onRefresh).toHaveBeenCalledTimes(1);
	});

	it("disables the refresh button while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		expect(screen.getByTestId("quota-modal-refresh")).toBeDisabled();
	});

	it("renders a subtitle when given", () => {
		renderShell({ subtitle: <span data-testid="sub">pro</span> });
		expect(screen.getByTestId("sub")).toBeInTheDocument();
	});

	it("closes when the close button is pressed", async () => {
		const props = renderShell();
		await userEvent.click(screen.getByTestId("quota-modal-close"));
		expect(props.onClose).toHaveBeenCalledTimes(1);
	});

	it("keeps initial focus on the bar-mode toggle, with close last in the header", () => {
		// Modal focuses the first focusable control on open and headerActions
		// renders before the body, so close has to stay LAST or opening a quota
		// modal would land the keyboard on a dismiss button.
		renderShell();
		expect(screen.getByTestId("quota-modal-toggle")).toHaveFocus();
		const ids = Array.from(
			screen.getByRole("dialog").querySelectorAll("button"),
		).map((b) => b.getAttribute("data-testid"));
		expect(ids).toEqual([
			"quota-modal-toggle",
			"quota-modal-refresh",
			"quota-modal-close",
		]);
	});

	it("spins the refresh icon while a refresh is in flight", () => {
		renderShell({ isRefreshing: true });
		const icon = screen.getByTestId("quota-modal-refresh").querySelector("svg");
		expect(icon?.getAttribute("class")).toContain("fd-spin");
	});
});

// The bindings hand each shared body Front Desk's shell and its `quota.modal`
// strings; the bodies themselves are covered in the dashboard's suites.
describe("quota modal bindings", () => {
	it("wraps a shared body in the Front Desk shell with this app's strings", async () => {
		const onToggleBarMode = vi.fn();
		render(
			<OpenCodeGoQuotaModal
				providerName="acct"
				payload={{ usage: { weekly: { status: "ok", percent: 30 } } }}
				barMode="used"
				fetchedAt="2026-07-26T10:00:00Z"
				onToggleBarMode={onToggleBarMode}
				onRefresh={vi.fn()}
				isRefreshing={false}
				onClose={vi.fn()}
			/>,
		);
		expect(screen.getByRole("dialog")).toHaveAttribute(
			"aria-label",
			i18next.t("quota.modal.openCodeGoPlanQuota", { provider: "acct" }),
		);
		expect(screen.getByTestId("opencode-go-weekly-fill")).toHaveStyle({
			width: "30%",
		});
		expect(screen.getByTestId("quota-modal-fetched-at")).toBeInTheDocument();
		await userEvent.click(screen.getByTestId("quota-modal-toggle"));
		expect(onToggleBarMode).toHaveBeenCalledTimes(1);
	});
});

// The shared bodies' own suites run against the dashboard's strings, so this is
// what proves every key a body asks for also exists under Front Desk's
// `quota.modal` prefix: a missing one would render as a raw key path here.
describe("quota modal translation contract", () => {
	const reset = "2026-08-01T00:00:00Z";
	// Typed at `never` so one table can hold every body; each payload is cast
	// back to it at render time.
	const cases: [string, ComponentType<Body.QuotaBodyProps<never>>, unknown][] =
		[
			[
				"Kimi Code",
				Body.KimiCodeQuotaModal,
				{
					user: { membership: { level: "pro" } },
					limits: [
						{
							window: { timeUnit: "TIME_UNIT_MINUTE", duration: 300 },
							detail: { limit: "1000", remaining: "250", resetTime: reset },
						},
					],
					usage: { limit: "5000", remaining: "4000", resetTime: reset },
					parallel: { limit: "4" },
					totalQuota: { limit: "9000", remaining: "8000" },
				},
			],
			[
				"MiniMax",
				Body.MiniMaxQuotaModal,
				{
					base_resp: { status_code: 0, status_msg: "ok" },
					model_remains: [
						{
							model_name: "general",
							start_time: 0,
							end_time: 18_000_000,
							current_interval_status: 1,
							current_interval_remaining_percent: 70,
							remains_time: 3_600_000,
							current_weekly_status: 1,
							current_weekly_remaining_percent: 30,
							weekly_remains_time: 86_400_000,
						},
						{
							model_name: "video",
							current_interval_status: 3,
							current_interval_remaining_percent: 0,
							remains_time: 0,
							current_weekly_status: 3,
							current_weekly_remaining_percent: 0,
							weekly_remains_time: 0,
						},
					],
				},
			],
			[
				"NanoGPT",
				Body.NanoGPTQuotaModal,
				{
					active: true,
					provider: "nanogpt",
					providerStatus: "active",
					cancelAtPeriodEnd: true,
					limits: {
						weeklyInputTokens: 1000,
						dailyInputTokens: null,
						dailyImages: 50,
					},
					allowOverage: true,
					period: { currentPeriodEnd: reset },
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
				},
			],
			// The inactive subscription, the unset weekly limit and the disabled
			// overage are the branches the full fixture above never takes.
			[
				"NanoGPT (inactive, no weekly limit)",
				Body.NanoGPTQuotaModal,
				{
					active: false,
					provider: "nanogpt",
					providerStatus: "inactive",
					cancelAtPeriodEnd: false,
					limits: { weeklyInputTokens: 0 },
					allowOverage: false,
					weeklyInputTokens: { used: 0, remaining: 0, percentUsed: 0 },
				},
			],
			[
				"NeuralWatt",
				Body.NeuralWattQuotaModal,
				{
					balance: {
						credits_remaining_usd: 30,
						total_credits_usd: 30,
						credits_used_usd: 0,
						accounting_method: "",
					},
					usage: {
						current_month: {
							cost_usd: 1,
							requests: 2,
							tokens: 3,
							energy_kwh: 4,
						},
						lifetime: { cost_usd: 5, requests: 6, tokens: 7, energy_kwh: 8 },
					},
					limits: { overage_limit_usd: null, rate_limit_tier: "standard" },
					subscription: {
						plan: "pro",
						status: "active",
						billing_interval: "month",
						current_period_start: "2026-07-01T00:00:00Z",
						current_period_end: reset,
						auto_renew: false,
						kwh_included: 20,
						kwh_used: 20,
						kwh_remaining: 0,
						in_overage: true,
					},
					key: { name: "k", allowance: null },
				},
			],
			[
				"OpenCode Go",
				Body.OpenCodeGoQuotaModal,
				{
					usage: {
						rolling: { status: "ok", percent: 12, resetsAt: reset },
						weekly: { status: "ok", percent: 34 },
						monthly: { status: "exhausted", percent: 100 },
					},
				},
			],
			[
				"OpenRouter",
				Body.OpenRouterQuotaModal,
				{
					label: "k",
					limit: 0,
					limit_reset: reset,
					limit_remaining: 0,
					usage: 40,
					usage_daily: 1,
					usage_weekly: 5,
					usage_monthly: 20,
					credits_total: 0,
					credits_used: 0,
					credits_remaining: 0,
					is_free_tier: true,
				},
			],
			// A paid key with a credit total and a positive spending limit: the
			// tier label, credit bar and reset sublabel the free fixture skips.
			[
				"OpenRouter (paid)",
				Body.OpenRouterQuotaModal,
				{
					limit: 100,
					limit_reset: reset,
					limit_remaining: 25,
					credits_total: 10,
					credits_used: 5,
					credits_remaining: 5,
					is_free_tier: false,
				},
			],
			[
				"Z.ai Coding",
				Body.ZAICodingQuotaModal,
				{
					success: true,
					data: {
						level: "pro",
						limits: [
							{
								type: "TOKENS_LIMIT",
								unit: 3,
								percentage: 40,
								nextResetTime: 1_800_000_000_000,
							},
							{ type: "TOKENS_LIMIT", unit: 6, percentage: 10 },
							{
								type: "TIME_LIMIT",
								unit: 5,
								percentage: 25,
								usageDetails: [{ modelCode: "glm-4", usage: 7 }],
							},
						],
					},
				},
			],
		];

	it.each(cases)(
		"%s asks only for keys Front Desk defines",
		(_, QuotaBody, payload) => {
			const asked: [string, Record<string, unknown> | undefined][] = [];
			for (const barMode of ["used", "remaining"] as const) {
				const { unmount } = render(
					<QuotaBody
						providerName="acct"
						payload={payload as never}
						barMode={barMode}
						fetchedAt="2026-07-26T10:00:00Z"
						kit={{
							t: (key, opts) => {
								asked.push([key, opts]);
								return i18next.t(`quota.modal.${key}`, opts);
							},
							time: { absolute: (iso) => iso ?? "-", relative: () => "" },
							shell: ({ title, children }) => (
								<div role="dialog" aria-label={title}>
									{children}
								</div>
							),
						}}
					/>,
				);
				unmount();
			}
			expect(asked.length).toBeGreaterThan(0);
			const missing = asked
				.filter(([key, opts]) => !i18next.exists(`quota.modal.${key}`, opts))
				.map(([key]) => key);
			expect(missing).toEqual([]);
		},
	);
});
