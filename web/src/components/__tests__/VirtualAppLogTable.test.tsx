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

			expect(screen.getByText("Loading newer…")).toBeInTheDocument();
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

			expect(screen.getByText("Loading older…")).toBeInTheDocument();
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

			expect(screen.queryByText("Loading newer…")).not.toBeInTheDocument();
			expect(screen.queryByText("Loading older…")).not.toBeInTheDocument();
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
			// Note: getByText finds the inner <span class="badge-text">, so we use
			// closest('[data-test-variant]') to find the outer Badge span
			const badge = screen.getByText("ERROR").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-red-900/30");
			expect(badge?.className).toContain("text-red-400");
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

			const badge = screen.getByText("WARNING").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-yellow-900/30");
			expect(badge?.className).toContain("text-yellow-400");
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

			const badge = screen.getByText("INFO").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-blue-900/30");
			expect(badge?.className).toContain("text-blue-400");
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

			const badge = screen.getByText("auth").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-purple-900/30");
			expect(badge?.className).toContain("text-purple-400");
		});

		it('renders source badge with correct classes for "proxy" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "proxy" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("proxy").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-cyan-900/30");
			expect(badge?.className).toContain("text-cyan-400");
		});

		it('renders source badge with correct classes for "settings" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "settings" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("settings").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-indigo-900/30");
			expect(badge?.className).toContain("text-indigo-400");
		});

		it("renders source badge with correct classes for unknown source (default)", () => {
			const entry = createAppLogEntry({
				id: "log-1",
				source: "unknown-source",
			});
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("unknown-source")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-gray-800/30");
			expect(badge?.className).toContain("text-gray-400");
		});

		it('renders source badge with correct classes for "circuit-breaker" source', () => {
			const entry = createAppLogEntry({
				id: "log-1",
				source: "circuit-breaker",
			});
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("circuit-breaker")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-orange-900/30");
			expect(badge?.className).toContain("text-orange-400");
		});

		// Additional source badge tests for untested sources
		it('renders source badge with correct classes for "resolve" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "resolve" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("resolve").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-teal-900/30");
			expect(badge?.className).toContain("text-teal-400");
		});

		it('renders source badge with correct classes for "discovery" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "discovery" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("discovery")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-emerald-900/30");
			expect(badge?.className).toContain("text-emerald-400");
		});

		it('renders source badge with correct classes for "failover" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "failover" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("failover").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-slate-700/50");
			expect(badge?.className).toContain("text-slate-300");
		});

		it('renders source badge with correct classes for "ratelimit" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "ratelimit" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("ratelimit")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-amber-900/30");
			expect(badge?.className).toContain("text-amber-400");
		});

		it('renders source badge with correct classes for "vkey" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "vkey" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("vkey").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-pink-900/30");
			expect(badge?.className).toContain("text-pink-400");
		});

		it('renders source badge with correct classes for "admin" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "admin" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("admin").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-pink-900/30");
			expect(badge?.className).toContain("text-pink-400");
		});

		it('renders source badge with correct classes for "events" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "events" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("events").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-violet-900/30");
			expect(badge?.className).toContain("text-violet-400");
		});

		it('renders source badge with correct classes for "docker" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "docker" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("docker").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-sky-900/30");
			expect(badge?.className).toContain("text-sky-400");
		});

		// Lime group: keycache, model, provider, cache, db
		it('renders source badge with correct classes for "keycache" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "keycache" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("keycache").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-lime-900/30");
			expect(badge?.className).toContain("text-lime-400");
		});

		it('renders source badge with correct classes for "model" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "model" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("model").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-lime-900/30");
			expect(badge?.className).toContain("text-lime-400");
		});

		it('renders source badge with correct classes for "provider" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "provider" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("provider").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-lime-900/30");
			expect(badge?.className).toContain("text-lime-400");
		});

		it('renders source badge with correct classes for "cache" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "cache" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("cache").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-lime-900/30");
			expect(badge?.className).toContain("text-lime-400");
		});

		it('renders source badge with correct classes for "db" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "db" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("db").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-lime-900/30");
			expect(badge?.className).toContain("text-lime-400");
		});

		it('renders source badge with correct classes for "access" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "access" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("access").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-fuchsia-900/30");
			expect(badge?.className).toContain("text-fuchsia-400");
		});

		// Blue group: server, startup, retention
		it('renders source badge with correct classes for "server" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "server" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("server").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-blue-900/30");
			expect(badge?.className).toContain("text-blue-400");
		});

		it('renders source badge with correct classes for "startup" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "startup" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("startup").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-blue-900/30");
			expect(badge?.className).toContain("text-blue-400");
		});

		it('renders source badge with correct classes for "retention" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "retention" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("retention")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-blue-900/30");
			expect(badge?.className).toContain("text-blue-400");
		});

		it('renders source badge with correct classes for "modelsdev" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "modelsdev" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen
				.getByText("modelsdev")
				.closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-rose-900/30");
			expect(badge?.className).toContain("text-rose-400");
		});

		it('renders source badge with correct classes for "applogs" source', () => {
			const entry = createAppLogEntry({ id: "log-1", source: "applogs" });
			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			const badge = screen.getByText("applogs").closest("[data-test-variant]");
			expect(badge).toBeInTheDocument();
			expect(badge?.className).toContain("bg-gray-700/30");
			expect(badge?.className).toContain("text-gray-400");
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

			expect(screen.getByText("Loading newer…")).toBeInTheDocument();
			expect(screen.getByText("Loading older…")).toBeInTheDocument();
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
			if (!scrollEl) throw new Error("Scroll container not found");
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
		});

		it("does not trigger fetches when scroll position is far from edges", () => {
			const onFetchNewer = vi.fn();
			const onFetchOlder = vi.fn();

			const entries = [createAppLogEntry({ id: "log-1" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			// Render with hasBefore/hasAfter true
			const { container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={entries}
					total={1}
					hasBefore={true}
					hasAfter={true}
					onFetchNewer={onFetchNewer}
					onFetchOlder={onFetchOlder}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");

			// Set scroll position far from edges (> 500px from both top and bottom)
			// so handleScroll's early return (!el) is the only guard being tested
			Object.defineProperty(scrollEl, "scrollTop", {
				value: 1000,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 2000,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				writable: true,
				configurable: true,
			});

			// Fire scroll event - should not crash
			expect(() => fireEvent.scroll(scrollEl)).not.toThrow();

			// Should not trigger fetches when scroll position is not near edges
			expect(onFetchNewer).not.toHaveBeenCalled();
			expect(onFetchOlder).not.toHaveBeenCalled();
		});
	});

	// ==================== useLayoutEffect Prepend Compensation Tests ====================
	describe("useLayoutEffect Prepend Compensation", () => {
		it("adjusts scroll position when entries are prepended and scrollTop > 1", () => {
			const initialEntries = [
				createAppLogEntry({ id: "log-2", message: "Second" }),
				createAppLogEntry({ id: "log-3", message: "Third" }),
			];
			const prependedEntries = [
				createAppLogEntry({ id: "log-1", message: "First" }),
				...initialEntries,
			];

			// Mock virtual items for initial state
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-2", start: 0, end: 48 },
				{ index: 1, key: "log-3", start: 48, end: 96 },
			]);
			mockGetTotalSize.mockReturnValue(96);

			const { rerender, container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={initialEntries}
					total={2}
				/>,
			);

			// Get scroll element and set initial scroll position > 1
			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");

			// Use fireEvent to trigger scroll with proper property descriptors
			Object.defineProperty(scrollEl, "scrollTop", {
				value: 50,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 2000,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 500,
				writable: true,
				configurable: true,
			});

			// Update mock for prepended state with larger total size
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
				{ index: 1, key: "log-2", start: 48, end: 96 },
				{ index: 2, key: "log-3", start: 96, end: 144 },
			]);
			mockGetTotalSize.mockReturnValue(144);

			// Rerender with prepended entries (more items at the start)
			rerender(
				<VirtualAppLogTable
					{...defaultProps}
					entries={prependedEntries}
					total={3}
				/>,
			);

			// The scroll position should be adjusted (scrollTop increased)
			// to compensate for the prepended item
			// After prepend: scrollTop should be 50 + (144 - 96) = 98
			expect(scrollEl.scrollTop).toBeGreaterThan(50);
		});

		it("does not adjust scroll position when scrollTop is at top (<= 1)", () => {
			const initialEntries = [
				createAppLogEntry({ id: "log-2", message: "Second" }),
			];
			const prependedEntries = [
				createAppLogEntry({ id: "log-1", message: "First" }),
				...initialEntries,
			];

			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-2", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			const { rerender, container } = renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={initialEntries}
					total={1}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");

			Object.defineProperty(scrollEl, "scrollTop", {
				value: 0,
				writable: true,
				configurable: true,
			});

			// Update mock for prepended state
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
				{ index: 1, key: "log-2", start: 48, end: 96 },
			]);
			mockGetTotalSize.mockReturnValue(96);

			rerender(
				<VirtualAppLogTable
					{...defaultProps}
					entries={prependedEntries}
					total={2}
				/>,
			);

			// Scroll position should remain at top (not adjusted)
			expect(scrollEl.scrollTop).toBe(0);
		});
	});

	// ==================== Footer Range Display Edge Cases ====================
	describe("Footer Range Display Edge Cases", () => {
		it('displays "1-20 of 100" for first page showing 20 entries of 100 total', () => {
			const entries = Array.from({ length: 20 }, (_, i) =>
				createAppLogEntry({ id: `log-${i + 1}`, message: `Message ${i + 1}` }),
			);

			mockGetVirtualItems.mockReturnValue(
				entries.map((_, i) => ({
					index: i,
					key: `log-${i + 1}`,
					start: i * 48,
					end: (i + 1) * 48,
				})),
			);
			mockGetTotalSize.mockReturnValue(entries.length * 48);

			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={entries} total={100} />,
			);

			expect(screen.getByText("1–20 / 100")).toBeInTheDocument();
		});

		it('displays "1-1 of 1" for single entry', () => {
			const entries = [createAppLogEntry({ id: "log-1", message: "Single" })];

			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 48 },
			]);
			mockGetTotalSize.mockReturnValue(48);

			renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={entries} total={1} />,
			);

			expect(screen.getByText("1–1 / 1")).toBeInTheDocument();
		});

		it('displays "0 entries" when entries array is empty', () => {
			mockGetVirtualItems.mockReturnValue([]);
			mockGetTotalSize.mockReturnValue(0);

			renderWithProviders(<VirtualAppLogTable {...defaultProps} />);

			expect(screen.getByText("0 entries")).toBeInTheDocument();
		});

		it("displays correct range for middle page (e.g., 21-40 of 100)", () => {
			// Create 40 entries total, but only render virtual items for 21-40
			const allEntries = Array.from({ length: 40 }, (_, i) =>
				createAppLogEntry({
					id: `log-${i + 1}`,
					message: `Message ${i + 1}`,
				}),
			);

			// Simulate virtual items showing only indices 20-39 (items 21-40)
			mockGetVirtualItems.mockReturnValue(
				Array.from({ length: 20 }, (_, i) => ({
					index: i + 20,
					key: `log-${i + 21}`,
					start: i * 48,
					end: (i + 1) * 48,
				})),
			);
			mockGetTotalSize.mockReturnValue(100 * 48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={allEntries}
					total={100}
				/>,
			);

			expect(screen.getByText("21–40 / 100")).toBeInTheDocument();
		});

		it("displays correct range for last page with partial items", () => {
			// Create 100 entries total
			const allEntries = Array.from({ length: 100 }, (_, i) =>
				createAppLogEntry({
					id: `log-${i + 1}`,
					message: `Message ${i + 1}`,
				}),
			);

			// Simulate virtual items for last 5 entries (indices 95-99)
			mockGetVirtualItems.mockReturnValue(
				Array.from({ length: 5 }, (_, i) => ({
					index: i + 95,
					key: `log-${i + 96}`,
					start: i * 48,
					end: (i + 1) * 48,
				})),
			);
			mockGetTotalSize.mockReturnValue(100 * 48);

			renderWithProviders(
				<VirtualAppLogTable
					{...defaultProps}
					entries={allEntries}
					total={100}
				/>,
			);

			expect(screen.getByText("96–100 / 100")).toBeInTheDocument();
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
			// Should contain a formatted date, not the raw ISO string.
			// Avoid locale-sensitive regex (different locales use dots, dashes, etc.)
			expect(timestampCell?.textContent).not.toBe("2026-05-23T10:00:00Z");
			expect(timestampCell?.textContent).toBeTruthy();
		});

		it("renders a fallback when date is unparseable", () => {
			const entry = createAppLogEntry({
				id: "log-1",
				timestamp: "invalid-date-string",
			});
			const { container } = renderWithProviders(
				<VirtualAppLogTable {...defaultProps} entries={[entry]} total={1} />,
			);

			// new Date("invalid-date-string") creates an Invalid Date object.
			// Depending on the V8 version, toLocaleString with options either:
			// - returns "Invalid Date" (older V8 / some JSDOM setups)
			// - throws RangeError, caught by the catch block which returns the raw string
			const cell = container.querySelector('td[class*="whitespace-nowrap"]');
			expect(cell).toBeInTheDocument();
			const text = cell?.textContent ?? "";
			expect(text === "Invalid Date" || text === "invalid-date-string").toBe(
				true,
			);
		});
	});
});
