import { act, screen, waitFor, within } from "@testing-library/react";
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

// Factory functions for creating mock log entries
interface MockLogEntry {
	id: string;
	created_at: string;
	request_hash: string;
	model_id: string;
	provider_name: string;
	status_code: number;
	tokens_prompt: number;
	tokens_completion: number;
	tokens_per_second: number;
	tokens_completion_reasoning: number;
	ttft_ms: number;
	response_header_ms: number;
	duration_ms: number;
	proxy_overhead_ms: number;
	state: "completed" | "pending" | "streaming";
	error_message: string;
	parse_ms: number;
	failover_lookup_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	dial_ms: number;
	virtual_key_deleted: boolean;
	virtual_key_id: string;
	virtual_key_name?: string;
}

function createMockLogEntry(
	overrides: Partial<MockLogEntry> = {},
): MockLogEntry {
	const defaultEntry: MockLogEntry = {
		id: "log-001",
		created_at: "2026-05-11T10:00:00Z",
		request_hash: "abc123",
		model_id: "test-model",
		provider_name: "Test",
		status_code: 200,
		tokens_prompt: 0,
		tokens_completion: 0,
		tokens_per_second: 0,
		tokens_completion_reasoning: 0,
		ttft_ms: 0,
		response_header_ms: 0,
		duration_ms: 0,
		proxy_overhead_ms: 0,
		state: "completed",
		error_message: "",
		parse_ms: 0,
		failover_lookup_ms: 0,
		model_lookup_ms: 0,
		provider_lookup_ms: 0,
		key_decrypt_ms: 0,
		dial_ms: 0,
		virtual_key_deleted: false,
		virtual_key_id: "",
	};
	return { ...defaultEntry, ...overrides };
}

function createMockLogs(
	entries: MockLogEntry[],
	total?: number,
	page: number = 1,
	perPage: number = 25,
) {
	return {
		entries,
		total: total ?? entries.length,
		page,
		per_page: perPage,
	};
}

