import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { VirtualLogTable } from "../VirtualLogTable";

// Mock @tanstack/react-virtual
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

// Helper to resolve column index from header text
function getColumnIndex(headerText: string): number {
	const headers = document.querySelectorAll("th");
	for (let i = 0; i < headers.length; i++) {
		if (headers[i].textContent?.includes(headerText)) return i;
	}
	throw new Error(`Column header "${headerText}" not found in table`);
}

// Helper to create mock LogEntry
function createLogEntry(overrides: Partial<LogEntry> = {}): LogEntry {
	return {
		id: "log-1",
		provider_id: "prov-1",
		provider_name: "Test Provider",
		model_id: "test-provider/model-v1",
		request_hash: "abc123def456",
		status_code: 200,
		latency_ms: 150,
		duration_ms: 200,
		ttft_ms: 50,
		response_header_ms: 50,
		proxy_overhead_ms: 5,
		parse_ms: 1,
		failover_lookup_ms: 0,
		model_lookup_ms: 1,
		provider_lookup_ms: 1,
		key_decrypt_ms: 2,
		dial_ms: 10,
		settings_read_ms: 1,
		tokens_per_second: 25.5,
		tokens_prompt: 100,
		tokens_completion: 50,
		tokens_prompt_cache_hit: 0,
		tokens_prompt_cache_miss: 100,
		tokens_completion_reasoning: 0,
		streaming: true,
		state: "completed",
		virtual_key_name: "test-key",
		virtual_key_id: "vk-1",
		error_message: "",
		failover_attempt: 0,
		created_at: "2026-05-23T10:00:00Z",
		resolved_model_id: "",
		endpoint_type: "chat",
		...overrides,
	};
}

