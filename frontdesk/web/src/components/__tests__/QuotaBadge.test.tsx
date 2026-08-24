import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import type { QuotaBadgeModel } from "../../utils/quota";
import { QuotaBadge } from "../QuotaBadge";

function model(
	over: Partial<QuotaBadgeModel> = {},
	payload: unknown = {},
): QuotaBadgeModel {
	const snapshot: QuotaSnapshot = {
		provider_name: over.providerName ?? "p",
		type: over.type ?? "nanogpt",
		kind: "usage",
		payload,
		http_status: 200,
		fetched_at: "2026-07-26T10:00:00Z",
	};
	return {
		key: `${snapshot.type}:${snapshot.provider_name}`,
		type: snapshot.type as QuotaBadgeModel["type"],
		providerName: snapshot.provider_name,
		showProviderName: false,
		degraded: false,
		snapshot,
		...over,
	};
}

describe("QuotaBadge", () => {
	it("renders the type prefix when the type is unambiguous", () => {
		render(
			<QuotaBadge
				model={model(
					{},
					{
						limits: { weeklyInputTokens: 1000 },
						weeklyInputTokens: {
							used: 250,
							remaining: 750,
							percentUsed: 0.25,
							resetAt: 0,
						},
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		const badge = screen.getByTestId("quota-badge-nanogpt:p");
		expect(badge).toHaveTextContent("NG");
		expect(badge).not.toHaveTextContent("p");
	});

	it("appends the provider name when the type collides", () => {
		render(
			<QuotaBadge
				model={model(
					{ showProviderName: true, providerName: "nano-a" },
					{
						limits: { weeklyInputTokens: 1000 },
						weeklyInputTokens: {
							used: 250,
							remaining: 750,
							percentUsed: 0.25,
							resetAt: 0,
						},
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-nanogpt:nano-a")).toHaveTextContent(
			"nano-a",
		);
	});

	it("shows remaining tokens over the limit in remaining mode", () => {
		render(
			<QuotaBadge
				model={model(
					{},
					{
						limits: { weeklyInputTokens: 1000 },
						weeklyInputTokens: {
							used: 250,
							remaining: 750,
							percentUsed: 0.25,
							resetAt: 0,
						},
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-nanogpt:p")).toHaveTextContent(
			"750/1K",
		);
	});

	it("shows used tokens over the limit in used mode", () => {
		render(
			<QuotaBadge
				model={model(
					{},
					{
						limits: { weeklyInputTokens: 1000 },
						weeklyInputTokens: {
							used: 250,
							remaining: 750,
							percentUsed: 0.25,
							resetAt: 0,
						},
					},
				)}
				barMode="used"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-nanogpt:p")).toHaveTextContent(
			"250/1K",
		);
	});

	it("renders the two window percentages for Z.ai", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "zai-coding" },
					{
						success: true,
						data: {
							limits: [
								{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 },
								{ type: "TOKENS_LIMIT", unit: 6, percentage: 10 },
							],
						},
					},
				)}
				barMode="used"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-zai-coding:p")).toHaveTextContent(
			"40%/10%",
		);
	});

	it("inverts window percentages in remaining mode", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "zai-coding" },
					{
						success: true,
						data: {
							limits: [
								{ type: "TOKENS_LIMIT", unit: 3, percentage: 40 },
								{ type: "TOKENS_LIMIT", unit: 6, percentage: 10 },
							],
						},
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-zai-coding:p")).toHaveTextContent(
			"60%/90%",
		);
	});

	it("renders a dash when a window is absent", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "kimi-code" },
					{
						usage: { limit: "100", remaining: "40", resetTime: "" },
					},
				)}
				barMode="used"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-kimi-code:p")).toHaveTextContent(
			"-/60%",
		);
	});

	it("renders the two window percentages for MiniMax", () => {
		// MiniMax reports REMAINING per window, so a 70%-left 5h window and a
		// 30%-left weekly window render as 30%/70% used. The two figures are
		// deliberately asymmetric: swapping the 5h and weekly getters in
		// QuotaBadge fails this assertion.
		render(
			<QuotaBadge
				model={model(
					{ type: "minimax" },
					{
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
				)}
				barMode="used"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-minimax:p")).toHaveTextContent(
			"30%/70%",
		);
	});

	it("renders a dash for the NanoGPT limit when no weekly limit is set", () => {
		render(
			<QuotaBadge
				model={model({}, { weeklyInputTokens: { used: 0 } })}
				barMode="used"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-nanogpt:p")).toHaveTextContent(
			"0/-",
		);
	});

	it("renders DeepSeek USD balance", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "deepseek" },
					{
						is_available: true,
						balance_infos: [{ currency: "USD", total_balance: "12.34" }],
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-deepseek:p")).toHaveTextContent(
			"$12.34",
		);
	});

	it("renders a dash for DeepSeek when no USD balance entry is found", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "deepseek" },
					{
						is_available: true,
						balance_infos: [],
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-deepseek:p")).toHaveTextContent(
			"$-",
		);
	});

	it("renders OpenRouter credits remaining", () => {
		render(
			<QuotaBadge
				model={model({ type: "openrouter" }, { credits_remaining: 8.5 })}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-openrouter:p")).toHaveTextContent(
			"$8.50",
		);
	});

	it("renders Ollama Cloud plan", () => {
		render(
			<QuotaBadge
				model={model({ type: "ollama-cloud" }, { plan: "pro" })}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-ollama-cloud:p")).toHaveTextContent(
			"pro",
		);
	});

	it("renders a dash for Ollama Cloud when the plan is empty", () => {
		render(
			<QuotaBadge
				model={model({ type: "ollama-cloud" }, { plan: "" })}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-ollama-cloud:p")).toHaveTextContent(
			"-",
		);
	});

	it("renders NeuralWatt energy used", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "neuralwatt" },
					{
						subscription: { kwh_used: 1.5, kwh_included: 10 },
					},
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(screen.getByTestId("quota-badge-neuralwatt:p")).toHaveTextContent(
			"1.5/10 kWh",
		);
	});

	it("renders NeuralWatt energy used alone when the plan includes no allowance", () => {
		render(
			<QuotaBadge
				model={model(
					{ type: "neuralwatt" },
					{ subscription: { kwh_used: 1.5, kwh_included: 0 } },
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		const badge = screen.getByTestId("quota-badge-neuralwatt:p");
		expect(badge).toHaveTextContent("1.5 kWh");
		expect(badge).not.toHaveTextContent("/");
	});

	it("switches the NeuralWatt tooltip to the overage wording without dollar figures", () => {
		// NeuralWatt exposes no cumulative credit draw (credits_used_usd is a
		// hardwired 0 and total re-bases to remaining as spend settles), so
		// the overage tooltip explains the situation and names no amount.
		const subscription = {
			kwh_used: 2.37,
			kwh_included: 2.35,
			kwh_remaining: 0,
			in_overage: true,
		};
		const { unmount } = render(
			<QuotaBadge
				model={model({ type: "neuralwatt" }, { subscription })}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		const overageTitle = screen
			.getByTestId("quota-badge-neuralwatt:p")
			.getAttribute("title");
		expect(overageTitle).not.toContain("$");
		unmount();

		render(
			<QuotaBadge
				model={model(
					{ type: "neuralwatt" },
					{ subscription: { ...subscription, in_overage: false } },
				)}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		const plainTitle = screen
			.getByTestId("quota-badge-neuralwatt:p")
			.getAttribute("title");
		expect(plainTitle).not.toBe(overageTitle);
	});

	it("renders a dash and the degraded class when the fetch failed", () => {
		const m = model({ degraded: true });
		m.snapshot.http_status = 502;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const badge = screen.getByTestId("quota-badge-nanogpt:p");
		expect(badge).toHaveTextContent("-");
		expect(badge.className).toContain("fd-quota-pill-degraded");
	});

	it("names the HTTP status in the tooltip when the fetch actually failed", () => {
		const m = model({ degraded: true });
		m.snapshot.http_status = 502;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const title = screen
			.getByTestId("quota-badge-nanogpt:p")
			.getAttribute("title");
		expect(title).toContain("502");
		expect(title).toContain("failed");
	});

	it("does not claim the fetch failed when it returned 200 with an unusable body", () => {
		// payloadOf() rejects a 200 whose body is not a usable object, so the badge
		// is degraded even though the fetch succeeded. Reusing the failure message
		// here produced "last fetch failed (HTTP 200)", which contradicts itself.
		// Asserting only on the testid/class would pass either way, so this pins
		// the wording: no "200", no "failed".
		const m = model({ degraded: true }, null);
		m.snapshot.http_status = 200;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const badge = screen.getByTestId("quota-badge-nanogpt:p");
		const title = badge.getAttribute("title");
		expect(badge).toHaveTextContent("-");
		expect(badge.className).toContain("fd-quota-pill-degraded");
		expect(title).not.toContain("200");
		expect(title).not.toContain("failed");
		expect(title).toContain("unreadable");
	});

	it("does not claim a failure for a 204, which is a successful empty quota", () => {
		// internal/api/quota_snapshot.go:90 emits 204 with a null body for a
		// NeuralWatt free-tier account: the fetch succeeded and there is simply
		// no quota. Reporting that as "last fetch failed (HTTP 204)" describes a
		// working account as broken. It is not "unreadable" either - nothing was
		// unreadable - so it gets its own wording.
		const m = model({ degraded: true, type: "neuralwatt" }, null);
		m.snapshot.http_status = 204;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const title = screen
			.getByTestId("quota-badge-neuralwatt:p")
			.getAttribute("title");
		expect(title).not.toContain("failed");
		expect(title).not.toContain("204");
		expect(title).not.toContain("unreadable");
		expect(title).toContain("no quota");
	});

	it("treats a 200 carrying a JSON array as unreadable, not as a failed fetch", () => {
		// typeof [] === "object", so the array guard in payloadOf() is what keeps
		// this off the healthy path.
		const m = model({ degraded: true }, []);
		m.snapshot.http_status = 200;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const title = screen
			.getByTestId("quota-badge-nanogpt:p")
			.getAttribute("title");
		expect(title).toContain("unreadable");
		expect(title).not.toContain("200");
	});

	it("omits the inline brand colour on a degraded badge so the CSS rule can grey it", () => {
		// An inline custom property outranks any author rule, so emitting one here
		// would make `.fd-quota-pill-degraded { --quota-brand: ... }` dead and a
		// failed provider would keep its full brand colour. Asserting the class is
		// present is not enough: the class had no effect before this.
		const m = model({ degraded: true });
		m.snapshot.http_status = 502;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const badge = screen.getByTestId("quota-badge-nanogpt:p");
		expect(badge.style.getPropertyValue("--quota-brand")).toBe("");
	});

	it("sets the inline brand colour on a healthy badge", () => {
		render(
			<QuotaBadge
				model={model({ type: "deepseek" }, { is_available: true })}
				barMode="remaining"
				onClick={vi.fn()}
			/>,
		);
		expect(
			screen
				.getByTestId("quota-badge-deepseek:p")
				.style.getPropertyValue("--quota-brand"),
		).toBe("#4D6BFE");
	});

	it("calls onClick when pressed", async () => {
		const onClick = vi.fn();
		render(
			<QuotaBadge
				model={model(
					{},
					{
						limits: { weeklyInputTokens: 1000 },
						weeklyInputTokens: {
							used: 250,
							remaining: 750,
							percentUsed: 0.25,
							resetAt: 0,
						},
					},
				)}
				barMode="remaining"
				onClick={onClick}
			/>,
		);
		await userEvent.click(screen.getByTestId("quota-badge-nanogpt:p"));
		expect(onClick).toHaveBeenCalledTimes(1);
	});
});
