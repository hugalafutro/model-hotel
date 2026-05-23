import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AppLogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { VirtualAppLogTable } from "../VirtualAppLogTable";

// Mock @tanstack/react-virtual - CRITICAL for JSDOM
const mockGetVirtualItems = vi.fn();
const mockGetTotalSize = vi.fn();
const mockMeasureElement = vi.fn();

vi.mock("@tanstack/react-virtual", () => ({
	useVirtualizer: vi.fn(() => ({
		getVirtualItems: mockGetVirtualItems,
		getTotalSize: mockGetTotalSize,
		measureElement: mockMeasureElement,
	})),
}));

// Helper to create mock AppLogEntry
function createAppLogEntry(overrides: Partial<AppLogEntry> = {}): AppLogEntry {
	return {
		id: "applog-1",
		timestamp: "2026-05-23T10:00:00Z",
		level: "info",
		source: "proxy",
		message: "Test log message",
		...overrides,
	};
}

// Default props
const defaultProps = {
	entries: [] as AppLogEntry[],
	total: 0,
	hasBefore: false,
	hasAfter: false,
	isLoadingBefore: false,
	isLoadingAfter: false,
	onFetchNewer: vi.fn(),
	onFetchOlder: vi.fn(),
	onRowClick: vi.fn(),
	sortDir: "desc" as const,
	onSortToggle: vi.fn(),
};

