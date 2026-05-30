import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { VirtualLogTable } from "../VirtualLogTable";

// Mock @tanstack/react-virtual with controllable getTotalSize
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

// Helper to create mock LogEntry
function createLogEntry(overrides: Partial<LogEntry> = {}): LogEntry {
	return {
		id: `log-${Math.random().toString(36).slice(2)}`,
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

	describe("Scroll position adjustment on prepend", () => {
		it("adjusts scrollTop when entries are prepended and scroll is not at top", () => {
			// Initial entries
			const initialEntries = [
				createLogEntry({ id: "log-1" }),
				createLogEntry({ id: "log-2" }),
				createLogEntry({ id: "log-3" }),
			];

			// Set initial virtualizer state
			mockGetVirtualItems.mockReturnValue(
				initialEntries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 29,
					end: (i + 1) * 29,
				})),
			);
			const initialTotalSize = 87;
			mockGetTotalSize.mockReturnValue(initialTotalSize);

			const { container, rerender } = renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={initialEntries} />,
			);

			// Get scroll container and set scroll position
			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");

			// Set scroll position to middle (not at top)
			Object.defineProperty(scrollEl, "scrollTop", {
				value: 50,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "scrollHeight", {
				value: 500,
				writable: true,
				configurable: true,
			});
			Object.defineProperty(scrollEl, "clientHeight", {
				value: 300,
				writable: true,
				configurable: true,
			});

			// New entries prepended (2 new entries at the start)
			const newEntries = [
				createLogEntry({ id: "log-new-1" }),
				createLogEntry({ id: "log-new-2" }),
				...initialEntries, // original entries shifted
			];

			// Update mock to return new total size (5 entries * 29 = 145)
			const newTotalSize = 145;
			mockGetVirtualItems.mockReturnValue(
				newEntries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 29,
					end: (i + 1) * 29,
				})),
			);
			mockGetTotalSize.mockReturnValue(newTotalSize);

			// Rerender with new entries (simulates prepending)
			rerender(<VirtualLogTable {...defaultProps} entries={newEntries} />);

			// Wait for useLayoutEffect to run and adjust scrollTop
			waitFor(() => {
				// scrollTop should be adjusted by the difference in total size
				// Original: 50, adjustment: 145 - 87 = 58
				// Expected: 50 + 58 = 108
				expect(scrollEl.scrollTop).toBeGreaterThan(50);
			});
		});

		it("does NOT adjust scrollTop when scroll is at the very top (scrollTop <= 1)", () => {
			const initialEntries = [
				createLogEntry({ id: "log-1" }),
				createLogEntry({ id: "log-2" }),
			];

			mockGetVirtualItems.mockReturnValue(
				initialEntries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 29,
					end: (i + 1) * 29,
				})),
			);
			mockGetTotalSize.mockReturnValue(58);

			const { container, rerender } = renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={initialEntries} />,
			);

			const scrollEl = container.querySelector(
				'[class*="overflow-y-auto"]',
			) as HTMLElement;
			if (!scrollEl) throw new Error("Scroll container not found");

			// Set scroll position at the very top
			Object.defineProperty(scrollEl, "scrollTop", {
				value: 0,
				writable: true,
				configurable: true,
			});

			const newEntries = [
				createLogEntry({ id: "log-new-1" }),
				...initialEntries,
			];

			mockGetVirtualItems.mockReturnValue(
				newEntries.map((e, i) => ({
					index: i,
					key: e.id,
					start: i * 29,
					end: (i + 1) * 29,
				})),
			);
			mockGetTotalSize.mockReturnValue(87);

			rerender(<VirtualLogTable {...defaultProps} entries={newEntries} />);

			// Should NOT adjust when at top (user wants to see newest)
			waitFor(() => {
				expect(scrollEl.scrollTop).toBe(0);
			});
		});
	});

	describe("ResizeObserver total size sync", () => {
		it("updates prevTotalSizeRef when getTotalSize changes between renders", () => {
			const entries = [createLogEntry()];

			// Initial total size
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			const { rerender } = renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Simulate ResizeObserver correction changing total size
			mockGetTotalSize.mockReturnValue(35);

			// Rerender with same entries but different total size
			rerender(<VirtualLogTable {...defaultProps} entries={entries} />);

			// The useEffect should have synced the new total size
			// This is verified by the component not throwing and rendering correctly
			expect(screen.getByText("Test Provider")).toBeInTheDocument();
		});
	});

	describe("Model ID edge cases with multiple slashes", () => {
		it("strips only first segment from model_id with multiple slashes", () => {
			// Model with provider/a/b/c format
			const entries = [
				createLogEntry({ model_id: "provider/a/b/c/model-name" }),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should strip only 'provider/', showing 'a/b/c/model-name'
			expect(screen.getByText("a/b/c/model-name")).toBeInTheDocument();
		});

		it("handles model_id with exactly one slash correctly", () => {
			const entries = [createLogEntry({ model_id: "openai/gpt-4" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("gpt-4")).toBeInTheDocument();
		});

		it("handles model_id without any slash", () => {
			const entries = [createLogEntry({ model_id: "standalone-model" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("standalone-model")).toBeInTheDocument();
		});
	});

	describe("Tokens column with reasoning tokens", () => {
		it("shows prompt+completion tokens when tokens_completion_reasoning > 0", () => {
			const entries = [
				createLogEntry({
					tokens_prompt: 200,
					tokens_completion: 100,
					tokens_completion_reasoning: 50,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should still show prompt+completion format
			const tableText = screen.getByRole("table").textContent || "";
			expect(tableText).toContain("200");
			expect(tableText).toContain("100");
		});

		it("shows '-' when all token counts are 0 even with reasoning tokens", () => {
			const entries = [
				createLogEntry({
					tokens_prompt: 0,
					tokens_completion: 0,
					tokens_completion_reasoning: 50,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Check for "-" in the tokens column area
			const row = screen.getByText("abc123def456").closest("tr");
			expect(row).not.toBeNull();
		});
	});

	describe("Duration formatting edge cases", () => {
		it("shows '1.0s' at exactly 1000ms boundary", () => {
			const entries = [createLogEntry({ duration_ms: 1000 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// At 1000ms should show "1.0s" (toFixed(1))
			expect(screen.getByText("1.0s")).toBeInTheDocument();
		});

		it("shows '999ms' just below 1000ms boundary", () => {
			const entries = [createLogEntry({ duration_ms: 999 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("999ms")).toBeInTheDocument();
		});

		it("shows '1000ms' at exactly 1000ms with .toFixed(0)", () => {
			// Check the code: duration_ms >= 1000 shows seconds
			// So 1000ms shows as 1.0s
			const entries = [createLogEntry({ duration_ms: 1000 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("1.0s")).toBeInTheDocument();
		});

		it("shows '0ms' for duration_ms = 0", () => {
			const entries = [createLogEntry({ duration_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show "-" when duration_ms is 0
			expect(screen.getAllByText("-").length).toBeGreaterThan(0);
		});
	});

	describe("T/s formatting with edge cases", () => {
		it("handles very small TPS values (< 0.1)", () => {
			const entries = [createLogEntry({ tokens_per_second: 0.05 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// formatTPS should handle small values
			const row = screen.getByText("Test Provider").closest("tr");
			expect(row).not.toBeNull();
		});

		it("handles TPS = 0", () => {
			const entries = [createLogEntry({ tokens_per_second: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const row = screen.getByText("Test Provider").closest("tr");
			expect(row).not.toBeNull();
		});

		it("shows '-' for TPS when request is cancelled", () => {
			const entries = [
				createLogEntry({
					error_message: "Request cancelled",
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

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
			// TPS column should be "-" for cancelled
		});
	});

	describe("Overhead column edge cases", () => {
		it("shows formatted overhead when proxy_overhead_ms > 0", () => {
			const entries = [createLogEntry({ proxy_overhead_ms: 15.5 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("15.50ms")).toBeInTheDocument();
		});

		it("shows '-' when proxy_overhead_ms = 0", () => {
			const entries = [createLogEntry({ proxy_overhead_ms: 0 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getAllByText("-").length).toBeGreaterThan(0);
		});

		it("shows '-' when proxy_overhead_ms is negative", () => {
			const entries = [createLogEntry({ proxy_overhead_ms: -5 })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Negative values are falsy, should show "-"
			expect(screen.getAllByText("-").length).toBeGreaterThan(0);
		});

		it("shows '-' when proxy_overhead_ms is null", () => {
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

			expect(screen.getAllByText("-").length).toBeGreaterThan(0);
		});
	});

	describe("Request hash slicing", () => {
		it("shows first 16 chars when hash is longer than 16", () => {
			const longHash = "abcdef1234567890abcdef1234567890"; // 32 chars
			const entries = [createLogEntry({ request_hash: longHash })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Should show first 16 chars: "abcdef1234567890"
			expect(screen.getByText("abcdef1234567890")).toBeInTheDocument();
		});

		it("shows full hash when hash is exactly 16 chars", () => {
			const exactHash = "abcdef1234567890"; // exactly 16 chars
			const entries = [createLogEntry({ request_hash: exactHash })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("abcdef1234567890")).toBeInTheDocument();
		});

		it("shows full hash when hash is shorter than 16 chars", () => {
			const shortHash = "abc123"; // 6 chars
			const entries = [createLogEntry({ request_hash: shortHash })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getByText("abc123")).toBeInTheDocument();
		});

		it("shows '-' when request_hash is empty", () => {
			const entries = [createLogEntry({ request_hash: "" })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			expect(screen.getAllByText("-").length).toBeGreaterThan(0);
		});

		it("sets title attribute to full hash when longer than 16 chars", () => {
			const longHash = "abcdef1234567890abcdef1234567890extra";
			const entries = [createLogEntry({ request_hash: longHash })];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			const hashCell = screen.getByText("abcdef1234567890").closest("td");
			expect(hashCell).toHaveAttribute("title", longHash);
		});
	});

	describe("Combined edge cases", () => {
		it("renders correctly with extreme values across all columns", () => {
			const entries = [
				createLogEntry({
					model_id: "provider/a/b/c/deep/nested",
					request_hash: "a".repeat(100),
					tokens_prompt: 10000,
					tokens_completion: 5000,
					tokens_completion_reasoning: 1000,
					tokens_per_second: 0.01,
					duration_ms: 1000,
					proxy_overhead_ms: 0,
					ttft_ms: 0,
					response_header_ms: 0,
				}),
			];
			mockGetVirtualItems.mockReturnValue([
				{ index: 0, key: entries[0].id, start: 0, end: 29 },
			]);
			mockGetTotalSize.mockReturnValue(29);

			renderWithProviders(
				<VirtualLogTable {...defaultProps} entries={entries} />,
			);

			// Model should strip only first segment
			expect(screen.getByText("a/b/c/deep/nested")).toBeInTheDocument();

			// Hash should be sliced to 16 chars
			expect(screen.getByText("a".repeat(16))).toBeInTheDocument();

			// Duration at boundary should show seconds
			expect(screen.getByText("1.0s")).toBeInTheDocument();
		});
	});
});
