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

	describe("App Logs Mode", () => {
		it("switches to app logs mode when Logs tab is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Click Logs tab
			const logsTab = screen.getByText("Logs");
			await user.click(logsTab);

			// Should switch to app logs view - check for AppLogs page header
			await waitFor(() => {
				expect(
					screen.getByText("Server application log output"),
				).toBeInTheDocument();
			});
		});
	});

	describe("API Integration", () => {
		it("fetches logs from correct endpoint", async () => {
			let apiCalled = false;
			server.use(
				http.get("/api/logs", ({ request }) => {
					apiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json({
						entries: [],
						total: 0,
						page: 1,
						per_page: 25,
					});
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(apiCalled).toBe(true);
			});
		});

		it("passes filter parameters to API", async () => {
			let capturedParams: {
				model_id: string | null;
				provider_id: string | null;
				status_code: string | null;
			} | null = null;
			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					capturedParams = {
						model_id: url.searchParams.get("model_id"),
						provider_id: url.searchParams.get("provider_id"),
						status_code: url.searchParams.get("status_code"),
					};
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Apply filters
			const modelFilter = screen.getByPlaceholderText("Filter by model ID…");
			await user.type(modelFilter, "test-model");

			await waitFor(
				() => {
					expect(capturedParams?.model_id).toBe("test-model");
				},
				{ timeout: 2000 },
			);
		});

		it("passes pagination parameters to API", async () => {
			let capturedParams: {
				page: string | null;
				per_page: string | null;
			} | null = null;
			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					capturedParams = {
						page: url.searchParams.get("page"),
						per_page: url.searchParams.get("per_page"),
					};
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(
				() => {
					expect(capturedParams?.page).toBe("1");
					expect(capturedParams?.per_page).toBe("20");
				},
				{ timeout: 2000 },
			);
		});

		it("passes sort parameters to API", async () => {
			let capturedParams: {
				sort_by: string | null;
				sort_dir: string | null;
			} | null = null;
			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					capturedParams = {
						sort_by: url.searchParams.get("sort_by"),
						sort_dir: url.searchParams.get("sort_dir"),
					};
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(capturedParams?.sort_by).toBe("time");
				expect(capturedParams?.sort_dir).toBe("desc");
			});
		});
	});

	describe("Stale Request Detection", () => {
		it("displays stale warning for old pending requests", async () => {
			// Create a log entry that's older than the stale threshold
			const oldDate = new Date(Date.now() - 31 * 60 * 60 * 1000).toISOString();
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								created_at: oldDate,
								model_id: "abc123",
								status_code: 0,
								state: "pending",
							}),
						]),
					),
				),
				http.get("/api/settings", () =>
					HttpResponse.json({ stale_request_timeout: "30m0s" }),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show stale warning icon
			expect(screen.getByText("⚠")).toBeInTheDocument();
		});
	});

	describe("Token Display", () => {
		it("displays token counts in prompt+completion format", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								tokens_prompt: 100,
								tokens_completion: 200,
								tokens_per_second: 50,
								ttft_ms: 250,
								response_header_ms: 250,
								duration_ms: 6000,
								proxy_overhead_ms: 45,
								parse_ms: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				// Token counts render as 3 separate DOM nodes: "100" "+" "200"
				// getByText can't match across elements, so find the row and
				// assert on the cell's full text content instead.
				const row = screen.getByText("abc123").closest("tr");
				const tokenCells = within(row as HTMLTableRowElement).getAllByRole(
					"cell",
				);
				// Tokens cell is after Hash, Model, Provider, Status columns
				const tokenCell = tokenCells.find((c) => c.textContent?.includes("+"));
				expect(tokenCell).toHaveTextContent("100+200");
			});
		});

		it("displays dash when no tokens", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show dash for tokens
			const tokenCells = screen.getAllByText("-");
			expect(tokenCells.length).toBeGreaterThan(0);
		});
	});

	describe("Duration Formatting", () => {
		it("formats duration in seconds for values >= 1000ms", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								duration_ms: 6500,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show 6.5s
			expect(screen.getByText("6.5s")).toBeInTheDocument();
		});

		it("formats duration in milliseconds for values < 1000ms", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								duration_ms: 450,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show 450ms
			expect(screen.getByText("450ms")).toBeInTheDocument();
		});
	});

	describe("Overhead Display", () => {
		it("displays overhead value when present", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								duration_ms: 1000,
								proxy_overhead_ms: 45,
								parse_ms: 5,
								model_lookup_ms: 10,
								provider_lookup_ms: 20,
								key_decrypt_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show overhead value (formatMs adds 2 decimal places)
			expect(screen.getByText("45.00ms")).toBeInTheDocument();
		});

		it("displays dash when no overhead", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "abc123",
								duration_ms: 1000,
								proxy_overhead_ms: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Should show dash for overhead
			const overheadCells = document.querySelectorAll("td");
			const dashCells = Array.from(overheadCells).filter(
				(cell) => cell.textContent === "-",
			);
			expect(dashCells.length).toBeGreaterThan(0);
		});
	});
});