// Default props
const defaultProps = {
	entries: [] as LogEntry[],
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

describe("VirtualLogTable", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.setItem("adminToken", "test-token");
		mockGetVirtualItems.mockReturnValue([]);
		mockGetTotalSize.mockReturnValue(0);
		mockMeasureElement.mockImplementation(() => {});
	});

	describe("Empty state", () => {
		it('renders "No logs found" when entries is empty', () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} />);
			expect(screen.getByText("No logs found")).toBeInTheDocument();
		});

		it('renders "0 entries" in footer when entries is empty', () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} />);
			expect(screen.getByText("0 entries")).toBeInTheDocument();
		});

		it("renders loading newer indicator when isLoadingBefore=true and entries empty", () => {
			renderWithProviders(
				<VirtualLogTable {...defaultProps} isLoadingBefore />,
			);
			expect(screen.getByText("Loading newer…")).toBeInTheDocument();
		});

		it("renders loading older indicator when isLoadingAfter=true and entries empty", () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} isLoadingAfter />);
			expect(screen.getByText("Loading older…")).toBeInTheDocument();
		});

		it("does not render loading indicators when not loading", () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} />);
			expect(screen.queryByText("Loading newer…")).not.toBeInTheDocument();
			expect(screen.queryByText("Loading older…")).not.toBeInTheDocument();
		});

		it("renders loading older indicator when isLoadingAfter=true and entries empty", () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} isLoadingAfter />);
			expect(screen.getByText("Loading older…")).toBeInTheDocument();
		});

		it("does not render loading indicators when not loading", () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} />);
			expect(screen.queryByText("↻ Loading newer…")).not.toBeInTheDocument();
			expect(screen.queryByText("↻ Loading older…")).not.toBeInTheDocument();
		});
	});

	describe("Populated state rendering", () => {
		it("renders table headers with sort direction (desc arrow ↓)", () => {
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} sortDir="desc" />,
			);
			expect(screen.getByText("Time/Date ↓")).toBeInTheDocument();
		});

		it("renders table headers with sort direction (asc arrow ↑)", () => {
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} sortDir="asc" />,
			);
			expect(screen.getByText("Time/Date ↑")).toBeInTheDocument();
		});

		it("renders row data for each virtual item", () => {
			const entries = [
				createLogEntry({
					id: "log-1",
					provider_name: "Provider A",
					model_id: "provider-a/model-1",
					status_code: 200,
				}),
				createLogEntry({
					id: "log-2",
					provider_name: "Provider B",
					model_id: "provider-b/model-2",
					status_code: 500,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-1", start: 0, end: 29 },
				{ index: 1, key: "log-2", start: 29, end: 58 },
			]);
			mockGetTotalSize.mockReturnValue(58);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Check provider names are rendered
			expect(screen.getByText("Provider A")).toBeInTheDocument();
			expect(screen.getByText("Provider B")).toBeInTheDocument();
			// Check status codes
			expect(screen.getByText("200")).toBeInTheDocument();
			expect(screen.getByText("500")).toBeInTheDocument();
		});

		it("calls onRowClick with correct entry when row is clicked", async () => {
			const onRowClick = vi.fn();
			const entries = [createLogEntry({ id: "log-123" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: "log-123", start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { user } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					onRowClick={onRowClick}
				/>,
			);

			// Find the row and click it
			const row = screen.getByText("Test Provider").closest("tr");
			if (row) {
				await user.click(row);
			}

			expect(onRowClick).toHaveBeenCalledWith(entries[0]);
		});

		it("calls onSortToggle when Time/Date header is clicked", async () => {
			const onSortToggle = vi.fn();
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { user } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					onSortToggle={onSortToggle}
				/>,
			);

			const header = screen.getByText("Time/Date ↓");
			await user.click(header);

			expect(onSortToggle).toHaveBeenCalledTimes(1);
		});

		it('renders "Deleted" (italic red) for deleted provider_name', () => {
			const entries = [createLogEntry({ provider_name: "Deleted" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const deletedSpan = screen.getByText("Deleted");
			expect(deletedSpan).toBeInTheDocument();
			expect(deletedSpan).toHaveClass("text-red-400", "italic");
		});

		it('renders "-" when provider_name is empty', () => {
			const entries = [createLogEntry({ provider_name: "" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// The cell should contain "-"
			const providerCell = screen.getByText("-");
			expect(providerCell).toBeInTheDocument();
		});

		it("renders status badge variant correctly for status 200 (success)", () => {
			const entries = [createLogEntry({ status_code: 200 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const badge = screen.getByText("200").closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-success");
		});

		it("renders status badge variant correctly for status 0 (error)", () => {
			const entries = [createLogEntry({ status_code: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const badge = screen.getByText("0").closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-error");
		});

		it("renders status badge variant correctly for status 429 (orange - 4xx)", () => {
			const entries = [createLogEntry({ status_code: 429 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const badge = screen.getByText("429").closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-orange");
		});

		it("renders status badge variant correctly for status 500 (error - 5xx)", () => {
			const entries = [createLogEntry({ status_code: 500 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const badge = screen.getByText("500").closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-error");
		});

		it("renders status badge variant correctly for status 100 (muted - 1xx)", () => {
			const entries = [createLogEntry({ status_code: 100 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const badge = screen.getByText("100").closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-neutral");
		});

		it('renders "Interrupted" for cancelled error messages in tokens column', () => {
			const entries = [
				createLogEntry({ error_message: "Request cancelled by user" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it('renders tokens as "prompt+completion" when > 0', () => {
			const entries = [
				createLogEntry({ tokens_prompt: 100, tokens_completion: 50 }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// The tokens column renders: {formatNumber(100)} + "+" + {formatNumber(50)}
			// Check that both numbers appear in the table's text content
			const tableText = screen.getByRole("table").textContent || "";
			expect(tableText).toContain("100");
			expect(tableText).toContain("50");
		});

		it('renders "-" when tokens are 0', () => {
			const entries = [
				createLogEntry({ tokens_prompt: 0, tokens_completion: 0 }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Find the tokens column - it should have "-"
			// The tokens column is the 6th column
			const tokensCell = screen.getAllByText("-").find((el) => {
				const parent = el.closest("td");
				return parent !== null;
			});
			expect(tokensCell).toBeDefined();
		});

		it("renders model_id with provider prefix stripped", () => {
			const entries = [createLogEntry({ model_id: "openai/gpt-4" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show "gpt-4" not "openai/gpt-4"
			expect(screen.getByText("gpt-4")).toBeInTheDocument();
		});

		it("renders hotel/ model_id as-is", () => {
			const entries = [createLogEntry({ model_id: "hotel/my-failover-group" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show full "hotel/my-failover-group"
			expect(screen.getByText("hotel/my-failover-group")).toBeInTheDocument();
		});

		it('renders "Deleted" (italic red) for virtual_key_deleted=true', () => {
			const entries = [
				createLogEntry({
					virtual_key_deleted: true,
					virtual_key_name: "old-key",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const deletedSpan = screen.getByText("Deleted");
			expect(deletedSpan).toBeInTheDocument();
			expect(deletedSpan).toHaveClass("text-red-400", "italic");
		});

		it("does NOT set title attribute on key column when virtual_key_deleted=true", () => {
			const entries = [
				createLogEntry({
					virtual_key_deleted: true,
					virtual_key_name: "old-key",
					virtual_key_id: "vk-deleted",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Find the key column cell (last column in the row)
			const row = screen.getByText("Deleted").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				// Key column is the last column
				const keyCell = cells[cells.length - 1];
				expect(keyCell).not.toHaveAttribute("title");
			}
		});

		it('renders "internal" (italic) for internal virtual keys', () => {
			const entries = [
				createLogEntry({ virtual_key_name: "internal", virtual_key_id: "" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const internalSpan = screen.getByText("internal");
			expect(internalSpan).toBeInTheDocument();
			expect(internalSpan).toHaveClass("text-gray-400", "italic");
		});

		it("renders virtual_key_name when present", () => {
			const entries = [createLogEntry({ virtual_key_name: "my-api-key" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("my-api-key")).toBeInTheDocument();
		});

		it('renders "-" when no virtual key info', () => {
			const entries = [
				createLogEntry({ virtual_key_name: "", virtual_key_id: "" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Find a "-" in the key column (last column)
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Pagination footer", () => {
		it('renders "X–Y / total" pagination in footer', () => {
			const entries = Array.from({ length: 10 }, (_, i) =>
				createLogEntry({ id: `log-${i}` }),
			);
			mockGetVirtualItems.mockReturnValue(
				entries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 29,
					end: (i + 1) * 29,
				})),
			);
			mockGetTotalSize.mockReturnValue(290);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} total={100} />,
			);

			// Should show "1–10 / 100"
			expect(screen.getByText("1–10 / 100")).toBeInTheDocument();
		});

		it('renders "0 entries" when entries empty', () => {
			renderWithProviders(<VirtualLogTable {...defaultProps} />);
			expect(screen.getByText("0 entries")).toBeInTheDocument();
		});

		it("renders loading indicators in footer when populated", () => {
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					isLoadingBefore
					isLoadingAfter
				/>,
			);

			expect(screen.getByText("Loading newer…")).toBeInTheDocument();
			expect(screen.getByText("Loading older…")).toBeInTheDocument();
		});
	});

	describe("Scroll/edge threshold", () => {
		it("calls onFetchNewer when scrollTop < 500 and hasBefore=true and not loading", () => {
			const onFetchNewer = vi.fn();
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					hasBefore
					onFetchNewer={onFetchNewer}
				/>,
			);

			// Find the scroll container
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

			expect(onFetchNewer).toHaveBeenCalledTimes(1);
		});

		it("does NOT call onFetchNewer when isLoadingBefore=true", () => {
			const onFetchNewer = vi.fn();
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					hasBefore
					isLoadingBefore
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
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
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
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					hasAfter
					onFetchOlder={onFetchOlder}
				/>,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");
			// Near bottom: scrollHeight - scrollTop - clientHeight < 500
			// 2000 - 1600 - 500 = -100 < 500
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
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
					hasAfter
					isLoadingAfter
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
			const entries = [createLogEntry()];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { container } = renderWithProviders(
				<VirtualLogTable
					{...defaultProps}
					entries={entries}
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
	});

	describe("Duration formatting", () => {
		it("renders duration in ms when < 1000", () => {
			const entries = [createLogEntry({ duration_ms: 500 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("500ms")).toBeInTheDocument();
		});

		it("renders duration in seconds when >= 1000", () => {
			const entries = [createLogEntry({ duration_ms: 1500 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("1.5s")).toBeInTheDocument();
		});

		it('renders "-" when duration_ms is 0', () => {
			const entries = [createLogEntry({ duration_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Find "-" in the duration column
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Proxy overhead", () => {
		it("renders overhead with accent color when > 0", () => {
			const entries = [createLogEntry({ proxy_overhead_ms: 10 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show "10.00ms" with accent color
			const overheadCell = screen.getByText("10.00ms");
			expect(overheadCell).toBeInTheDocument();
			expect(overheadCell).toHaveClass("text-(--accent)");
		});

		it('renders "-" when overhead is 0 or null', () => {
			const entries = [createLogEntry({ proxy_overhead_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show "-" in the overhead column
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("TTFT formatting", () => {
		it("renders TTFT value when ttft_ms > 0", () => {
			const entries = [createLogEntry({ ttft_ms: 120.5 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// formatMs(120.5, 1) = "120.5ms"
			expect(screen.getByText("120.5ms")).toBeInTheDocument();
		});

		it('renders "-" when ttft_ms is 0', () => {
			const entries = [createLogEntry({ ttft_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// TTFT column should show "-"
			const row = screen.getByText("abc123def456").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const ttftIndex = getColumnIndex("TTFT");
				expect(cells[ttftIndex].textContent).toBe("-");
			}
		});
	});

	describe("Headers formatting", () => {
		it("renders Headers value when response_header_ms > 0", () => {
			const entries = [createLogEntry({ response_header_ms: 150.5 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("abc123def456").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const headersIndex = getColumnIndex("Headers");
				expect(cells[headersIndex].textContent).toBe("150.5ms");
			}
		});

		it('renders "-" when response_header_ms is 0', () => {
			const entries = [createLogEntry({ response_header_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("abc123def456").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const headersIndex = getColumnIndex("Headers");
				expect(cells[headersIndex].textContent).toBe("-");
			}
		});
	});

	describe("Cancelled request formatting", () => {
		it('renders "-" for TPS on cancelled requests', () => {
			const entries = [
				createLogEntry({
					error_message: "request cancelled",
					tokens_per_second: 25.5,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// TPS column should show "-", not 25.5
			const row = screen.getByText("Interrupted").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const tpsIndex = getColumnIndex("T/s");
				expect(cells[tpsIndex].textContent).toBe("-");
			}
		});

		it('renders "-" for TTFT on cancelled requests', () => {
			const entries = [
				createLogEntry({
					error_message: "request cancelled",
					ttft_ms: 120.5,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("Interrupted").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const ttftIndex = getColumnIndex("TTFT");
				expect(cells[ttftIndex].textContent).toBe("-");
			}
		});

		it('renders "-" for Headers on cancelled requests', () => {
			const entries = [
				createLogEntry({
					error_message: "request cancelled",
					response_header_ms: 150.5,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("Interrupted").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const headersIndex = getColumnIndex("Headers");
				expect(cells[headersIndex].textContent).toBe("-");
			}
		});
	});

	describe("TPS cache-hit tinting", () => {
		it("applies tertiary text color when tokens_prompt_cache_hit > 0", () => {
			const entries = [
				createLogEntry({
					tokens_per_second: 42.5,
					tokens_prompt_cache_hit: 500,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("42.5").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const tpsIndex = getColumnIndex("T/s");
				const tpsSpan = cells[tpsIndex].querySelector("span");
				expect(tpsSpan).not.toBeNull();
				expect(tpsSpan?.className).toContain("opacity-50");
			}
		});

		it("shows inflated by prompt cache hits tooltip when tokens_prompt_cache_hit > 0", () => {
			const entries = [
				createLogEntry({
					tokens_per_second: 42.5,
					tokens_prompt_cache_hit: 500,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("42.5").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const tpsIndex = getColumnIndex("T/s");
				const tpsSpan = cells[tpsIndex].querySelector("span");
				expect(tpsSpan?.getAttribute("title")).toBe(
					"Inflated by prompt cache hits",
				);
			}
		});

		it("uses default gray color when tokens_prompt_cache_hit is 0", () => {
			const entries = [
				createLogEntry({
					tokens_per_second: 42.5,
					tokens_prompt_cache_hit: 0,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("42.5").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				const tpsIndex = getColumnIndex("T/s");
				const tpsSpan = cells[tpsIndex].querySelector("span");
				expect(tpsSpan).not.toBeNull();
				expect(tpsSpan?.className).toBe("");
				expect(tpsSpan?.getAttribute("title")).toBeNull();
			}
		});
	});

	describe("Model ID formatting", () => {
		it("renders model_id as-is when it contains no slash", () => {
			const entries = [createLogEntry({ model_id: "gpt-4o-mini" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("gpt-4o-mini")).toBeInTheDocument();
		});

		it("renders hotel/ model_id with resolved_model_id showing both in cell and tooltip", () => {
			const entries = [
				createLogEntry({
					model_id: "hotel/my-failover-group",
					resolved_model_id: "openai/gpt-4o",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show the group name in accent color
			expect(screen.getByText("hotel/my-failover-group")).toBeInTheDocument();
			// Should show resolved model in parentheses
			expect(screen.getByText("(openai/gpt-4o)")).toBeInTheDocument();

			// Check tooltip title includes both
			const modelCell = screen
				.getByText("hotel/my-failover-group")
				.closest("td");
			expect(modelCell).toHaveAttribute(
				"title",
				"hotel/my-failover-group (openai/gpt-4o)",
			);
		});

		it("renders non-hotel/ model_id with resolved_model_id not showing resolved in tooltip", () => {
			const entries = [
				createLogEntry({
					model_id: "anthropic/claude-3-5-sonnet",
					resolved_model_id: "anthropic/claude-3-5-sonnet",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show stripped model name (without prefix)
			expect(screen.getByText("claude-3-5-sonnet")).toBeInTheDocument();

			// Check tooltip title shows full model_id (not just stripped name)
			const modelCell = screen.getByText("claude-3-5-sonnet").closest("td");
			expect(modelCell).toHaveAttribute("title", "anthropic/claude-3-5-sonnet");
		});
	});

	describe("created_at formatting", () => {
		it('renders "-" when created_at is empty', () => {
			const entries = [createLogEntry({ created_at: "" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// The time column should show "-"
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Virtual key case sensitivity", () => {
		it('renders "internal" (italic) for case-insensitive Internal virtual key', () => {
			const entries = [
				createLogEntry({ virtual_key_name: "Internal", virtual_key_id: "" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const internalSpan = screen.getByText("internal");
			expect(internalSpan).toBeInTheDocument();
			expect(internalSpan).toHaveClass("text-gray-400", "italic");
		});
	});

	describe("Virtual key fallback", () => {
		it("renders virtual_key_id when name is empty", () => {
			const entries = [
				createLogEntry({ virtual_key_name: "", virtual_key_id: "vk-abc123" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("vk-abc123")).toBeInTheDocument();
		});

		it("sets title to virtual_key_id when virtual_key_name is empty and key is not deleted", () => {
			const entries = [
				createLogEntry({
					virtual_key_deleted: false,
					virtual_key_name: "",
					virtual_key_id: "vk-abc123",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("vk-abc123")).toBeInTheDocument();

			// Find the key column cell (last column in the row)
			const row = screen.getByText("vk-abc123").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				// Key column is the last column
				const keyCell = cells[cells.length - 1];
				expect(keyCell).toHaveAttribute("title", "vk-abc123");
			}
		});

		it("does NOT set title attribute when virtual_key_name and virtual_key_id are both empty", () => {
			const entries = [
				createLogEntry({
					virtual_key_deleted: false,
					virtual_key_name: "",
					virtual_key_id: "",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// The cell should show "-" as fallback
			expect(screen.getByText("-")).toBeInTheDocument();

			// Find the key column cell (last column in the row)
			const row = screen.getByText("-").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = row.querySelectorAll("td");
				// Key column is the last column
				const keyCell = cells[cells.length - 1];
				expect(keyCell).not.toHaveAttribute("title");
			}
		});
	});

	describe("Proxy overhead null", () => {
		it('renders "-" when proxy_overhead_ms is null', () => {
			const entries = [
				createLogEntry({ proxy_overhead_ms: null as unknown as number }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Overhead column should show "-"
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Cancel error detection", () => {
		it('detects "disconnect" as cancelled error', () => {
			const entries = [createLogEntry({ error_message: "Client disconnect" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it('detects "client disconnected" as cancelled error', () => {
			const entries = [
				createLogEntry({ error_message: "client disconnected" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it('detects "timed out" as cancelled error', () => {
			const entries = [
				createLogEntry({
					error_message: "all providers failed: upstream request timed out",
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});
	});

	describe("Request hash formatting", () => {
		it('renders "-" when request_hash is empty', () => {
			const entries = [createLogEntry({ request_hash: "" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Hash column should show "-"
			expect(screen.getAllByText("-").length).toBeGreaterThanOrEqual(1);
		});
	});
});
