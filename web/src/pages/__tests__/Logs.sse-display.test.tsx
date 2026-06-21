import { act, fireEvent, screen, waitFor } from "@testing-library/react";
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
			<button
				type="button"
				data-testid="select-date-btn"
				onClick={() => onSelect("2026-05-10")}
			>
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

	describe("SSE Live Updates", () => {
		it("fetches log by ID and merges on request.completed event", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let singleLogCallCount = 0;
			// Mock cursor endpoint for initial load
			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json({
						entries: [
							createMockLogEntry({
								id: "log-1",
								request_hash: "pending1",
								state: "pending",
								status_code: 0,
							}),
						],
						total: 1,
						has_before: false,
						has_after: false,
					}),
				),
				// Mock the single-log endpoint
				http.get("/api/logs/log-1", () => {
					singleLogCallCount++;
					return HttpResponse.json(
						createMockLogEntry({
							id: "log-1",
							request_hash: "pending1",
							state: "completed",
							status_code: 200,
							provider_name: "TestProvider",
						}),
					);
				}),
			);

			renderWithProviders(<Logs />);

			// Wait for scroll mode to render
			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			// Dispatch request.completed SSE event
			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.completed",
							metadata: { request_id: "log-1", model_id: "test-model" },
						},
					}),
				);
			});

			// The single-log endpoint should have been called
			await waitFor(
				() => {
					expect(singleLogCallCount).toBeGreaterThan(0);
				},
				{ timeout: 3000 },
			);
		});

		it("fetches the row by ID and merges on request.streaming event", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let singleLogCallCount = 0;
			server.use(
				http.get("/api/logs/log-1", () => {
					singleLogCallCount++;
					// The committed provider/model the live row will swap in for
					// its "Resolving" placeholder once the stream starts.
					return HttpResponse.json(
						createMockLogEntry({
							id: "log-1",
							request_hash: "stream1",
							state: "streaming",
							status_code: 0,
							provider_name: "TestProvider",
						}),
					);
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.streaming",
							metadata: {
								request_id: "log-1",
								model_id: "test-model",
								provider_name: "TestProvider",
							},
						},
					}),
				);
			});

			// request.streaming must fetch the row by id and merge it so the
			// provider/model can replace "Resolving" before completion.
			await waitFor(
				() => {
					expect(singleLogCallCount).toBeGreaterThan(0);
				},
				{ timeout: 3000 },
			);
		});

		it("falls back to fetchNewer on request.completed without request_id", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let cursorCallCount = 0;
			server.use(
				http.get("/api/logs/cursor", () => {
					cursorCallCount++;
					return HttpResponse.json({
						entries: [createMockLogEntry({ id: "log-1", model_id: "abc123" })],
						total: 1,
						has_before: false,
						has_after: false,
					});
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			const initialCallCount = cursorCallCount;

			// Dispatch request.completed without request_id
			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.completed",
							metadata: {}, // no request_id
						},
					}),
				);
			});

			// Should trigger a fetchNewer (cursor endpoint called again)
			await waitFor(
				() => {
					expect(cursorCallCount).toBeGreaterThan(initialCallCount);
				},
				{ timeout: 3000 },
			);
		});

		it("falls back to fetchNewer when event metadata is undefined on request.completed", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let cursorCallCount = 0;
			server.use(
				http.get("/api/logs/cursor", () => {
					cursorCallCount++;
					return HttpResponse.json({
						entries: [createMockLogEntry({ id: "log-1", model_id: "abc123" })],
						total: 1,
						has_before: false,
						has_after: false,
					});
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			const initialCallCount = cursorCallCount;

			// Dispatch request.completed with no metadata property at all
			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.completed",
							// no metadata at all - tests the event.metadata?.request_id path
						},
					}),
				);
			});

			// Should trigger a fetchNewer (cursor endpoint called again)
			await waitFor(
				() => {
					expect(cursorCallCount).toBeGreaterThan(initialCallCount);
				},
				{ timeout: 3000 },
			);
		});

		it("calls fetchNewer on request.started event", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let cursorCallCount = 0;
			server.use(
				http.get("/api/logs/cursor", () => {
					cursorCallCount++;
					return HttpResponse.json({
						entries: [createMockLogEntry({ id: "log-1", model_id: "abc123" })],
						total: 1,
						has_before: false,
						has_after: false,
					});
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			const initialCallCount = cursorCallCount;

			// Dispatch request.started event
			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.started",
							metadata: { request_id: "new-request", model_id: "test-model" },
						},
					}),
				);
			});

			// Should trigger a fetchNewer (cursor endpoint called again)
			await waitFor(
				() => {
					expect(cursorCallCount).toBeGreaterThan(initialCallCount);
				},
				{ timeout: 3000 },
			);
		});

		it("falls back to fetchNewer when api.logs.get rejects on request.completed", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let cursorCallCount = 0;
			server.use(
				http.get("/api/logs/cursor", () => {
					cursorCallCount++;
					return HttpResponse.json({
						entries: [createMockLogEntry({ id: "log-1", model_id: "abc123" })],
						total: 1,
						has_before: false,
						has_after: false,
					});
				}),
				// Mock single-log endpoint to return 500 error
				http.get("/api/logs/log-1", () => {
					return HttpResponse.json(
						{ error: "internal error" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			const initialCallCount = cursorCallCount;

			// Dispatch request.completed with request_id, but api.logs.get will fail
			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: {
							type: "request.completed",
							metadata: { request_id: "log-1", model_id: "test-model" },
						},
					}),
				);
			});

			// Should trigger fetchNewer as fallback (cursor endpoint called again)
			await waitFor(
				() => {
					expect(cursorCallCount).toBeGreaterThan(initialCallCount);
				},
				{ timeout: 3000 },
			);
		});
	});

	describe("parseGoDuration with minutes and seconds only", () => {
		it("parses minutes-only duration correctly", async () => {
			// Override settings with minutes-only stale timeout (45m0s = 2,700,000ms)
			server.use(
				http.get("/api/settings", () =>
					HttpResponse.json({
						stale_request_timeout: "45m0s",
					}),
				),
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "stale-min-001",
								state: "pending",
								created_at: new Date(Date.now() - 50 * 60 * 1000).toISOString(),
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("stale-min-001")).toBeInTheDocument();
			});

			// 50min old pending request with 45min threshold → stale
			expect(screen.getByText("⚠")).toBeInTheDocument();
		});

		it("parses seconds-only duration correctly", async () => {
			// Override settings with seconds-only stale timeout (90s = 90,000ms)
			server.use(
				http.get("/api/settings", () =>
					HttpResponse.json({
						stale_request_timeout: "90s",
					}),
				),
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "stale-sec-001",
								state: "streaming",
								created_at: new Date(Date.now() - 120 * 1000).toISOString(),
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("stale-sec-001")).toBeInTheDocument();
			});

			// 2min old streaming request with 90s threshold → stale
			expect(screen.getByText("⚠")).toBeInTheDocument();
		});
	});

	describe("Clear Date Filter via X Button", () => {
		it("clears date filter when X button is clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(createMockLogs([createMockLogEntry()])),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("test-model")).toBeInTheDocument();
			});

			// Apply a date filter first
			const calendarButton = screen.getByLabelText("Filter by date range");
			await user.click(calendarButton);

			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});

			// Select start and end dates via mock calendar.
			// Use fireEvent for interactions inside the portaled popover to avoid
			// the mousedown-based click-outside handler closing the popover.
			const selectBtn = screen.getByTestId("select-date-btn");
			fireEvent.click(selectBtn); // Select Start
			fireEvent.click(selectBtn); // Select End

			// Click Apply
			const applyButton = screen.getByRole("button", { name: /apply/i });
			fireEvent.click(applyButton);

			await waitFor(() => {
				expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
			});

			// Now the X button should appear
			const clearButton = screen.getByLabelText(/Clear date filter/);
			expect(clearButton).toBeInTheDocument();

			// Click the X button to clear
			await user.click(clearButton);

			// X button should disappear
			await waitFor(() => {
				expect(
					screen.queryByLabelText(/Clear date filter/),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("View Mode Toggle Back to Paginate", () => {
		it("switches from scroll mode back to paginate mode", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json({
						entries: [],
						total: 0,
						has_before: false,
						has_after: false,
					}),
				),
				http.get("/api/logs", () =>
					HttpResponse.json(createMockLogs([createMockLogEntry()])),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByTestId("virtual-log-table")).toBeInTheDocument();
			});

			// Click the switch to pagination mode button
			const switchToPaginateBtn = screen.getByLabelText(
				"Switch to pagination mode",
			);
			await user.click(switchToPaginateBtn);

			// Should now show the paginate table (not virtual-log-table)
			await waitFor(() => {
				expect(
					screen.queryByTestId("virtual-log-table"),
				).not.toBeInTheDocument();
			});

			// Button should now offer switching to scroll mode
			expect(
				screen.getByLabelText("Switch to scroll mode"),
			).toBeInTheDocument();
		});

		it("persists view mode to localStorage", async () => {
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

			// Click switch to scroll mode
			const switchToScrollBtn = screen.getByLabelText("Switch to scroll mode");
			await user.click(switchToScrollBtn);

			// View mode should be persisted
			expect(localStorage.getItem("requestLogsViewMode")).toBe("scroll");
		});
	});

	describe("Scroll Mode Error State", () => {
		it("shows error message in scroll mode when fetch fails", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json(
						{ error: "internal server error" },
						{ status: 500 },
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText(/Failed to load logs/)).toBeInTheDocument();
			});
		});
	});

	describe("Scroll Mode Initial Loading", () => {
		it("shows loading spinner during initial scroll fetch", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			// Delay the cursor response so loading state is visible
			server.use(
				http.get("/api/logs/cursor", async () => {
					await new Promise((resolve) => setTimeout(resolve, 500));
					return HttpResponse.json({
						entries: [],
						total: 0,
						has_before: false,
						has_after: false,
					});
				}),
			);

			renderWithProviders(<Logs />);

			// LoadingSpinner should appear (role="status")
			await waitFor(() => {
				expect(screen.getByRole("status")).toBeInTheDocument();
			});
		});
	});

	describe("Status Badge Muted Fallback", () => {
		it("displays muted badge for 3xx status codes", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "3xx-001",
								status_code: 301,
								state: "completed",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("3xx-001")).toBeInTheDocument();
			});

			const statusElement = screen.getByText("301");
			expect(statusElement).toBeInTheDocument();
			// Find the span containing the status text and check its class directly
			const badge = statusElement.closest("[data-test-variant]");
			expect(badge).toHaveClass("ui-badge-neutral");
		});
	});

	describe("isCancelled Broader Match", () => {
		it("detects 'request cancelled' as cancelled", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "cancel-broad-001",
								error_message: "request cancelled by user",
								state: "completed",
								status_code: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("cancel-broad-001")).toBeInTheDocument();
			});

			expect(screen.getByText("Interrupted")).toBeInTheDocument();
		});
	});

	describe("Page Size Change", () => {
		it("changes page size via PaginationBar select", async () => {
			let capturedUrl = "";
			const newPageSize = 50;

			server.use(
				http.get("/api/logs", ({ request }) => {
					capturedUrl = request.url;
					return HttpResponse.json(
						createMockLogs(
							Array.from({ length: newPageSize }, (_, i) =>
								createMockLogEntry({
									id: `log-${i}`,
									model_id: `hash${i}`,
								}),
							),
							100,
							1,
							newPageSize,
						),
					);
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("hash0")).toBeInTheDocument();
			});

			// Default pageSize is 20, so select should show "20 / page"
			const pageSizeSelect = screen.getByDisplayValue("20 / page");
			expect(pageSizeSelect).toBeInTheDocument();

			// Change to 50 per page
			await user.selectOptions(pageSizeSelect, "50");

			// Select should now show 50
			expect(screen.getByDisplayValue("50 / page")).toBeInTheDocument();

			// Verify the API was called with the correct per_page parameter
			await waitFor(() => {
				expect(capturedUrl).toContain("per_page=50");
			});
		});
	});

	describe("PaginationBar Hidden with Zero Entries", () => {
		it("does not render PaginationBar when no entries exist", async () => {
			server.use(
				http.get("/api/logs", () => HttpResponse.json(createMockLogs([], 0))),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("No logs found")).toBeInTheDocument();
			});

			// When PaginationBar is hidden, the page size select (combobox) should not exist
			expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
		});
	});

	describe("Overhead Accent with Individual Components", () => {
		it("uses accent color when only parse_ms is non-zero", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "oh-parse-001",
								proxy_overhead_ms: 20,
								parse_ms: 5,
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
				expect(screen.getByText("oh-parse-001")).toBeInTheDocument();
			});

			const overheadElement = screen.getByText("20.00ms");
			expect(overheadElement).toBeInTheDocument();
			expect(overheadElement.className).toContain("accent");
		});

		it("uses accent color when only provider_lookup_ms is non-zero", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								model_id: "oh-prov-001",
								proxy_overhead_ms: 30,
								parse_ms: 0,
								model_lookup_ms: 0,
								provider_lookup_ms: 8,
								key_decrypt_ms: 0,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("oh-prov-001")).toBeInTheDocument();
			});

			const overheadElement = screen.getByText("30.00ms");
			expect(overheadElement).toBeInTheDocument();
			expect(overheadElement.className).toContain("accent");
		});
	});

	describe("Combined Filters", () => {
		it("applies model, provider, and status filters simultaneously", async () => {
			let capturedUrl = "";

			server.use(
				http.get("/api/logs", ({ request }) => {
					capturedUrl = request.url;
					return HttpResponse.json(createMockLogs([createMockLogEntry()]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("test-model")).toBeInTheDocument();
			});

			// Type model filter
			const modelInput = screen.getByPlaceholderText("Filter by model ID…");
			await user.type(modelInput, "gpt-4");

			// Type provider filter
			const providerInput = screen.getByPlaceholderText("Filter by provider…");
			await user.type(providerInput, "openai");

			// Select status filter
			const statusButton = screen.getByRole("button", {
				name: "Status",
			});
			await user.click(statusButton);
			const option2xx = screen.getByText("2XX");
			await user.click(option2xx);

			await waitFor(() => {
				// URL should contain all three filters
				expect(capturedUrl).toContain("model_id=gpt-4");
				expect(capturedUrl).toContain("provider_id=openai");
				expect(capturedUrl).toContain("status_code=2xx");
			});
		});
	});
});
