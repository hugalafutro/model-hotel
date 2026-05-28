import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import {
	NanoGPTQuotaModal,
	OpenRouterQuotaModal,
	ZAICodingQuotaModal,
} from "../ProviderModals";

describe("NanoGPTQuotaModal", () => {
	const mockUsage = {
		active: true,
		provider: "nanogpt",
		providerStatus: "active",
		providerStatusRaw: "active",
		stripeSubscriptionId: "sub_test123",
		cancellationReason: null,
		canceledAt: null,
		endedAt: null,
		cancelAt: null,
		cancelAtPeriodEnd: false,
		limits: {
			weeklyInputTokens: 1000000,
			dailyInputTokens: 200000,
			dailyImages: 100,
		},
		allowOverage: false,
		period: {
			currentPeriodEnd: new Date(
				Date.now() + 7 * 24 * 60 * 60 * 1000,
			).toISOString(),
		},
		dailyImages: {
			used: 10,
			remaining: 90,
			percentUsed: 10,
			resetAt: Date.now() + 24 * 60 * 60 * 1000,
		},
		dailyInputTokens: {
			used: 50000,
			remaining: 150000,
			percentUsed: 25,
			resetAt: Date.now() + 24 * 60 * 60 * 1000,
		},
		weeklyInputTokens: {
			used: 200000,
			remaining: 800000,
			percentUsed: 20,
			resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
		},
		state: "active" as const,
		graceUntil: null,
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
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const heading = screen.getByRole("heading", {
				name: "NanoGPT Subscription",
			});
			expect(heading).toBeInTheDocument();
		});

		it("renders active status indicator", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Active")).toBeInTheDocument();
		});

		it("renders green status dot for active subscription", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const statusDot = screen.getByTestId("status-dot-active");
			expect(statusDot).toHaveClass("bg-green-400");
		});

		it("renders inactive status when subscription is not active", () => {
			const inactiveUsage = { ...mockUsage, active: false };
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={inactiveUsage} />,
			);
			expect(screen.getByText("Inactive")).toBeInTheDocument();
		});

		it("renders red status dot for inactive subscription", () => {
			const inactiveUsage = { ...mockUsage, active: false };
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={inactiveUsage} />,
			);
			const statusDot = screen.getByTestId("status-dot-inactive");
			expect(statusDot).toHaveClass("bg-red-400");
		});

		it("renders weekly token quota section", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Weekly Token Quota")).toBeInTheDocument();
		});

		it("renders weekly token usage numbers", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			// Mock data: weeklyInputTokens used=200000, limit=1000000 → "200K / 1M"
			expect(screen.getByText("200K / 1M")).toBeInTheDocument();
		});

		it("renders progress bar for weekly quota", () => {
			const { container } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			expect(container.querySelector("[style*=width]")).toBeInTheDocument();
		});

		it("renders daily images section", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Daily Images")).toBeInTheDocument();
		});

		it("renders daily images usage", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			// Mock data: dailyImages used=10, limit=100 → "10 / 100"
			expect(screen.getByText("10 / 100")).toBeInTheDocument();
		});

		it("renders daily input tokens section", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Daily Input Tokens")).toBeInTheDocument();
		});

		it("renders daily input tokens usage", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			// Mock data: dailyInputTokens used=50000, limit=200000 → "50K / 200K"
			expect(screen.getByText("50K / 200K")).toBeInTheDocument();
		});

		it("renders subscription details section", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Subscription Details")).toBeInTheDocument();
		});

		it("renders provider name", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("nanogpt")).toBeInTheDocument();
		});

		it("renders provider status", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("active")).toBeInTheDocument();
		});

		it("renders period end date", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			// Should show a formatted date
			expect(screen.getByText(/Period End/)).toBeInTheDocument();
		});

		it("renders allow overage setting", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Allow Overage")).toBeInTheDocument();
			expect(screen.getByText("No")).toBeInTheDocument();
		});

		it("renders refresh button", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const refreshButton = screen.getByRole("button", {
				name: "Refresh",
			});
			expect(refreshButton).toBeInTheDocument();
		});

		it("renders last refreshed timestamp", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Last refreshed")).toBeInTheDocument();
		});
	});

	describe("subscription status", () => {
		it("shows active status with green dot", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			expect(screen.getByText("Active")).toBeInTheDocument();
		});

		it("shows inactive status with red dot", () => {
			const inactiveUsage = { ...mockUsage, active: false };
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={inactiveUsage} />,
			);
			expect(screen.getByText("Inactive")).toBeInTheDocument();
		});

		it("shows cancellation warning when cancelAtPeriodEnd is true", () => {
			const cancellingUsage = { ...mockUsage, cancelAtPeriodEnd: true };
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={cancellingUsage} />,
			);
			expect(
				screen.getByText(/Subscription will cancel at period end/),
			).toBeInTheDocument();
		});
	});

	describe("refresh functionality", () => {
		it("calls onRefresh when refresh button is clicked", async () => {
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			expect(onRefresh).toHaveBeenCalledTimes(1);
		});

		it("calls onToast with success message after refresh", async () => {
			onRefresh.mockResolvedValue(undefined);
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("Quota refreshed", "success");
			});
		});

		it("calls onToast with error message on refresh failure", async () => {
			onRefresh.mockRejectedValue(new Error("Refresh failed"));
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Failed to refresh quota",
					"error",
				);
			});
		});

		it("shows spinning icon while refreshing", () => {
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} isRefreshing={true} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeDisabled();
			const hasSpinner = screen.queryByTestId("spinner");
			const hasSpinningIcon = refreshButton.querySelector(".animate-spin");
			expect(hasSpinner || hasSpinningIcon).toBeTruthy();
		});

		it("disables refresh button while refreshing", () => {
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} isRefreshing={true} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeDisabled();
		});
	});

	describe("progress bar colors", () => {
		it("uses red color when remaining percentage is below 20%", () => {
			const lowQuotaUsage = {
				...mockUsage,
				limits: {
					...mockUsage.limits,
					weeklyInputTokens: 1000000,
				},
				weeklyInputTokens: {
					...mockUsage.weeklyInputTokens,
					used: 900000,
				},
			};
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={lowQuotaUsage} />,
			);
			const progressBarFill = screen.getByTestId("weekly-progress-fill");
			expect(progressBarFill).toHaveClass("bg-red-500");
		});

		it("uses amber color when remaining percentage is between 20% and 60%", () => {
			const mediumQuotaUsage = {
				...mockUsage,
				limits: {
					...mockUsage.limits,
					weeklyInputTokens: 1000000,
				},
				weeklyInputTokens: {
					...mockUsage.weeklyInputTokens,
					used: 500000,
				},
			};
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={mediumQuotaUsage} />,
			);
			const progressBarFill = screen.getByTestId("weekly-progress-fill");
			expect(progressBarFill).toHaveClass("bg-amber-500");
		});

		it("uses indigo color when remaining percentage is above 60%", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const progressBarFill = screen.getByTestId("weekly-progress-fill");
			expect(progressBarFill).toHaveClass("bg-[#6366F1]");
		});
	});

	describe("close functionality", () => {
		it("calls onClose when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);
			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when backdrop is clicked", async () => {
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	describe("quota display edge cases", () => {
		it("shows 'Yes' for allow overage when allowOverage is true", () => {
			const usageWithOverage = { ...mockUsage, allowOverage: true };
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={usageWithOverage} />,
			);
			expect(screen.getByText("Yes")).toBeInTheDocument();
		});

		it("shows 'No limit set' when weeklyLimit is 0", () => {
			const usageWithNoLimit = {
				...mockUsage,
				limits: { ...mockUsage.limits, weeklyInputTokens: 0 },
			};
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={usageWithNoLimit} />,
			);
			expect(
				screen.getByText((text) => text.includes("No limit set")),
			).toBeInTheDocument();
		});

		it("shows 'N/A' for daily images reset when resetAt is undefined", () => {
			const usageWithNoReset = {
				...mockUsage,
				dailyImages: {
					...mockUsage.dailyImages,
					resetAt: undefined,
				},
			};
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={usageWithNoReset} />,
			);
			// Find the paragraph containing "N/A" in the Daily Images section
			const dailyHeading = screen.getByText("Daily Images");
			const dailySection = dailyHeading.parentElement?.parentElement;
			expect(dailySection?.textContent).toContain("N/A");
		});

		it("shows 'N/A' for daily input tokens reset when resetAt is undefined", () => {
			const usageWithNoReset = {
				...mockUsage,
				dailyInputTokens: {
					...mockUsage.dailyInputTokens,
					resetAt: undefined,
				},
			};
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} usage={usageWithNoReset} />,
			);
			// Find the paragraph containing "N/A" in the Daily Input Tokens section
			const dailyHeading = screen.getByText("Daily Input Tokens");
			const dailySection = dailyHeading.parentElement?.parentElement;
			expect(dailySection?.textContent).toContain("N/A");
		});

		it("does not render last refreshed section when lastRefreshed is undefined", () => {
			renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("bar toggle", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("renders toggle button", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("defaults to remaining mode", () => {
			renderWithProviders(<NanoGPTQuotaModal {...defaultProps} />);
			const progressBarFill = screen.getByTestId("weekly-progress-fill");
			expect(progressBarFill).toHaveClass("bg-[#6366F1]");
			expect(progressBarFill).not.toHaveClass("bg-red-500");
			expect(progressBarFill).not.toHaveClass("bg-amber-500");
			expect(progressBarFill).not.toHaveClass("bg-orange-500");
		});

		it("switches to used mode on toggle click", async () => {
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			await user.click(toggleButton);
			const progressBarFill = screen.getByTestId("weekly-progress-fill");
			// In used mode with 20% used (100 - 80% remaining), bar should be amber (usedPct < 50)
			expect(progressBarFill).toHaveClass("bg-amber-500");
		});

		it("has different bar width after toggle", async () => {
			const { user } = renderWithProviders(
				<NanoGPTQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			let progressBarFill = screen.getByTestId("weekly-progress-fill");
			const initialWidth = progressBarFill.getAttribute("style");

			await user.click(toggleButton);
			progressBarFill = screen.getByTestId("weekly-progress-fill");
			const afterToggleWidth = progressBarFill.getAttribute("style");

			// Width should change (80% -> 20% or vice versa)
			expect(initialWidth).not.toEqual(afterToggleWidth);
		});
	});
});

describe("ZAICodingQuotaModal", () => {
	const mockUsage = {
		code: 0,
		msg: "success",
		success: true,
		data: {
			level: "basic",
			limits: [
				{
					type: "TOKENS_LIMIT" as const,
					unit: 3,
					number: 10000,
					usage: 5000,
					currentValue: 5000,
					remaining: 5000,
					percentage: 50,
					nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
				},
				{
					type: "TOKENS_LIMIT" as const,
					unit: 6,
					number: 50000,
					usage: 25000,
					currentValue: 25000,
					remaining: 25000,
					percentage: 50,
					nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
				},
				{
					type: "TIME_LIMIT" as const,
					unit: 5,
					number: 100,
					usage: 30,
					currentValue: 30,
					remaining: 70,
					percentage: 30,
					nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
					usageDetails: [
						{ modelCode: "model-1", usage: 10 },
						{ modelCode: "model-2", usage: 20 },
					],
				},
			],
		},
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
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			const heading = screen.getByRole("heading", {
				name: "Z.ai Coding Plan Quota",
			});
			expect(heading).toBeInTheDocument();
		});

		it("renders plan level", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("basic")).toBeInTheDocument();
		});

		it("renders 5h token quota section", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("5h Token Quota")).toBeInTheDocument();
		});

		it("renders weekly token quota section", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("Weekly Token Quota")).toBeInTheDocument();
		});

		it("renders MCP time quota section", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("MCP Time Quota")).toBeInTheDocument();
		});

		it("renders MCP usage details", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("model-1")).toBeInTheDocument();
			expect(screen.getByText("model-2")).toBeInTheDocument();
		});

		it("renders refresh button", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeInTheDocument();
		});

		it("renders last refreshed timestamp", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("Last refreshed")).toBeInTheDocument();
		});
	});

	describe("refresh functionality", () => {
		it("calls onRefresh when refresh button is clicked", async () => {
			const { user } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			expect(onRefresh).toHaveBeenCalledTimes(1);
		});

		it("calls onToast with success message after refresh", async () => {
			onRefresh.mockResolvedValue(undefined);
			const { user } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("Quota refreshed", "success");
			});
		});

		it("calls onToast with error message on refresh failure", async () => {
			onRefresh.mockRejectedValue(new Error("Refresh failed"));
			const { user } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Failed to refresh quota",
					"error",
				);
			});
		});

		it("shows spinning icon while refreshing", () => {
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} isRefreshing={true} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeDisabled();
			// Default theme renders RefreshCw with animate-spin class
			expect(refreshButton.querySelector(".animate-spin")).toBeTruthy();
		});
	});

	describe("close functionality", () => {
		it("calls onClose when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);
			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when backdrop is clicked", async () => {
			const { user } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	describe("edge cases", () => {
		it("shows '-' when plan level is undefined", () => {
			const usageWithNoLevel = {
				...mockUsage,
				data: { ...mockUsage.data, level: undefined },
			};
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithNoLevel} />,
			);
			expect(screen.getByText("-")).toBeInTheDocument();
		});

		it("does not render quota sections when limits array is empty", () => {
			const usageWithNoLimits = {
				...mockUsage,
				data: { ...mockUsage.data, limits: [] },
			};
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithNoLimits} />,
			);
			expect(screen.queryByText("5h Token Quota")).not.toBeInTheDocument();
			expect(screen.queryByText("Weekly Token Quota")).not.toBeInTheDocument();
			expect(screen.queryByText("MCP Time Quota")).not.toBeInTheDocument();
		});

		it("renders only present limit sections when some are missing", () => {
			const usageWithPartialLimits = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TOKENS_LIMIT" as const,
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
					],
				},
			};
			renderWithProviders(
				<ZAICodingQuotaModal
					{...defaultProps}
					usage={usageWithPartialLimits}
				/>,
			);
			expect(screen.getByText("5h Token Quota")).toBeInTheDocument();
			expect(screen.queryByText("Weekly Token Quota")).not.toBeInTheDocument();
			expect(screen.queryByText("MCP Time Quota")).not.toBeInTheDocument();
		});

		it("renders lastRefreshed timestamp when provided", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			expect(screen.getByText("Last refreshed")).toBeInTheDocument();
		});

		it("does not render lastRefreshed section when undefined", () => {
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("progress bar colors", () => {
		it("uses red color when remaining percentage is below 20%", () => {
			const usageWithLowQuota = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TOKENS_LIMIT" as const,
							unit: 3,
							number: 10000,
							usage: 8500,
							currentValue: 8500,
							remaining: 1500,
							percentage: 85,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
					],
				},
			};
			const { container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithLowQuota} />,
			);
			const progressBar = container.querySelector(
				".bg-red-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("uses amber color when remaining percentage is between 20% and 60%", () => {
			const usageWithMediumQuota = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TOKENS_LIMIT" as const,
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
					],
				},
			};
			const { container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithMediumQuota} />,
			);
			const progressBar = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("uses indigo color when remaining percentage is above 60%", () => {
			const usageWithHighQuota = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TOKENS_LIMIT" as const,
							unit: 3,
							number: 10000,
							usage: 2000,
							currentValue: 2000,
							remaining: 8000,
							percentage: 20,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
					],
				},
			};
			const { container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithHighQuota} />,
			);
			const progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});
	});

	describe("MCP quota edge cases", () => {
		it("does not show usage detail rows when usageDetails is undefined", () => {
			const usageWithNoDetails = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TIME_LIMIT" as const,
							unit: 5,
							number: 100,
							usage: 30,
							currentValue: 30,
							remaining: 70,
							percentage: 30,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
							usageDetails: undefined,
						},
					],
				},
			};
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithNoDetails} />,
			);
			expect(screen.queryByText("model-1")).not.toBeInTheDocument();
			expect(screen.queryByText("model-2")).not.toBeInTheDocument();
		});

		it("does not show usage detail rows when usageDetails is empty array", () => {
			const usageWithEmptyDetails = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TIME_LIMIT" as const,
							unit: 5,
							number: 100,
							usage: 30,
							currentValue: 30,
							remaining: 70,
							percentage: 30,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
							usageDetails: [],
						},
					],
				},
			};
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithEmptyDetails} />,
			);
			expect(screen.queryByText("model-1")).not.toBeInTheDocument();
			expect(screen.queryByText("model-2")).not.toBeInTheDocument();
		});

		it("shows 'N/A' for MCP reset time when nextResetTime is undefined", () => {
			const usageWithNoReset = {
				...mockUsage,
				data: {
					...mockUsage.data,
					limits: [
						{
							type: "TOKENS_LIMIT" as const,
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
						{
							type: "TOKENS_LIMIT" as const,
							unit: 6,
							number: 50000,
							usage: 25000,
							currentValue: 25000,
							remaining: 25000,
							percentage: 50,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
						{
							type: "TIME_LIMIT" as const,
							unit: 5,
							number: 100,
							usage: 30,
							currentValue: 30,
							remaining: 70,
							percentage: 30,
							nextResetTime: undefined,
						},
					],
				},
			};
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} usage={usageWithNoReset} />,
			);
			// Find the paragraph containing "N/A" after the MCP section heading
			const mcpHeading = screen.getByText("MCP Time Quota");
			const mcpSection = mcpHeading.parentElement?.parentElement;
			expect(mcpSection?.textContent).toContain("N/A");
		});

		it("does not render last refreshed section when lastRefreshed is undefined", () => {
			renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("bar toggle", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("renders toggle button", () => {
			renderWithProviders(<ZAICodingQuotaModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("defaults to remaining mode", () => {
			const { container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			// 5h quota: 50% remaining (100 - 50% used) → >60% remaining → indigo
			const progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("switches to used mode on toggle click", async () => {
			const { user, container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			await user.click(toggleButton);
			// In used mode with 50% used: usedPct >= 50 && < 80 → orange
			const progressBar = container.querySelector(
				".bg-orange-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("toggles bar colors between remaining and used modes", async () => {
			const { user, container } = renderWithProviders(
				<ZAICodingQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});

			// Initial state: remaining mode (indigo for 50% remaining)
			let progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();

			// Toggle to used mode
			await user.click(toggleButton);
			progressBar = container.querySelector(".bg-amber-500.h-3.rounded-full");
			expect(progressBar).toBeInTheDocument();

			// Toggle back to remaining mode
			await user.click(toggleButton);
			progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});
	});
});

describe("OpenRouterQuotaModal", () => {
	const mockBalance = {
		label: "OpenRouter",
		limit: null,
		limit_reset: "",
		limit_remaining: null,
		usage: 100000,
		usage_daily: 10000,
		usage_weekly: 50000,
		usage_monthly: 100000,
		credits_total: 1000000,
		credits_used: 100000,
		credits_remaining: 900000,
		is_free_tier: false,
	};

	const onClose = vi.fn();
	const onRefresh = vi.fn();
	const onToast = vi.fn();

	const defaultProps = {
		balance: mockBalance,
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
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			const heading = screen.getByRole("heading", {
				name: "OpenRouter Credits",
			});
			expect(heading).toBeInTheDocument();
		});

		it("renders credits remaining", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			// OpenRouter shows "Account Balance" section with credits remaining
			expect(screen.getByText("Account Balance")).toBeInTheDocument();
			expect(screen.getByText("$900,000.00")).toBeInTheDocument();
		});

		it("renders credits used", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			// OpenRouter shows "spent total" text with credits used
			expect(screen.getByText("$100,000.00 spent total")).toBeInTheDocument();
		});

		it("renders tier status", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			// OpenRouter modal shows "Free Tier" or "Paid Account" based on is_free_tier
			// With is_free_tier: false, it should show "Paid Account"
			expect(screen.getByText("Paid Account")).toBeInTheDocument();
		});

		it("renders refresh button", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeInTheDocument();
		});
	});

	describe("free tier display", () => {
		it("shows Free Tier status when is_free_tier is true", () => {
			const freeTierBalance = { ...mockBalance, is_free_tier: true };
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} balance={freeTierBalance} />,
			);
			expect(screen.getByText("Free Tier")).toBeInTheDocument();
		});

		it("shows Paid Account status when is_free_tier is false", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			expect(screen.getByText("Paid Account")).toBeInTheDocument();
		});
	});

	describe("refresh functionality", () => {
		it("calls onRefresh when refresh button is clicked", async () => {
			const { user } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			expect(onRefresh).toHaveBeenCalledTimes(1);
		});

		it("calls onToast with success message after refresh", async () => {
			onRefresh.mockResolvedValue(undefined);
			const { user } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith("Balance refreshed", "success");
			});
		});

		it("calls onToast with error message on refresh failure", async () => {
			onRefresh.mockRejectedValue(new Error("Refresh failed"));
			const { user } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			await user.click(refreshButton);
			await waitFor(() => {
				expect(onToast).toHaveBeenCalledWith(
					"Failed to refresh balance",
					"error",
				);
			});
		});

		it("shows spinning icon while refreshing", () => {
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} isRefreshing />,
			);
			const refreshButton = screen.getByRole("button", { name: "Refresh" });
			expect(refreshButton).toBeDisabled();
			expect(refreshButton.querySelector(".animate-spin")).toBeTruthy();
		});
	});

	describe("close functionality", () => {
		it("calls onClose when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);
			expect(onClose).toHaveBeenCalledTimes(1);
		});

		it("calls onClose when backdrop is clicked", async () => {
			const { user } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const backdrop = screen.getByRole("button", { name: "Close dialog" });
			await user.click(backdrop);
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	describe("spending limit section", () => {
		it("renders Key Spending Limit section when limit is set", () => {
			const balanceWithLimit = {
				...mockBalance,
				limit: 10,
				limit_remaining: 5,
				limit_reset: new Date(Date.now() + 86400 * 1000).toISOString(),
			};
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} balance={balanceWithLimit} />,
			);
			expect(screen.getByText("Key Spending Limit")).toBeInTheDocument();
		});

		it("shows remaining percentage and progress bar when limit > 0", () => {
			const balanceWithLimit = {
				...mockBalance,
				limit: 10,
				limit_remaining: 5,
				limit_reset: new Date(Date.now() + 86400 * 1000).toISOString(),
			};
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} balance={balanceWithLimit} />,
			);
			// Text includes reset timestamp: "50.0% remaining · Resets ..."
			expect(screen.getByText(/50\.0% remaining/)).toBeInTheDocument();
			expect(screen.getByText("$5.00 remaining")).toBeInTheDocument();
		});

		it("shows $0 limit - spending blocked when limit === 0", () => {
			const balanceWithZeroLimit = {
				...mockBalance,
				limit: 0,
				limit_remaining: 0,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithZeroLimit}
				/>,
			);
			expect(
				screen.getByText("$0 limit - spending blocked"),
			).toBeInTheDocument();
		});

		it("shows No limit set when limit < 0", () => {
			const balanceWithNegativeLimit = {
				...mockBalance,
				limit: -1,
				limit_remaining: -1,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNegativeLimit}
				/>,
			);
			expect(screen.getByText("No limit set")).toBeInTheDocument();
		});

		it("shows reset timestamp when limit_reset is provided", () => {
			const balanceWithReset = {
				...mockBalance,
				limit: 10,
				limit_remaining: 5,
				limit_reset: new Date(Date.now() + 86400 * 1000).toISOString(),
			};
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} balance={balanceWithReset} />,
			);
			expect(screen.getByText(/Resets/)).toBeInTheDocument();
		});

		it("uses red color for progress bar when remaining percentage is below 20%", () => {
			const balanceWithLowRemaining = {
				...mockBalance,
				limit: 10,
				limit_remaining: 1,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithLowRemaining}
				/>,
			);
			const progressBar = container.querySelector(
				".bg-red-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("uses amber color for progress bar when remaining percentage is between 20% and 60%", () => {
			const balanceWithMediumRemaining = {
				...mockBalance,
				limit: 10,
				limit_remaining: 4,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithMediumRemaining}
				/>,
			);
			const progressBar = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("uses indigo color for progress bar when remaining percentage is above 60%", () => {
			const balanceWithHighRemaining = {
				...mockBalance,
				limit: 10,
				limit_remaining: 8,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithHighRemaining}
				/>,
			);
			const progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("uses amber bar color when limit is 0 (fallback)", () => {
			const balanceWithZeroLimit = {
				...mockBalance,
				limit: 0,
				limit_remaining: 0,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithZeroLimit}
				/>,
			);
			// When limit is 0, the bar should use amber fallback color
			const progressBar = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
			// Width should be 0% (not -Infinity or NaN)
			expect(progressBar?.getAttribute("style")).toContain("width: 0%");
		});

		it("uses amber bar color when limit is negative (no limit set)", () => {
			const balanceWithNegativeLimit = {
				...mockBalance,
				limit: -1,
				limit_remaining: -1,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNegativeLimit}
				/>,
			);
			const progressBar = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
			expect(progressBar?.getAttribute("style")).toContain("width: 0%");
		});
	});

	describe("credits display", () => {
		it("shows 'No credits' when credits_total is 0", () => {
			const balanceWithNoCredits = {
				...mockBalance,
				credits_total: 0,
				credits_used: 0,
				credits_remaining: 0,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNoCredits}
				/>,
			);
			expect(screen.getByText("No credits")).toBeInTheDocument();
		});

		it("does not render progress bar when credits_total is 0", () => {
			const balanceWithNoCredits = {
				...mockBalance,
				credits_total: 0,
				credits_used: 0,
				credits_remaining: 0,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNoCredits}
				/>,
			);
			// The account balance progress bar should not be rendered
			const accountBalanceSection =
				screen.getByText("Account Balance").parentElement?.parentElement;
			const progressBars =
				accountBalanceSection?.querySelectorAll('[style*="width"]');
			expect(progressBars).toHaveLength(0);
		});

		it("renders progress bar when credits_total > 0", () => {
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			// Find the progress bar div in the Account Balance section
			const progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});
	});

	describe("key usage section", () => {
		it("renders daily usage with formatDollars", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			expect(screen.getByText("$10,000.00")).toBeInTheDocument();
		});

		it("renders weekly usage with formatDollars", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			expect(screen.getByText("$50,000.00")).toBeInTheDocument();
		});

		it("renders monthly usage with formatDollars", () => {
			const balanceWithDistinctMonthly = {
				...mockBalance,
				usage_monthly: 75000,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithDistinctMonthly}
				/>,
			);
			expect(screen.getByText("$75,000.00")).toBeInTheDocument();
		});

		it("renders all-time usage with formatDollars", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			// usage and usage_monthly are both 100000, so there are two "$100,000.00" elements
			const allTimeValues = screen.getAllByText("$100,000.00");
			expect(allTimeValues.length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("lastRefreshed", () => {
		it("renders last refreshed timestamp when provided", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			expect(screen.getByText("Last refreshed")).toBeInTheDocument();
		});

		it("does not render last refreshed section when undefined", () => {
			renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} lastRefreshed={undefined} />,
			);
			expect(screen.queryByText("Last refreshed")).not.toBeInTheDocument();
		});
	});

	describe("progress bar colors for account balance", () => {
		it("uses red color when remaining percentage is below 20%", () => {
			const balanceWithLowCredits = {
				...mockBalance,
				credits_total: 1000000,
				credits_remaining: 100000,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithLowCredits}
				/>,
			);
			// Find the progress bar div with bg-red-500 class in the Account Balance section
			const accountBalanceSection = container.querySelector(
				".bg-red-500.h-3.rounded-full",
			);
			expect(accountBalanceSection).toBeInTheDocument();
		});

		it("uses amber color when remaining percentage is between 20% and 60%", () => {
			const balanceWithMediumCredits = {
				...mockBalance,
				credits_total: 1000000,
				credits_remaining: 400000,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithMediumCredits}
				/>,
			);
			const accountBalanceSection = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(accountBalanceSection).toBeInTheDocument();
		});

		it("uses indigo color when remaining percentage is above 60%", () => {
			const balanceWithHighCredits = {
				...mockBalance,
				credits_total: 1000000,
				credits_remaining: 800000,
			};
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithHighCredits}
				/>,
			);
			const accountBalanceSection = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(accountBalanceSection).toBeInTheDocument();
		});
	});

	describe("limit_reset and limit_remaining edge cases", () => {
		it("does not show reset text when limit_reset is empty string", () => {
			const balanceWithEmptyReset = {
				...mockBalance,
				limit: 10,
				limit_remaining: 5,
				limit_reset: "",
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithEmptyReset}
				/>,
			);
			const limitSection =
				screen.getByText("Key Spending Limit").parentElement?.parentElement;
			expect(limitSection?.textContent).not.toContain("Resets");
		});

		it("defaults to 0 when limit_remaining is null", () => {
			const balanceWithNullRemaining = {
				...mockBalance,
				limit: 10,
				limit_remaining: null,
				limit_reset: new Date(Date.now() + 86400 * 1000).toISOString(),
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNullRemaining}
				/>,
			);
			expect(screen.getByText("$0.00 remaining")).toBeInTheDocument();
		});

		it("shows 'No credits' and no progress bar when credits_total is 0", () => {
			const balanceWithNoCredits = {
				...mockBalance,
				credits_total: 0,
				credits_used: 0,
				credits_remaining: 0,
			};
			renderWithProviders(
				<OpenRouterQuotaModal
					{...defaultProps}
					balance={balanceWithNoCredits}
				/>,
			);
			expect(screen.getByText("No credits")).toBeInTheDocument();
			const accountBalanceSection =
				screen.getByText("Account Balance").parentElement?.parentElement;
			const progressBars =
				accountBalanceSection?.querySelectorAll('[style*="width"]');
			expect(progressBars).toHaveLength(0);
		});
	});

	describe("bar toggle", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("renders toggle button", () => {
			renderWithProviders(<OpenRouterQuotaModal {...defaultProps} />);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("defaults to remaining mode", () => {
			const { container } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			// credits_remaining: 900000 / credits_total: 1000000 = 90% remaining → >60% → indigo
			const progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("switches to used mode on toggle click", async () => {
			const { user, container } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});
			await user.click(toggleButton);
			// In used mode with 10% used (100 - 90% remaining), bar should be amber (usedPct < 50)
			const progressBar = container.querySelector(
				".bg-amber-500.h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});

		it("toggles bar colors between remaining and used modes", async () => {
			const { user, container } = renderWithProviders(
				<OpenRouterQuotaModal {...defaultProps} />,
			);
			const toggleButton = screen.getByRole("button", {
				name: "Toggle between remaining and used",
			});

			// Initial state: remaining mode (indigo for 90% remaining)
			let progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();

			// Toggle to used mode
			await user.click(toggleButton);
			progressBar = container.querySelector(".bg-amber-500.h-3.rounded-full");
			expect(progressBar).toBeInTheDocument();

			// Toggle back to remaining mode
			await user.click(toggleButton);
			progressBar = container.querySelector(
				".bg-\\[\\#6366F1\\].h-3.rounded-full",
			);
			expect(progressBar).toBeInTheDocument();
		});
	});
});
