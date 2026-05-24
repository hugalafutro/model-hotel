import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaDataResult } from "../../hooks/useQuotaData";
import { renderWithProviders } from "../../test/utils";
import { ProviderQuotaPanel } from "../ProviderQuotaPanel";

// Mock useQuotaData
vi.mock("../../hooks/useQuotaData", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("../../hooks/useQuotaData")>();
	return {
		...actual,
		useQuotaData: vi.fn(),
	};
});

// Get the mocked function after vi.mock is set up
const { useQuotaData } = await import("../../hooks/useQuotaData");
const mockUseQuotaData = vi.mocked(useQuotaData);

function createMockQuotaData(
	overrides?: Partial<QuotaDataResult>,
): QuotaDataResult {
	return {
		hasAnyProvider: true,
		nanogptUsage: undefined,
		zaiCodingUsage: undefined,
		openrouterBalance: undefined,
		isNanoRefetching: false,
		isZaiCodingRefetching: false,
		isDsRefetching: false,
		isOrRefetching: false,
		isOllamaCloudRefetching: false,
		invalidateAll: vi.fn(),
		refetchNano: vi.fn(),
		refetchZaiCoding: vi.fn(),
		refetchDeepseek: vi.fn(),
		refetchOpenRouter: vi.fn(),
		refetchOllamaCloud: vi.fn(),
		nanogptDataUpdatedAt: 0,
		zaiCodingDataUpdatedAt: 0,
		openrouterDataUpdatedAt: 0,
		deepseekDataUpdatedAt: 0,
		ollamaCloudDataUpdatedAt: 0,
		showNanoBadge: false,
		showZaiCodingBadge: false,
		showDsBadge: false,
		showOrBadge: false,
		showOllamaCloudBadge: false,
		nanogptProviderId: undefined,
		zaiCodingProviderId: undefined,
		deepseekProviderId: undefined,
		openrouterProviderId: undefined,
		ollamaCloudProviderId: undefined,
		deepseekBalance: undefined,
		ollamaCloudAccount: undefined,
		zaiCodingFiveHour: undefined,
		zaiCodingWeekly: undefined,
		nanoWeeklyUsed: undefined,
		nanoWeeklyLimit: undefined,
		...overrides,
	};
}

function setupPanel(overrides?: Partial<QuotaDataResult>) {
	mockUseQuotaData.mockReturnValue(createMockQuotaData(overrides));
	return renderWithProviders(<ProviderQuotaPanel />);
}

