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
});
