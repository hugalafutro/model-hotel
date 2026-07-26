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
			"1.5/10",
		);
	});

	it("renders a dash and the degraded class when the fetch failed", () => {
		const m = model({ degraded: true });
		m.snapshot.http_status = 502;
		render(<QuotaBadge model={m} barMode="remaining" onClick={vi.fn()} />);
		const badge = screen.getByTestId("quota-badge-nanogpt:p");
		expect(badge).toHaveTextContent("-");
		expect(badge.className).toContain("fd-quota-pill-degraded");
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