describe("ProviderQuotaPanel", () => {
	const mockNanoUsage: import("../../api/types").NanoGPTUsage = {
		active: true,
		provider: "nanogpt",
		providerStatus: "active",
		providerStatusRaw: "active",
		stripeSubscriptionId: "sub_123",
		cancellationReason: null,
		canceledAt: null,
		endedAt: null,
		cancelAt: null,
		cancelAtPeriodEnd: false,
		limits: {
			weeklyInputTokens: 1000000,
			dailyInputTokens: 200000,
			dailyImages: 50,
		},
		allowOverage: false,
		period: { currentPeriodEnd: "2025-12-31" },
		dailyImages: {
			used: 10,
			remaining: 40,
			percentUsed: 20,
			resetAt: 1735689600000,
		},
		dailyInputTokens: {
			used: 50000,
			remaining: 150000,
			percentUsed: 25,
			resetAt: 1735689600000,
		},
		weeklyInputTokens: {
			used: 500000,
			remaining: 500000,
			percentUsed: 50,
			resetAt: 1735689600000,
		},
		state: "active",
		graceUntil: null,
	};

	const mockOpenRouterBalance: import("../../api/types").OpenRouterBalance = {
		label: "OR",
		limit: null,
		limit_reset: "",
		limit_remaining: null,
		usage: 0,
		usage_daily: 0,
		usage_weekly: 0,
		usage_monthly: 0,
		credits_total: 10,
		credits_used: 5,
		credits_remaining: 5.0,
		is_free_tier: false,
	};

	const mockZaiCodingUsage: import("../../api/types").ZAICodingQuotaResponse = {
		code: 0,
		msg: "success",
		success: true,
		data: {
			level: "basic",
			limits: [
				{
					type: "TOKENS_LIMIT",
					unit: 0,
					number: 0,
					usage: 50,
					currentValue: 100,
					remaining: 50,
					percentage: 50,
					nextResetTime: 1735689600000,
				},
			],
		},
	};

	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
		mockUseQuotaData.mockReturnValue(createMockQuotaData());
	});

	describe("rendering", () => {
		it("renders null when hasAnyProvider is false", () => {
			mockUseQuotaData.mockReturnValue(
				createMockQuotaData({ hasAnyProvider: false }),
			);
			const { container } = renderWithProviders(<ProviderQuotaPanel />);
			expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
		});

		it("renders null when quota is disabled", () => {
			localStorage.setItem("sidebarQuotaDisabled", "true");
			mockUseQuotaData.mockReturnValue(
				createMockQuotaData({ hasAnyProvider: true }),
			);
			const { container } = renderWithProviders(<ProviderQuotaPanel />);
			expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
		});

		it("renders panel when providers exist", () => {
			const { container } = setupPanel();
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();
		});
	});

	describe("collapsed state", () => {
		it("toggleCollapsed changes collapsed state and saves to localStorage", async () => {
			const user = userEvent.setup();
			const { getByTitle } = setupPanel();

			// Initially expanded - check for collapse button
			expect(getByTitle("Collapse")).toBeInTheDocument();

			// Click to collapse
			await user.click(getByTitle("Collapse"));

			expect(localStorage.getItem("sidebarQuotaCollapsed")).toBe("true");
		});

		it("toggleCollapsed shows toast when collapsing", async () => {
			const user = userEvent.setup();
			setupPanel();

			const collapseButton = screen.getByTitle("Collapse");
			await user.click(collapseButton);

			expect(
				screen.getByText("Quota panel collapsed - auto-refresh paused"),
			).toBeInTheDocument();
		});

		it("toggleCollapsed shows toast when expanding", async () => {
			const user = userEvent.setup();
			localStorage.setItem("sidebarQuotaCollapsed", "true");
			setupPanel();

			const expandButton = screen.getByTitle("Expand quotas");
			await user.click(expandButton);

			expect(
				screen.getByText("Quota panel expanded - auto-refresh resumed"),
			).toBeInTheDocument();
		});

		it("collapsed UI hides label and refresh button", () => {
			localStorage.setItem("sidebarQuotaCollapsed", "true");
			const { container, queryByTitle } = setupPanel();

			// Label should have invisible class
			const label = container.querySelector(".sidebar-quota-label");
			expect(label).toHaveClass("invisible");

			// Refresh button should be hidden when collapsed
			expect(queryByTitle("Refresh all quotas")).toBeNull();

			// Should show expand button
			expect(queryByTitle("Expand quotas")).toBeInTheDocument();
		});

		it("expanded UI shows label and refresh button", () => {
			const { container, queryByTitle } = setupPanel();

			// Label should be visible (no invisible class)
			const label = container.querySelector(".sidebar-quota-label");
			expect(label).not.toHaveClass("invisible");

			// Refresh button should be shown
			expect(queryByTitle("Refresh all quotas")).toBeInTheDocument();

			// Should show collapse button
			expect(queryByTitle("Collapse")).toBeInTheDocument();
		});
	});

	describe("refresh behavior", () => {
		it("handleRefresh calls invalidateAll and shows toast", async () => {
			const mockInvalidateAll = vi.fn();
			const user = userEvent.setup();
			setupPanel({ invalidateAll: mockInvalidateAll });

			const refreshButton = screen.getByTitle("Refresh all quotas");
			await user.click(refreshButton);

			expect(mockInvalidateAll).toHaveBeenCalledTimes(1);
			expect(screen.getByText("Refreshing quotas...")).toBeInTheDocument();
		});

		it("handleRefresh enforces cooldown on rapid clicks", async () => {
			const mockInvalidateAll = vi.fn();
			const user = userEvent.setup();
			setupPanel({ invalidateAll: mockInvalidateAll });

			const refreshButton = screen.getByTitle("Refresh all quotas");

			// First click succeeds
			await user.click(refreshButton);
			expect(mockInvalidateAll).toHaveBeenCalledTimes(1);

			// Immediate second click blocked by cooldown
			await user.click(refreshButton);
			expect(mockInvalidateAll).toHaveBeenCalledTimes(1);
			expect(
				screen.getByText("Please wait before refreshing again"),
			).toBeInTheDocument();
		});

		it("refresh button is disabled when anyRefreshing is true", () => {
			setupPanel({ isNanoRefetching: true });

			const refreshButton = screen.getByTitle(
				"Refresh all quotas",
			) as HTMLButtonElement;
			expect(refreshButton).toBeDisabled();
		});

		it("refresh icon spins when auto-refreshing", () => {
			const { container } = setupPanel({ isNanoRefetching: true });

			const refreshIcon = container.querySelector(".sidebar-quota-btn svg");
			expect(refreshIcon).toHaveClass("animate-spin");
		});

		it("refresh icon does not spin when not refreshing", () => {
			const { container } = setupPanel({ isNanoRefetching: false });

			const refreshIcon = container.querySelector(".sidebar-quota-btn svg");
			expect(refreshIcon).not.toHaveClass("animate-spin");
		});
	});

	describe("event listeners", () => {
		it("sidebarQuotaToggle event hides panel when disabled", async () => {
			const { container } = setupPanel();

			// Panel should be visible initially
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();

			// Set disabled in localStorage before dispatching event
			localStorage.setItem("sidebarQuotaDisabled", "true");

			// Dispatch toggle event
			window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));

			// Panel should be hidden when disabled
			await vi.waitFor(() => {
				expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
			});
		});

		it("sidebarQuotaRefreshChange event updates refresh interval", async () => {
			setupPanel();

			localStorage.setItem("sidebarQuotaRefreshMin", "10");
			window.dispatchEvent(new CustomEvent("sidebarQuotaRefreshChange"));

			// The handler calls setRefreshIntervalMin which triggers a
			// re-render with the updated refetchInterval passed to
			// useQuotaData. 10min = 600000ms.
			await vi.waitFor(() => {
				expect(mockUseQuotaData).toHaveBeenCalledWith(
					expect.anything(),
					expect.objectContaining({ refetchInterval: 600_000 }),
				);
			});
		});

		it("storage event (cross-tab) updates disabled state", async () => {
			const { container } = setupPanel();

			// Panel should be visible initially
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();

			// Set disabled in localStorage (simulates cross-tab change)
			localStorage.setItem("sidebarQuotaDisabled", "true");

			// Simulate cross-tab storage change.
			// The production handler re-reads localStorage directly and
			// does not inspect event.key or event.newValue, so those
			// fields are omitted to avoid implying key-based filtering.
			window.dispatchEvent(new StorageEvent("storage"));

			// Panel should be hidden when disabled via cross-tab
			await vi.waitFor(() => {
				expect(container.querySelector(".sidebar-quota-panel")).toBeNull();
			});
		});
	});

	describe("refreshMs calculation", () => {
		// Additional refetchInterval tests below verify the hook arguments directly;
		// these tests verify the component still renders correctly.
		it("renders panel when refreshIntervalMin is 0 (auto-refresh disabled)", () => {
			localStorage.setItem("sidebarQuotaRefreshMin", "0");
			const { container } = setupPanel();
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();
		});

		it("renders panel with valid refresh interval", () => {
			localStorage.setItem("sidebarQuotaRefreshMin", "10");
			const { container } = setupPanel();
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();
		});

		it("renders panel with invalid refresh interval (defaults to 5min)", () => {
			localStorage.setItem("sidebarQuotaRefreshMin", "invalid");
			const { container } = setupPanel();
			expect(
				container.querySelector(".sidebar-quota-panel"),
			).toBeInTheDocument();
		});
	});

	describe("modal rendering", () => {
		it("opens NanoGPTQuotaModal when clicking NanoGPT badge", async () => {
			const user = userEvent.setup();
			setupPanel({
				showNanoBadge: true,
				nanogptUsage: mockNanoUsage,
				nanoWeeklyUsed: 500000,
				nanoWeeklyLimit: 1000000,
			});

			const badge = screen.getByTitle(
				"NanoGPT weekly token quota - click for details",
			);
			await user.click(badge);

			expect(
				screen.getByRole("heading", { name: "NanoGPT Subscription" }),
			).toBeInTheDocument();
		});

		it("opens ZAICodingQuotaModal when clicking Z.ai Coding badge", async () => {
			const user = userEvent.setup();
			setupPanel({
				showZaiCodingBadge: true,
				zaiCodingUsage: mockZaiCodingUsage,
				zaiCodingFiveHour: mockZaiCodingUsage.data.limits[0],
				zaiCodingWeekly: mockZaiCodingUsage.data.limits[0],
			});

			const badge = screen.getByTitle(
				"Z.ai Coding Plan token quota - click for details",
			);
			await user.click(badge);

			expect(
				screen.getByRole("heading", { name: "Z.ai Coding Plan Quota" }),
			).toBeInTheDocument();
		});

		it("opens OpenRouterQuotaModal when clicking OpenRouter badge", async () => {
			const user = userEvent.setup();
			setupPanel({
				showOrBadge: true,
				openrouterBalance: mockOpenRouterBalance,
			});

			const badge = screen.getByTitle(
				"OpenRouter key balance - click for details",
			);
			await user.click(badge);

			expect(
				screen.getByRole("heading", { name: "OpenRouter Credits" }),
			).toBeInTheDocument();
		});
	});

	describe("modal close", () => {
		it("closes NanoGPTQuotaModal when clicking close button", async () => {
			const user = userEvent.setup();
			setupPanel({
				showNanoBadge: true,
				nanogptUsage: mockNanoUsage,
				nanoWeeklyUsed: 500000,
				nanoWeeklyLimit: 1000000,
			});

			const badge = screen.getByTitle(
				"NanoGPT weekly token quota - click for details",
			);
			await user.click(badge);

			// Wait for modal to appear
			await screen.findByRole("heading", { name: "NanoGPT Subscription" });

			// Click close button
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			// Modal should be closed
			expect(
				screen.queryByRole("heading", { name: "NanoGPT Subscription" }),
			).toBeNull();
		});

		it("closes ZAICodingQuotaModal when clicking close button", async () => {
			const user = userEvent.setup();
			setupPanel({
				showZaiCodingBadge: true,
				zaiCodingUsage: mockZaiCodingUsage,
				zaiCodingFiveHour: mockZaiCodingUsage.data.limits[0],
				zaiCodingWeekly: mockZaiCodingUsage.data.limits[0],
			});

			const badge = screen.getByTitle(
				"Z.ai Coding Plan token quota - click for details",
			);
			await user.click(badge);

			await screen.findByRole("heading", {
				name: "Z.ai Coding Plan Quota",
			});

			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			expect(
				screen.queryByRole("heading", {
					name: "Z.ai Coding Plan Quota",
				}),
			).toBeNull();
		});

		it("closes OpenRouterQuotaModal when clicking close button", async () => {
			const user = userEvent.setup();
			setupPanel({
				showOrBadge: true,
				openrouterBalance: mockOpenRouterBalance,
			});

			const badge = screen.getByTitle(
				"OpenRouter key balance - click for details",
			);
			await user.click(badge);

			await screen.findByRole("heading", {
				name: "OpenRouter Credits",
			});

			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			expect(
				screen.queryByRole("heading", {
					name: "OpenRouter Credits",
				}),
			).toBeNull();
		});
	});

	describe("collapsed + refreshing", () => {
		it("hides refresh button when collapsed even if refreshing", () => {
			localStorage.setItem("sidebarQuotaCollapsed", "true");
			const { container } = setupPanel({ isNanoRefetching: true });

			// Refresh button should be hidden when collapsed
			expect(container.querySelector(".sidebar-quota-btn")).toBeNull();
		});
	});

	describe("refetchInterval", () => {
		it("passes false when refreshIntervalMin is 0", async () => {
			localStorage.setItem("sidebarQuotaRefreshMin", "0");
			setupPanel();

			await vi.waitFor(() => {
				expect(mockUseQuotaData).toHaveBeenCalledWith(
					undefined,
					expect.objectContaining({ refetchInterval: false }),
				);
			});
		});

		it("passes 300000ms when refreshIntervalMin is invalid", async () => {
			localStorage.setItem("sidebarQuotaRefreshMin", "abc");
			setupPanel();

			await vi.waitFor(() => {
				expect(mockUseQuotaData).toHaveBeenCalledWith(
					undefined,
					expect.objectContaining({ refetchInterval: 300_000 }),
				);
			});
		});
	});
});
