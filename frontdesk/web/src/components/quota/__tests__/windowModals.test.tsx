import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	ZAICodingQuotaResponse,
} from "../../../api/types";
import { KimiCodeQuotaModal } from "../KimiCodeQuotaModal";
import { MiniMaxQuotaModal } from "../MiniMaxQuotaModal";
import { ZAICodingQuotaModal } from "../ZAICodingQuotaModal";

const chrome = {
	providerName: "acct",
	fetchedAt: "2026-07-26T10:00:00Z",
	onToggleBarMode: vi.fn(),
	onRefresh: vi.fn(),
	isRefreshing: false,
	onClose: vi.fn(),
};

describe("ZAICodingQuotaModal", () => {
	const payload: ZAICodingQuotaResponse = {
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
	};

	it("renders all three bars filled by used share", () => {
		render(
			<ZAICodingQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("zai-5h-fill")).toHaveStyle({ width: "40%" });
		expect(screen.getByTestId("zai-weekly-fill")).toHaveStyle({ width: "10%" });
		expect(screen.getByTestId("zai-mcp-fill")).toHaveStyle({ width: "25%" });
	});

	it("inverts the fills in remaining mode", () => {
		render(
			<ZAICodingQuotaModal {...chrome} payload={payload} barMode="remaining" />,
		);
		expect(screen.getByTestId("zai-5h-fill")).toHaveStyle({ width: "60%" });
	});

	it("renders the MCP per-model usage rows", () => {
		render(
			<ZAICodingQuotaModal {...chrome} payload={payload} barMode="used" />,
		);
		expect(screen.getByTestId("zai-mcp-detail-glm-4")).toHaveTextContent("7");
	});

	it("omits a bar whose window is absent", () => {
		render(
			<ZAICodingQuotaModal
				{...chrome}
				payload={{ success: true, data: { limits: [] } }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("zai-5h-fill")).toBeNull();
		expect(screen.queryByTestId("zai-weekly-fill")).toBeNull();
	});
});

describe("KimiCodeQuotaModal", () => {
	const payload: KimiCodeQuotaResponse = {
		user: { membership: { level: "pro" } },
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
			remaining: "4000",
			resetTime: "2026-08-01T00:00:00Z",
		},
		parallel: { limit: "4" },
		totalQuota: { limit: "9000", remaining: "8000" },
	};

	it("renders both windows from the parsed string counters", () => {
		render(<KimiCodeQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("kimi-5h-fill")).toHaveStyle({ width: "75%" });
		expect(screen.getByTestId("kimi-weekly-fill")).toHaveStyle({
			width: "20%",
		});
	});

	it("renders the parallel limit and total quota rows", () => {
		render(<KimiCodeQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("kimi-parallel")).toHaveTextContent("4");
		expect(screen.getByTestId("kimi-total-quota")).toHaveTextContent("8000");
	});

	it("omits the extras block when neither extra is reported", () => {
		render(
			<KimiCodeQuotaModal
				{...chrome}
				payload={{ usage: payload.usage }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("kimi-parallel")).toBeNull();
		expect(screen.queryByTestId("kimi-total-quota")).toBeNull();
	});
});

describe("MiniMaxQuotaModal", () => {
	const payload: MiniMaxQuotaResponse = {
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
	};

	it("converts remaining percentages into used fills", () => {
		render(<MiniMaxQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("minimax-general-5h-fill")).toHaveStyle({
			width: "30%",
		});
		expect(screen.getByTestId("minimax-general-weekly-fill")).toHaveStyle({
			width: "70%",
		});
	});

	it("renders a not-in-plan placeholder instead of bars for an excluded class", () => {
		render(<MiniMaxQuotaModal {...chrome} payload={payload} barMode="used" />);
		expect(screen.getByTestId("minimax-video-not-in-plan")).toBeInTheDocument();
		expect(screen.queryByTestId("minimax-video-5h-fill")).toBeNull();
	});

	it("derives the interval length from the window bounds", () => {
		render(<MiniMaxQuotaModal {...chrome} payload={payload} barMode="used" />);
		// end_time - start_time is 18,000,000 ms, i.e. 5 hours.
		expect(screen.getByTestId("minimax-general-5h-label")).toHaveTextContent(
			"5",
		);
	});

	it("renders nothing per class when model_remains is null", () => {
		render(
			<MiniMaxQuotaModal
				{...chrome}
				payload={{
					base_resp: { status_code: 0, status_msg: "ok" },
					model_remains: null,
				}}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("minimax-general-5h-fill")).toBeNull();
	});
});