describe("VirtualAppLogTable", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.setItem("adminToken", "test-token");
	});

	// ==================== Empty State Tests ====================
	describe("Empty State", () => {
		it('renders "No log entries found" when entries is empty', () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(<VirtualAppLogTable {...defaultProps} />);

			expect(screen.getByText("No log entries found")).toBeInTheDocument();
		});

		it('renders "0 entries" in footer when entries is empty', () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(<VirtualAppLogTable {...defaultProps} />);

			expect(screen.getByText("0 entries")).toBeInTheDocument();
		});

		it("renders loading newer indicator when isLoadingBefore=true and entries empty", () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					isLoadingBefore={true}
					isLoadingAfter={false}
				/>,
			);

			expect(screen.getByText("↻ Loading newer…")).toBeInTheDocument();
		});

		it("renders loading older indicator when isLoadingAfter=true and entries empty", () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					isLoadingBefore={false}
					isLoadingAfter={true}
				/>,
			);

			expect(screen.getByText("↻ Loading older…")).toBeInTheDocument();
		});

		it("does not render loading indicators when not loading", () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					isLoadingBefore={false}
					isLoadingAfter={false}
				/>,
			);

			expect(screen.queryByText("↻ Loading newer…")).not.toBeInTheDocument();
			expect(screen.queryByText("↻ Loading older…")).not.toBeInTheDocument();
		});
	});

	// ==================== Populated State Rendering Tests ====================
	describe("Populated State Rendering", () => {
		const entries = [
			createAppLogEntry({ id: "log-1", level: "info" }),
			createAppLogEntry({ id: "log-2", level: "error" }),
		];

		beforeEach(() => {
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
				{ index: 1, key: "log-2", start: 48, end: 96 },
			]);
			mockGetTotalSize.mockReturnValue(entries.length * 48);
		});

		it("renders table headers (Time/Date with sort arrow, Level, Source, Message)", () => {
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={entries} total={2} />,
			);

			// Use regex to match header text with arrow
			expect(screen.getByText(/Time\/Date/)).toBeInTheDocument();
			expect(screen.getByText("Level")).toBeInTheDocument();
			expect(screen.getByText("Source")).toBeInTheDocument();
			expect(screen.getByText("Message")).toBeInTheDocument();
		});

		it("renders row data for each virtual item (level badge, source badge, message text)", () => {
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={entries} total={2} />,
			);

			// Level badges - use getAllBy since there could be multiple
			expect(screen.getAllByText("INFO")).toHaveLength(1);
			expect(screen.getAllByText("ERROR")).toHaveLength(1);

			// Source badges - use getAllBy since there are 2 entries with same source
			expect(screen.getAllByText("proxy")).toHaveLength(2);

			// Message text
			expect(screen.getAllByText("Test log message")).toHaveLength(2);
		});

		it("calls onRowClick with correct entry when row is clicked", async () => {
			const onRowClick = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={2}
					onRowClick={onRowClick}
				/>,
			);

			// Find the first row (with data-index="0")
			const row = container.querySelector('[data-index="0"]') as HTMLElement;
			expect(row).toBeInTheDocument();

			if (row) {
				await row.click();
				expect(onRowClick).toHaveBeenCalledWith(entries[0]);
			}
		});

		it("calls onSortToggle when Time/Date header is clicked", async () => {
			const onSortToggle = vi.fn();
			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={2}
					onSortToggle={onSortToggle}
				/>,
			);

			const header = screen.getByText(/Time\/Date/);
			await header.click();

			expect(onSortToggle).toHaveBeenCalledTimes(1);
		});

		it('renders "desc" arrow "↓" in header when sortDir="desc"', () => {
			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={2}
					sortDir="desc"
				/>,
			);

			expect(screen.getByText("Time/Date ↓")).toBeInTheDocument();
		});

		it('renders "asc" arrow "↑" in header when sortDir="asc"', () => {
			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={2}
					sortDir="asc"
				/>,
			);

			expect(screen.getByText("Time/Date ↑")).toBeInTheDocument();
		});

		it("renders '-' when source is empty", () => {
			const entriesWithEmptySource = [
				createAppLogEntry({ id: "log-1", source: "" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entriesWithEmptySource}
					total={1}
				/>,
			);

			// The dash is rendered as fallback for empty source
			expect(screen.getByText("-")).toBeInTheDocument();
		});

		it('renders level badge with correct variant for "error" level', () => {
			const errorEntry = createAppLogEntry({ id: "log-1", level: "error" });
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={[errorEntry]}
					total={1}
				/>,
			);

			// Badge with ERROR text and red color classes
			const badge = screen.getByText("ERROR");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-red-900/30");
			expect(badge.className).toContain("text-red-400");
		});

		it('renders level badge with correct variant for "warning" level', () => {
			const warningEntry = createAppLogEntry({
				id: "log-1",
				level: "warning",
			});
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={[warningEntry]}
					total={1}
				/>,
			);

			const badge = screen.getByText("WARNING");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-yellow-900/30");
			expect(badge.className).toContain("text-yellow-400");
		});

		it('renders level badge with correct variant for "info" level', () => {
			const infoEntry = createAppLogEntry({ id: "log-1", level: "info" });
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={[infoEntry]}
					total={1}
				/>,
			);

			const badge = screen.getByText("INFO");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-blue-900/30");
			expect(badge.className).toContain("text-blue-400");
		});
	});

	// ==================== Source Badge Classes Tests ====================
	describe("Source Badge Classes", () => {
		beforeEach(() => {
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);
		});

		it('renders source badge with correct classes for "auth" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "auth" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("auth");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-purple-900/30");
			expect(badge.className).toContain("text-purple-400");
		});

		it('renders source badge with correct classes for "proxy" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "proxy" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("proxy");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-cyan-900/30");
			expect(badge.className).toContain("text-cyan-400");
		});

		it('renders source badge with correct classes for "settings" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "settings" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("settings");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-indigo-900/30");
			expect(badge.className).toContain("text-indigo-400");
		});

		it("renders source badge with correct classes for unknown source (default)", () => {
			const entry = createAppLogEntry({
				id: "log-1",
				source: "unknown-source",
			});
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("unknown-source");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-gray-800/30");
			expect(badge.className).toContain("text-gray-400");
		});

		it('renders source badge with correct classes for "circuit-breaker" source', () => {
			const entry = createAppLogEntry({
				id: "log-1",
				source: "circuit-breaker",
			});
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("circuit-breaker");
			expect(badge).toBeInTheDocument();
			expect(badge.className).toContain("bg-orange-900/30");
			expect(badge.className).toContain("text-orange-400");
		});
	});

	// ==================== Pagination Footer Tests ====================
	describe("Pagination Footer", () => {
		const entries = [
			createAppLogEntry({ id: "log-1" }),
			createAppLogEntry({ id: "log-2" }),
			createAppLogEntry({ id: "log-3" }),
		];

		beforeEach(() => {
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
				{ index: 1, key: "log-2", start: 48, end: 96 },
				{ index: 2, key: "log-3", start: 96, end: 144 },
			]);
			mockGetTotalSize.mockReturnValue(entries.length * 48);
		});

		it('renders "X–Y / total" pagination in footer when entries exist', () => {
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={entries} total={100} />,
			);

			// Should show "1–3 / 100" (indices 0-2 + 1 = 1-3)
			expect(screen.getByText("1–3 / 100")).toBeInTheDocument();
		});

		it('renders "0 entries" when entries empty', () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(<VirtualAppLogTable {...defaultProps} />);

			expect(screen.getByText("0 entries")).toBeInTheDocument();
		});

		it("renders loading indicators in footer when populated and loading", () => {
			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={3}
					isLoadingBefore={true}
					isLoadingAfter={true}
				/>,
			);

			expect(screen.getByText("↻ Loading newer…")).toBeInTheDocument();
			expect(screen.getByText("↻ Loading older…")).toBeInTheDocument();
		});
	});

	// ==================== Scroll/Edge Threshold Tests ====================
	describe("Scroll/Edge Threshold", () => {
		const entries = [createAppLogEntry({ id: "log-1" })];

		beforeEach(() => {
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);
		});

		it("calls onFetchNewer when scrollTop < 500 and hasBefore=true and not loading", () => {
			const onFetchNewer = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasBefore={true}
					hasAfter={false}
					isLoadingBefore={false}
					isLoadingAfter={false}
					onFetchNewer={onFetchNewer}
				/>,
			);

			// Find the scroll container
			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			expect(scrollEl).toBeInTheDocument();

			if (scrollEl) {
				// Set scroll position near top (< 500)
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 100,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				// Fire scroll event
				fireEvent.scroll(scrollEl);

				expect(onFetchNewer).toHaveBeenCalledTimes(1);
			}
		});

		it("does NOT call onFetchNewer when isLoadingBefore=true", () => {
			const onFetchNewer = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasBefore={true}
					isLoadingBefore={true}
					onFetchNewer={onFetchNewer}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;

			if (scrollEl) {
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 100,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				fireEvent.scroll(scrollEl);

				expect(onFetchNewer).not.toHaveBeenCalled();
			}
		});

		it("does NOT call onFetchNewer when hasBefore=false", () => {
			const onFetchNewer = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasBefore={false}
					onFetchNewer={onFetchNewer}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;

			if (scrollEl) {
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 100,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				fireEvent.scroll(scrollEl);

				expect(onFetchNewer).not.toHaveBeenCalled();
			}
		});

		it("calls onFetchOlder when near bottom and hasAfter=true and not loading", () => {
			const onFetchOlder = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasBefore={false}
					hasAfter={true}
					isLoadingBefore={false}
					isLoadingAfter={false}
					onFetchOlder={onFetchOlder}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;

			if (scrollEl) {
				// Set scroll position near bottom
				// scrollHeight - scrollTop - clientHeight < 500
				// 2000 - 1600 - 500 = -100 < 500 (near bottom)
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 1600,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				fireEvent.scroll(scrollEl);

				expect(onFetchOlder).toHaveBeenCalledTimes(1);
			}
		});

		it("does NOT call onFetchOlder when isLoadingAfter=true", () => {
			const onFetchOlder = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasAfter={true}
					isLoadingAfter={true}
					onFetchOlder={onFetchOlder}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;

			if (scrollEl) {
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 1600,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				fireEvent.scroll(scrollEl);

				expect(onFetchOlder).not.toHaveBeenCalled();
			}
		});

		it("does NOT call onFetchOlder when hasAfter=false", () => {
			const onFetchOlder = vi.fn();
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasAfter={false}
					onFetchOlder={onFetchOlder}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;

			if (scrollEl) {
				Object.defineProperty(scrollEl, "scrollTop", {
					value: 1600,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "scrollHeight", {
					value: 2000,
					configurable: true,
				});
				Object.defineProperty(scrollEl, "clientHeight", {
					value: 500,
					configurable: true,
				});

				fireEvent.scroll(scrollEl);

				expect(onFetchOlder).not.toHaveBeenCalled();
			}
		});
	});

	// ==================== formatTimestamp Tests ====================
	describe("formatTimestamp", () => {
		beforeEach(() => {
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);
		});

		it("renders formatted timestamp for valid date", () => {
			const entry = createAppLogEntry({
				id: "log-1",
				timestamp: "2026-05-23T10:00:00Z",
			});
			const { container } = renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			// The timestamp cell should contain formatted date
			// formatTimestamp returns locale string like "05/23/2026, 10:00:00"
			const timestampCell = container.querySelector(
				'td[class*="whitespace-nowrap"]',
			);
			expect(timestampCell).toBeInTheDocument();
			// Should contain some formatted date content (not the raw ISO string)
			expect(timestampCell?.textContent).not.toBe("2026-05-23T10:00:00Z");
			expect(timestampCell?.textContent).toMatch(/\d{1,2}\/\d{1,2}\/\d{4}/);
		});

		it("renders 'Invalid Date' for unparseable date string (JS Date behavior)", () => {
			const entry = createAppLogEntry({
				id: "log-1",
				timestamp: "invalid-date-string",
			});
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			// When date parsing fails, new Date() returns Invalid Date
			// toLocaleString on Invalid Date returns "Invalid Date"
			// The catch block doesn't catch this because no exception is thrown
			expect(screen.getByText("Invalid Date")).toBeInTheDocument();
		});
	});
});
