import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	ZAICodingQuotaResponse,
} from "../../../api/types";
import { formatAbsolute } from "../../../utils/time";
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
		const row = screen.getByTestId("zai-mcp-detail-glm-4");
		expect(row).toHaveTextContent("glm-4");
		expect(row).toHaveTextContent("7");
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
		// Full "remaining / limit" order, not just a substring: a
		// remaining/limit field swap would still contain "8000" and pass a
		// looser assertion.
		expect(screen.getByTestId("kimi-total-quota")).toHaveTextContent(
			"8000 / 9000",
		);
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

	it("renders only the parallel row when total quota is absent", () => {
		render(
			<KimiCodeQuotaModal
				{...chrome}
				payload={{ usage: payload.usage, parallel: payload.parallel }}
				barMode="used"
			/>,
		);
		expect(screen.getByTestId("kimi-parallel")).toHaveTextContent("4");
		expect(screen.queryByTestId("kimi-total-quota")).toBeNull();
	});

	it("renders only the total quota row, falling back to '-' for a missing field, when parallel is absent", () => {
		render(
			<KimiCodeQuotaModal
				{...chrome}
				payload={{ usage: payload.usage, totalQuota: { remaining: "8000" } }}
				barMode="used"
			/>,
		);
		expect(screen.queryByTestId("kimi-parallel")).toBeNull();
		expect(screen.getByTestId("kimi-total-quota")).toHaveTextContent(
			"8000 / -",
		);
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
			{
				// A second active class with a 24 hour window, distinct from
				// "general"'s 5 hour window: proves the label is DERIVED from
				// start_time/end_time rather than the 5 hour fallback, which
				// "general" alone cannot distinguish (its own window is 5 hours,
				// same as the fallback).
				model_name: "audio",
				start_time: 0,
				end_time: 86_400_000,
				current_interval_status: 1,
				current_interval_remaining_percent: 50,
				remains_time: 3_600_000,
				current_weekly_status: 1,
				current_weekly_remaining_percent: 50,
				weekly_remains_time: 86_400_000,
			},
			{
				// An active (non-status-3) class with no window bounds at all: the
				// only way to actually exercise the ": 5" fallback branch, since
				// "video" (the only other bounds-less entry) is status 3 and
				// early-returns before intervalHours is ever computed.
				model_name: "voice",
				current_interval_status: 1,
				current_interval_remaining_percent: 60,
				remains_time: 1_800_000,
				current_weekly_status: 1,
				current_weekly_remaining_percent: 40,
				weekly_remains_time: 43_200_000,
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

	it("anchors reset instants to fetchedAt rather than the render clock", () => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2026-08-01T00:00:00Z"));
		try {
			render(
				<MiniMaxQuotaModal {...chrome} payload={payload} barMode="used" />,
			);
			// general: remains_time 1h after fetchedAt 2026-07-26T10:00Z. Anchoring
			// to Date.now() would print a date in August instead.
			const expected = formatAbsolute("2026-07-26T11:00:00Z");
			// The testid sits on the bar track; the reset sublabel is its sibling
			// inside the same QuotaBar root.
			expect(
				screen.getByTestId("minimax-general-5h-bar").parentElement,
			).toHaveTextContent(expected);
		} finally {
			vi.useRealTimers();
		}
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
		// "audio"'s window is 86,400,000 ms, i.e. 24 hours, which differs from
		// the hardcoded fallback (5). If the derivation above were deleted and
		// replaced with a literal 5, this assertion (not the "general" one
		// above, which can't tell 5-computed from 5-hardcoded) would fail.
		expect(screen.getByTestId("minimax-audio-5h-label")).toHaveTextContent(
			"24",
		);
	});

	it("falls back to a 5 hour interval when a live class reports no window bounds", () => {
		render(<MiniMaxQuotaModal {...chrome} payload={payload} barMode="used" />);
		// "voice" has no start_time/end_time and is NOT status 3, so it is the
		// only fixture entry that actually reaches the ": 5" fallback branch.
		expect(screen.getByTestId("minimax-voice-5h-label")).toHaveTextContent("5");
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
