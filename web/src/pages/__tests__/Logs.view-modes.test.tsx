import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Logs } from "../Logs";

// Mock LogDetailModal component
vi.mock("../../components/LogDetailModal", () => ({
	LogDetailModal: ({
		log,
		onClose,
	}: {
		log: { id: string };
		onClose: () => void;
	}) => (
		<div data-testid="log-detail-modal">
			<span>Log Detail: {log.id}</span>
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));

// Mock AccentCalendar component
vi.mock("../../components/AccentCalendar", () => ({
	AccentCalendar: ({
		from,
		to,
		onSelect,
	}: {
		from: string;
		to: string;
		onSelect: (d: string) => void;
	}) => (
		<div data-testid="accent-calendar">
			<span>From: {from}</span>
			<span>To: {to}</span>
			<button type="button" onClick={() => onSelect("2026-05-10")}>
				Select Date
			</button>
		</div>
	),
}));

// Mock VirtualLogTable component
vi.mock("../../components/VirtualLogTable", () => ({
	VirtualLogTable: ({
		entries,
		total,
		hasBefore,
		hasAfter,
		onRowClick,
		sortDir,
		onSortToggle,
		/* isLoadingBefore, isLoadingAfter, onFetchNewer, onFetchOlder accepted but unused in mock */
	}: {
		entries: Array<{ id: string; request_hash: string }>;
		total: number;
		hasBefore: boolean;
		hasAfter: boolean;
		isLoadingBefore?: boolean;
		isLoadingAfter?: boolean;
		onFetchNewer?: () => void;
		onFetchOlder?: () => void;
		onRowClick: (entry: { id: string }) => void;
		sortDir: string;
		onSortToggle: () => void;
	}) => (
		<div data-testid="virtual-log-table">
			<span>VirtualLogTable: {total} entries</span>
			<span>HasBefore: {hasBefore ? "yes" : "no"}</span>
			<span>HasAfter: {hasAfter ? "yes" : "no"}</span>
			<span>SortDir: {sortDir}</span>
			{entries.map((e) => (
				<button key={e.id} type="button" onClick={() => onRowClick(e)}>
					{e.request_hash}
				</button>
			))}
			<button type="button" onClick={onSortToggle}>
				Toggle Sort
			</button>
		</div>
	),
}));

import { createMockLogEntry, createMockLogs } from "../../test/logFixtures";

describe("Logs", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		// Default to paginate mode so existing assertions match
		localStorage.setItem("requestLogsViewMode", "paginate");
	});

	describe("View Mode Toggle", () => {
		it("switches from paginate to scroll mode when toggle button is clicked", async () => {
			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json({
						entries: [],
						total: 0,
						has_before: false,
						has_after: false,
					}),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Live")).toBeInTheDocument();
			});

			// Click the switch to scroll mode button
			const switchToScrollBtn = screen.getByLabelText("Switch to scroll mode");
			await user.click(switchToScrollBtn);

			// Should now show VirtualLogTable
			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			// Button should now have different aria-label
			expect(
				screen.getByLabelText("Switch to pagination mode"),
			).toBeInTheDocument();
		});
	});

	describe("Scroll Mode Rendering", () => {
		it("renders VirtualLogTable in scroll mode with log entries", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json({
						entries: [
							{
								id: "log-scroll-001",
								request_hash: "scroll123",
							},
						],
						total: 1,
						has_before: true,
						has_after: false,
					}),
				),
				http.get("/api/logs", () => HttpResponse.json(createMockLogs([]))),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			expect(screen.getByText("scroll123")).toBeInTheDocument();
		});
	});

	describe("Model ID Display", () => {
		it("displays hotel/ prefixed model ID in full", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "hotel-model-001",
								model_id: "hotel/my-group",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("hotel-model-001")).toBeInTheDocument();
			});

			expect(screen.getByText("hotel/my-group")).toBeInTheDocument();
		});

		it("strips provider prefix from non-hotel slash model IDs", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "openai-model-001",
								model_id: "openai/gpt-4",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("openai-model-001")).toBeInTheDocument();
			});

			// Should show gpt-4 but NOT openai/gpt-4
			expect(screen.getByText("gpt-4")).toBeInTheDocument();
			expect(
				screen.queryByText("openai/gpt-4", { exact: true }),
			).not.toBeInTheDocument();
		});

		it("displays model ID as-is when no slash present", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "simple-model-001",
								model_id: "llama3",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("simple-model-001")).toBeInTheDocument();
			});

			expect(screen.getByText("llama3")).toBeInTheDocument();
		});
	});

	describe("Streaming Live Indicator", () => {
		it("displays Live indicator for in-progress streaming requests", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "streaming-001",
								state: "streaming",
								created_at: new Date().toISOString(),
								status_code: 0,
								provider_name: "TestProvider",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("streaming-001")).toBeInTheDocument();
			});

			// Check for Live indicator in the status badge within the row
			const row = screen.getByText("streaming-001").closest("tr");
			expect(row).not.toBeNull();
			const liveElement = within(row as HTMLTableRowElement).getByText("Live");
			expect(liveElement).toBeInTheDocument();
			expect(liveElement.className).toContain("text-blue-400");
		});
	});

	describe("Status Badge Variants", () => {
		it("displays orange badge for 4xx status codes", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "4xx-001",
								status_code: 403,
								state: "completed",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("4xx-001")).toBeInTheDocument();
			});

			const statusElement = screen.getByText("403");
			expect(statusElement).toBeInTheDocument();
			// Check that badge has orange variant (text-orange-400)
			const badge = statusElement.closest("[data-test-variant]");
			expect(badge?.className).toContain("text-orange-400");
		});

		it("displays error badge for 5xx status codes with completed state", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "5xx-001",
								status_code: 500,
								state: "completed",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("5xx-001")).toBeInTheDocument();
			});

			const statusElement = screen.getByText("500");
			expect(statusElement).toBeInTheDocument();
			// Check that badge has error variant (text-red-400)
			const badge = statusElement.closest("[data-test-variant]");
			expect(badge?.className).toContain("text-red-400");
		});
	});

	describe("Cancelled Request Variants", () => {
		it("detects disconnect error as cancelled", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "cancel-001",
								error_message: "stream disconnect",
								state: "completed",
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("cancel-001")).toBeInTheDocument();
			});

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it("detects client disconnected error as cancelled", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "cancel-002",
								error_message: "client disconnected",
								state: "completed",
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("cancel-002")).toBeInTheDocument();
			});

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it("detects timed out error as cancelled", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "timeout-001",
								error_message:
									"all providers failed: upstream request timed out",
								state: "completed",
								status_code: 502,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("timeout-001")).toBeInTheDocument();
			});

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});

		it("displays dash for TPS on cancelled requests", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "cancel-003",
								error_message: "cancelled",
								tokens_per_second: 42.5,
								state: "completed",
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("cancel-003")).toBeInTheDocument();
			});

			// Find the T/s column index from the table header, then check the
			// corresponding cell in the data row - avoids hard-coded index
			const table = screen.getByText("cancel-003").closest("table");
			expect(table).not.toBeNull();
			const headerRow = (table as HTMLElement).querySelector("thead tr");
			expect(headerRow).not.toBeNull();
			const headers = within(headerRow as HTMLElement).getAllByRole(
				"columnheader",
			);
			const tpsIndex = headers.findIndex((h) => h.textContent?.includes("T/s"));
			expect(tpsIndex).toBeGreaterThanOrEqual(0);

			const row = screen.getByText("cancel-003").closest("tr");
			expect(row).not.toBeNull();
			const cells = within(row as HTMLTableRowElement).getAllByRole("cell");
			expect(cells[tpsIndex].textContent).toBe("-");
		});
	});

	describe("TTFT Display", () => {
		it("displays TTFT value when positive", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "ttft-001",
								ttft_ms: 350,
								response_header_ms: 100,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("ttft-001")).toBeInTheDocument();
			});

			expect(screen.getByText("350.0ms")).toBeInTheDocument();
		});
	});

	describe("In-Progress Duration", () => {
		it("displays blue dash for in-progress requests with zero duration", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "inprogress-001",
								state: "streaming",
								duration_ms: 0,
								created_at: new Date().toISOString(),
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("inprogress-001")).toBeInTheDocument();
			});

			// Find the duration cell - should have blue dash
			const row = screen.getByText("inprogress-001").closest("tr");
			if (row) {
				const cells = within(row).getAllByRole("cell");
				// Find cell with dash that has blue styling
				const dashCell = cells.find(
					(cell) =>
						cell.textContent === "-" &&
						(cell.className?.includes("blue") ||
							cell.querySelector("[class*='blue']")),
				);
				expect(dashCell).toBeInTheDocument();
			}
		});
	});

	describe("Overhead with Accent Color", () => {
		it("displays overhead with accent styling when components are present", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								proxy_overhead_ms: 45,
								parse_ms: 5,
								model_lookup_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Check that overhead has accent styling
			const overheadElement = screen.getByText("45.00ms");
			expect(overheadElement).toBeInTheDocument();
			expect(overheadElement.className).toContain("accent");
		});

		it("displays overhead with gray styling when no components", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								proxy_overhead_ms: 15,
								parse_ms: 0,
								model_lookup_ms: 0,
								provider_lookup_ms: 0,
								key_decrypt_ms: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Check that overhead has gray styling
			const overheadElement = screen.getByText("15.00ms");
			expect(overheadElement).toBeInTheDocument();
			expect(overheadElement.className).toContain("gray");
		});
	});

	describe("Virtual Key Fallback and Case-Insensitive Internal", () => {
		it("falls back to virtual_key_id when name is empty", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "vk-fallback-001",
								virtual_key_name: "",
								virtual_key_id: "vk-abc123",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("vk-fallback-001")).toBeInTheDocument();
			});

			expect(screen.getByText("vk-abc123")).toBeInTheDocument();
		});

		it("treats Internal (capitalized) as internal key", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "internal-cap-001",
								virtual_key_name: "Internal",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("internal-cap-001")).toBeInTheDocument();
			});

			expect(screen.getByText("internal")).toBeInTheDocument();
		});

		it("treats INTERNAL (uppercase) as internal key", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								virtual_key_name: "INTERNAL",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			expect(screen.getByText("internal")).toBeInTheDocument();
		});

		it("does NOT set title on key cell when virtual_key_name and virtual_key_id are empty", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "vk-empty-001",
								virtual_key_deleted: false,
								virtual_key_name: "",
								virtual_key_id: "",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("vk-empty-001")).toBeInTheDocument();
			});

			// Find the row and check the key cell has no title attribute
			const row = screen.getByText("vk-empty-001").closest("tr");
			expect(row).not.toBeNull();
			if (row) {
				const cells = within(row).getAllByRole("cell");
				// Key column is the last column
				const keyCell = cells[cells.length - 1];
				expect(keyCell).not.toHaveAttribute("title");
			}
		});
	});

	describe("parseGoDuration via Custom Stale Timeout", () => {
		it("uses custom stale timeout from settings with hours", async () => {
			// Override settings with custom stale timeout (1h30m)
			// Entry is 80 min old, so should NOT be stale
			server.use(
				http.get("/api/settings", () =>
					HttpResponse.json({
						stale_request_timeout: "1h30m0s",
					}),
				),
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								state: "streaming",
								// 80 min ago (under 1h30m threshold)
								created_at: new Date(Date.now() - 80 * 60 * 1000).toISOString(),
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should NOT show stale warning (under threshold)
			expect(screen.queryByText("⚠")).not.toBeInTheDocument();
		});
	});
});
