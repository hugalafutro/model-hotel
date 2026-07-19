import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MiniMaxQuotaResponse } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { MiniMaxQuotaModal } from "../ProviderModals";

describe("MiniMaxQuotaModal", () => {
	// Live-captured reference payload (general active, video not in plan).
	const mockUsage: MiniMaxQuotaResponse = {
		model_remains: [
			{
				model_name: "general",
				start_time: 1784473200000,
				end_time: 1784491200000,
				remains_time: 16420081,
				weekly_start_time: 1783900800000,
				weekly_end_time: 1784505600000,
				weekly_remains_time: 30820081,
				current_interval_status: 1,
				current_interval_remaining_percent: 80,
				current_weekly_status: 1,
				current_weekly_remaining_percent: 50,
			},
			{
				model_name: "video",
				start_time: 1784419200000,
				end_time: 1784505600000,
				remains_time: 30820081,
				weekly_start_time: 1783900800000,
				weekly_end_time: 1784505600000,
				weekly_remains_time: 30820081,
				current_interval_status: 3,
				current_interval_remaining_percent: 100,
				current_weekly_status: 3,
				current_weekly_remaining_percent: 100,
			},
		],
		base_resp: { status_code: 0, status_msg: "success" },
	};

	const onClose = vi.fn();
	const onRefresh = vi.fn();
	const onToast = vi.fn();

	const defaultProps = {
		usage: mockUsage,
		onClose,
		onRefresh,
		isRefreshing: false,
		onToast,
		lastRefreshed: Date.now(),
	};

	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
	});

	describe("rendering", () => {
		it("renders modal title", () => {
			renderWithProviders(<MiniMaxQuotaModal {...defaultProps} />);
			expect(
				screen.getByRole("heading", { name: "MiniMax Plan Quota" }),
			).toBeInTheDocument();
		});

		it("renders 5h and weekly bars for the active general class", () => {
			renderWithProviders(<MiniMaxQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("minimax-general-5h-bar")).toBeInTheDocument();
			expect(
				screen.getByTestId("minimax-general-weekly-bar"),
			).toBeInTheDocument();
		});

		it("renders humanized class labels", () => {
			renderWithProviders(<MiniMaxQuotaModal {...defaultProps} />);
			expect(screen.getByText("Chat models")).toBeInTheDocument();
			expect(screen.getByText("Video models")).toBeInTheDocument();
		});

		it("renders a status-3 class as not-in-plan without bars", () => {
			renderWithProviders(<MiniMaxQuotaModal {...defaultProps} />);
			expect(screen.getByTestId("minimax-video-not-in-plan")).toHaveTextContent(
				"Not in plan",
			);
			expect(
				screen.queryByTestId("minimax-video-5h-bar"),
			).not.toBeInTheDocument();
			expect(
				screen.queryByTestId("minimax-video-weekly-bar"),
			).not.toBeInTheDocument();
		});

		it("renders reset countdowns derived from ms durations", () => {
			renderWithProviders(<MiniMaxQuotaModal {...defaultProps} />);
			// Two active bars => two "Resets ..." sublabels.
			expect(screen.getAllByText(/Resets/).length).toBeGreaterThanOrEqual(2);
		});

		it("hides last refreshed row when not provided", () => {
			renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("bar mode toggle", () => {
		it("toggles from remaining to used", async () => {
			const { user } = renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} />,
			);
			// general 5h: 80% remaining => 20% used.
			expect(screen.getByText("80% left")).toBeInTheDocument();
			await user.click(
				screen.getByRole("button", {
					name: "Toggle between remaining and used",
				}),
			);
			expect(screen.getByText("20% used")).toBeInTheDocument();
		});
	});

	describe("refresh functionality", () => {
		it("calls onRefresh when refresh button is clicked", async () => {
			const { user } = renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			expect(onRefresh).toHaveBeenCalledTimes(1);
		});

		it("calls onToast success after refresh", async () => {
			onRefresh.mockResolvedValue(undefined);
			const { user } = renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("Quota refreshed", "success");
			});
		});

		it("calls onToast error on refresh failure", async () => {
			onRefresh.mockRejectedValue(new Error("boom"));
			const { user } = renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Refresh" }));
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Failed to refresh quota",
					"error",
				);
			});
		});
	});

	describe("close functionality", () => {
		it("calls onClose when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<MiniMaxQuotaModal {...defaultProps} />,
			);
			await user.click(screen.getByRole("button", { name: "Close" }));
			await waitFor(() => {
				expect(onClose).toHaveBeenCalledTimes(1);
			});
		});
	});
});