describe("Logs", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		// Default to paginate mode so existing assertions match
		localStorage.setItem("requestLogsViewMode", "paginate");
	});

	describe("Initial Rendering", () => {
		it("renders page header with correct title and description", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests").length).toBeGreaterThan(0);
			});
			expect(
				screen.getByText("Monitor API requests across all providers and keys"),
			).toBeInTheDocument();
		});

		it("renders live updates badge", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Live")).toBeInTheDocument();
			});
		});

		it("renders submode toggle buttons", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests").length).toBeGreaterThan(0);
			});
			expect(screen.getByText("Logs")).toBeInTheDocument();
		});
	});

	describe("Request Logs Mode", () => {
		it("renders request logs table headers", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123def4567890",
								provider_name: "Test Provider",
								tokens_prompt: 100,
								tokens_completion: 200,
								tokens_per_second: 50,
								ttft_ms: 250,
								response_header_ms: 250,
								duration_ms: 6000,
								proxy_overhead_ms: 45,
								parse_ms: 5,
								model_lookup_ms: 10,
								provider_lookup_ms: 20,
								key_decrypt_ms: 10,
								virtual_key_id: "vk-001",
							}),
						]),
					),
				),
			);
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Time/Date")).toBeInTheDocument();
			});
			expect(screen.getByText("Hash")).toBeInTheDocument();
			expect(screen.getByText("Model")).toBeInTheDocument();
			expect(screen.getByText("Provider")).toBeInTheDocument();
			expect(screen.getAllByText("Status").length).toBeGreaterThan(0);
			expect(screen.getByText("Tokens")).toBeInTheDocument();
			expect(screen.getByText("T/s")).toBeInTheDocument();
			expect(screen.getByText("TTFT")).toBeInTheDocument();
			expect(screen.getByText("Duration")).toBeInTheDocument();
			expect(screen.getByText("Overhead")).toBeInTheDocument();
			expect(screen.getByText("Key")).toBeInTheDocument();
		});

		it("renders filter inputs", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests").length).toBeGreaterThan(0);
			});

			// Filter inputs should be present
			expect(
				screen.getByPlaceholderText("Filter by model ID…"),
			).toBeInTheDocument();
			expect(
				screen.getByPlaceholderText("Filter by provider…"),
			).toBeInTheDocument();
		});

		it("renders status filter dropdown", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests").length).toBeGreaterThan(0);
			});

			// Status dropdown should be present
			expect(screen.getAllByText("Status").length).toBeGreaterThan(0);
		});

		it("renders date picker button", async () => {
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests").length).toBeGreaterThan(0);
			});

			// Calendar icon button should be present
			const calendarButtons = screen.getAllByRole("button").filter((btn) => {
				const svg = btn.querySelector("svg");
				return svg;
			});
			expect(calendarButtons.length).toBeGreaterThan(0);
		});

		it("shows empty state when no logs", async () => {
			server.use(
				http.get("/api/logs", () => HttpResponse.json(createMockLogs([]))),
			);
			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("No logs found")).toBeInTheDocument();
			});
		});

		it("renders loading spinner initially", async () => {
			server.use(
				http.get("/api/logs", async () => {
					await new Promise((resolve) => setTimeout(resolve, 500));
					return HttpResponse.json(createMockLogs([]));
				}),
			);
			renderWithProviders(<Logs />);
			expect(screen.getByRole("status", { hidden: true })).toBeInTheDocument();
		});

		it("renders error message when API fails", async () => {
			server.use(
				http.get("/api/logs", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch logs" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText(/Failed to load logs/i)).toBeInTheDocument();
			});
		});
	});

	describe("Logs Data Display", () => {
		it("displays log entries in table", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123def456",
								model_id: "test-model-v1",
								provider_name: "Test Provider",
								tokens_prompt: 100,
								tokens_completion: 200,
								tokens_per_second: 50,
								ttft_ms: 250,
								response_header_ms: 250,
								duration_ms: 6000,
								proxy_overhead_ms: 45,
								virtual_key_name: "Test Key",
								parse_ms: 5,
								model_lookup_ms: 10,
								provider_lookup_ms: 20,
								key_decrypt_ms: 10,
								virtual_key_id: "vk-001",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123def456")).toBeInTheDocument();
			});

			// Log data should be displayed
			expect(screen.getByText("abc123def456")).toBeInTheDocument();
			expect(screen.getByText("test-model-v1")).toBeInTheDocument();
			expect(screen.getByText("Test Provider")).toBeInTheDocument();
			expect(screen.getByText("200")).toBeInTheDocument();
		});

		it("displays truncated request hash (16 chars)", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abcdefghij1234567890",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abcdefghij123456")).toBeInTheDocument();
			});
		});

		it("displays cancelled status for cancelled requests", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								status_code: 0,
								state: "pending",
								error_message: "Request cancelled by user",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Interrupted")).toBeInTheDocument();
			});
		});

		it("displays deleted provider indicator", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								provider_name: "Deleted",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Deleted")).toBeInTheDocument();
			});
		});

		it("displays deleted virtual key indicator", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
								virtual_key_deleted: true,
								virtual_key_id: "vk-001",
								virtual_key_name: "Old Key",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Deleted")[0]).toBeInTheDocument();
			});
		});

		it("displays internal key in lowercase italic", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
								virtual_key_name: "internal",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("internal")).toBeInTheDocument();
			});
		});

		it("displays pending/streaming state indicators", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								created_at: new Date().toISOString(),
								request_hash: "abc123",
								provider_name: "",
								status_code: 0,
								state: "pending",
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Resolving…")).toBeInTheDocument();
			});
		});
	});

	describe("Filtering", () => {
		it("filters by model ID", async () => {
			const mockLogs = createMockLogs([
				createMockLogEntry({
					request_hash: "abc123",
					model_id: "gpt-4",
					provider_name: "OpenAI",
					tokens_prompt: 100,
					tokens_completion: 200,
					tokens_per_second: 50,
					ttft_ms: 250,
					response_header_ms: 250,
					duration_ms: 6000,
					proxy_overhead_ms: 45,
					parse_ms: 5,
					model_lookup_ms: 10,
					provider_lookup_ms: 20,
					key_decrypt_ms: 10,
				}),
			]);

			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					const modelId = url.searchParams.get("model_id");
					if (modelId === "gpt-4") {
						return HttpResponse.json(mockLogs);
					}
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Type in model filter
			const modelFilter = screen.getByPlaceholderText("Filter by model ID…");
			await user.type(modelFilter, "gpt-4");

			// Advance timers to trigger debounce
			vi.useFakeTimers();
			await vi.advanceTimersByTimeAsync(350);
			vi.useRealTimers();

			// Wait for debounced filter to apply
			await waitFor(() => {
				expect(screen.getByText("gpt-4")).toBeInTheDocument();
			});
		});

		it("filters by provider ID", async () => {
			const mockLogs = createMockLogs([
				createMockLogEntry({
					request_hash: "abc123",
					provider_name: "OpenAI",
				}),
			]);

			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					const providerId = url.searchParams.get("provider_id");
					if (providerId === "openai") {
						return HttpResponse.json(mockLogs);
					}
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Type in provider filter
			const providerFilter = screen.getByPlaceholderText("Filter by provider…");
			await user.type(providerFilter, "openai");

			// Advance timers to trigger debounce
			vi.useFakeTimers();
			await vi.advanceTimersByTimeAsync(350);
			vi.useRealTimers();

			// Wait for debounced filter to apply
			await waitFor(() => {
				expect(screen.getByText("OpenAI")).toBeInTheDocument();
			});
		});

		it("filters by status code", async () => {
			const mockLogs = createMockLogs([
				createMockLogEntry({
					request_hash: "abc123",
					status_code: 500,
				}),
			]);

			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					const statusCode = url.searchParams.get("status_code");
					if (statusCode === "5xx") {
						return HttpResponse.json(mockLogs);
					}
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Open status dropdown and select 5XX
			const statusButton = screen.getByRole("button", { name: "Status" });
			await user.click(statusButton);

			// Select 5XX option
			const option5xx = screen.getByText("5XX");
			await user.click(option5xx);

			// Wait for filter to apply
			await waitFor(() => {
				expect(screen.getByText("500")).toBeInTheDocument();
			});
		});
	});

	describe("Date Filtering", () => {
		it("opens date picker when calendar button is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Click calendar button by aria-label
			const calendarButton = screen.getByLabelText("Filter by date range");
			await user.click(calendarButton);

			// Date picker should open
			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});
		});

		it("closes date picker when close button is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Click calendar button by aria-label
			const calendarButton = screen.getByLabelText("Filter by date range");
			await user.click(calendarButton);

			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});

			// Click close button
			const closeButton = screen.getByLabelText("Close date picker");
			await user.click(closeButton);

			// Date picker should close
			await waitFor(() => {
				expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
			});
		});

		it("applies date filter when Apply button is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Click calendar button by aria-label
			const calendarButton = screen.getByLabelText(/filter by date/i);
			await user.click(calendarButton);

			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});

			// Select a date
			const selectButton = screen.getByText("Select Date");
			await user.click(selectButton);

			// Click Apply
			const applyButton = screen.getByText("Apply");
			await user.click(applyButton);

			// Date picker should close
			await waitFor(() => {
				expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
			});
		});

		it("clears date filter when Clear button is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Click calendar button by aria-label
			const calendarButton = screen.getByLabelText(/filter by date/i);
			await user.click(calendarButton);

			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});

			// Select a date twice
			const selectButton = screen.getByText("Select Date");
			await user.click(selectButton);
			await user.click(selectButton);

			// Click Clear
			const clearButton = screen.getByText("Clear");
			await user.click(clearButton);

			// Date picker should close
			await waitFor(() => {
				expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
			});
		});
	});

	describe("Sorting", () => {
		it("sorts by time column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Time/Date")).toBeInTheDocument();
			});

			// Get time header button - initially sorted desc by default
			const timeHeader = screen.getByRole("button", { name: /Time\/Date/i });

			// Click to sort ascending
			await user.click(timeHeader);

			// Verify sort indicator changes to ascending arrow
			expect(timeHeader).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(timeHeader);

			// Verify sort indicator changes to descending arrow
			expect(timeHeader).toHaveTextContent(/↓/);
		});

		it("sorts by model column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Model")).toBeInTheDocument();
			});

			// Get model header button - initially not sorted
			const modelHeader = screen.getByRole("button", {
				name: /Sort by Model/i,
			});

			// Click to sort ascending
			await user.click(modelHeader);

			// Verify sort indicator changes to ascending arrow
			expect(modelHeader).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(modelHeader);

			// Verify sort indicator changes to descending arrow
			expect(modelHeader).toHaveTextContent(/↓/);
		});

		it("sorts by provider column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Provider")).toBeInTheDocument();
			});

			// Get provider header button - initially not sorted
			const providerHeader = screen.getByRole("button", { name: /Provider/i });

			// Click to sort ascending
			await user.click(providerHeader);

			// Verify sort indicator changes to ascending arrow
			expect(providerHeader).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(providerHeader);

			// Verify sort indicator changes to descending arrow
			expect(providerHeader).toHaveTextContent(/↓/);
		});

		it("sorts by status column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				// Wait for the SortableHeader to render with aria-label
				expect(
					screen.getByRole("button", { name: /Sort by Status/i }),
				).toBeInTheDocument();
			});

			// SortableHeader now has aria-label="Sort by {label}" so we can
			// disambiguate from FilterDropdown's "Status" button.
			const getStatusHeader = () =>
				screen.getByRole("button", { name: /Sort by Status/i });

			const statusHeader = getStatusHeader();

			// Verify initially no arrow (time column is sorted by default)
			expect(statusHeader).toHaveTextContent(/Status/);

			// Click to sort ascending
			await user.click(statusHeader);

			// Verify sort indicator changes to ascending arrow
			expect(getStatusHeader()).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(getStatusHeader());

			// Verify sort indicator changes to descending arrow
			expect(getStatusHeader()).toHaveTextContent(/↓/);
		});

		it("sorts by tokens column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Tokens")).toBeInTheDocument();
			});

			// Get tokens header button - initially not sorted
			const tokensHeader = screen.getByRole("button", { name: /Tokens/i });

			// Click to sort ascending
			await user.click(tokensHeader);

			// Verify sort indicator changes to ascending arrow
			expect(tokensHeader).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(tokensHeader);

			// Verify sort indicator changes to descending arrow
			expect(tokensHeader).toHaveTextContent(/↓/);
		});

		it("sorts by duration column when clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							[
								createMockLogEntry(),
								createMockLogEntry({
									id: "log-002",
									created_at: "2026-05-12T11:00:00Z",
									request_hash: "def456",
									model_id: "test-model-2",
									provider_name: "Test-2",
									status_code: 400,
									tokens_prompt: 100,
									tokens_completion: 50,
									tokens_per_second: 10,
									ttft_ms: 200,
									response_header_ms: 200,
									duration_ms: 1500,
									proxy_overhead_ms: 50,
									parse_ms: 10,
									model_lookup_ms: 5,
									provider_lookup_ms: 3,
									key_decrypt_ms: 2,
									virtual_key_id: "key-002",
								}),
							],
							2,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Duration")).toBeInTheDocument();
			});

			// Get duration header button - initially not sorted
			const durationHeader = screen.getByRole("button", { name: /Duration/i });

			// Click to sort ascending
			await user.click(durationHeader);

			// Verify sort indicator changes to ascending arrow
			expect(durationHeader).toHaveTextContent(/↑/);

			// Click again to sort descending
			await user.click(durationHeader);

			// Verify sort indicator changes to descending arrow
			expect(durationHeader).toHaveTextContent(/↓/);
		});
	});

	describe("Log Detail Modal", () => {
		it("opens log detail modal when row is clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								model_id: "test-model",
								provider_name: "Test Provider",
								tokens_prompt: 100,
								tokens_completion: 200,
								tokens_per_second: 50,
								ttft_ms: 250,
								response_header_ms: 250,
								duration_ms: 6000,
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

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Click on the row
			const row = screen.getByText("abc123").closest("tr");
			if (row) {
				await user.click(row);
				await waitFor(() => {
					expect(screen.getByTestId("log-detail-modal")).toBeInTheDocument();
				});
			}
		});

		it("closes log detail modal when close button is clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								model_id: "test-model",
								provider_name: "Test Provider",
								tokens_prompt: 100,
								tokens_completion: 200,
								tokens_per_second: 50,
								ttft_ms: 250,
								response_header_ms: 250,
								duration_ms: 6000,
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

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Click on the row
			const row = screen.getByText("abc123").closest("tr");
			if (row) {
				await user.click(row);

				await waitFor(() => {
					expect(screen.getByTestId("log-detail-modal")).toBeInTheDocument();
				});

				// Click close button
				const closeButton = screen.getByText("Close");
				await user.click(closeButton);

				// Modal should close
				await waitFor(() => {
					expect(
						screen.queryByTestId("log-detail-modal"),
					).not.toBeInTheDocument();
				});
			}
		});
	});

	describe("Live Updates", () => {
		it("toggles live updates on/off when badge is clicked", async () => {
			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("Live")).toBeInTheDocument();
			});

			// Click live badge
			const liveBadge = screen.getByText("Live").closest("button");
			if (liveBadge) {
				await user.click(liveBadge);

				// Should show "Live updates paused" toast
				await waitFor(() => {
					expect(screen.getByText("Live updates paused")).toBeInTheDocument();
				});

				// Click again to resume
				await user.click(liveBadge);

				// Should show "Live updates resumed" toast
				await waitFor(() => {
					expect(screen.getByText("Live updates resumed")).toBeInTheDocument();
				});
			}
		});
	});

	describe("Pagination", () => {
		it("renders pagination bar when logs exist", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							Array.from({ length: 25 }, (_, i) =>
								createMockLogEntry({
									id: `log-${i}`,
									request_hash: `hash${i}`,
								}),
							),
							50,
						),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("hash0")).toBeInTheDocument();
			});

			// Pagination should be visible: shows "1 to 20 of 50 entries" (default pageSize is 20)
			expect(screen.getByText(/1 to 20 of 50 entries/)).toBeInTheDocument();
			// Page navigation buttons should be visible
			expect(screen.getByText("Prev")).toBeInTheDocument();
			expect(screen.getByText("Next")).toBeInTheDocument();
			expect(screen.getByText("2")).toBeInTheDocument();
		});

		it("changes page when pagination button is clicked", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(
							Array.from({ length: 25 }, (_, i) =>
								createMockLogEntry({
									id: `log-${i}`,
									request_hash: `hash${i}`,
								}),
							),
							50,
						),
					),
				),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("hash0")).toBeInTheDocument();
			});

			// Click next page button
			const nextPageButtons = screen.getAllByRole("button").filter((btn) => {
				return btn.textContent === "›";
			});
			if (nextPageButtons.length > 0) {
				await user.click(nextPageButtons[0]);

				// Should navigate to page 2
				await waitFor(() => {
					expect(screen.getByText("Page 2 of 2")).toBeInTheDocument();
				});
			}
		});
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
								request_hash: "abc123",
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
								request_hash: "abc123",
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
				const tokenCells = within(row!).getAllByRole("cell");
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
								request_hash: "abc123",
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
								request_hash: "abc123",
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
								request_hash: "abc123",
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
								request_hash: "abc123",
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
								request_hash: "abc123",
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
			const liveElement = within(row!).getByText("Live");
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
			const badge = statusElement.closest("span");
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
			const badge = statusElement.closest("span");
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
			const headerRow = table!.querySelector("thead tr");
			expect(headerRow).not.toBeNull();
			const headers = within(headerRow as HTMLElement).getAllByRole(
				"columnheader",
			);
			const tpsIndex = headers.findIndex((h) => h.textContent?.includes("T/s"));
			expect(tpsIndex).toBeGreaterThanOrEqual(0);

			const row = screen.getByText("cancel-003").closest("tr");
			expect(row).not.toBeNull();
			const cells = within(row!).getAllByRole("cell");
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

		it("falls back to fetchNewer on request.completed without request_id", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");

			let cursorCallCount = 0;
			server.use(
				http.get("/api/logs/cursor", () => {
					cursorCallCount++;
					return HttpResponse.json({
						entries: [
							createMockLogEntry({ id: "log-1", request_hash: "abc123" }),
						],
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
						entries: [
							createMockLogEntry({ id: "log-1", request_hash: "abc123" }),
						],
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
						entries: [
							createMockLogEntry({ id: "log-1", request_hash: "abc123" }),
						],
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
						entries: [
							createMockLogEntry({ id: "log-1", request_hash: "abc123" }),
						],
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
								request_hash: "stale-min-001",
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
								request_hash: "stale-sec-001",
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
				expect(screen.getByText("abc123")).toBeInTheDocument();
			});

			// Apply a date filter first
			const calendarButton = screen.getByLabelText("Filter by date range");
			await user.click(calendarButton);

			await waitFor(() => {
				expect(screen.getByTestId("accent-calendar")).toBeInTheDocument();
			});

			// Select a date
			const selectButton = screen.getByText("Select Date");
			await user.click(selectButton);

			// Click Apply
			const applyButton = screen.getByText("Apply");
			await user.click(applyButton);

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
								request_hash: "3xx-001",
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
			const badge = statusElement.closest("span");
			expect(badge).toHaveClass("text-gray-400");
		});
	});

	describe("isCancelled Broader Match", () => {
		it("detects 'request cancelled' as cancelled", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "cancel-broad-001",
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
									request_hash: `hash${i}`,
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
								request_hash: "oh-parse-001",
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
								request_hash: "oh-prov-001",
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
				expect(screen.getByText("abc123")).toBeInTheDocument();
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
