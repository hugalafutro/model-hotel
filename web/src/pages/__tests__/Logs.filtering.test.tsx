import { fireEvent, screen, waitFor } from "@testing-library/react";
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
				onClick={() => onSelect(from ? "2026-05-15" : "2026-05-10")}
			>
				{from ? "Select End" : "Select Start"}
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
	endpoint_type?: string;
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

		it("filters by endpoint type", async () => {
			const mockLogs = createMockLogs([
				createMockLogEntry({
					request_hash: "embedhash123",
					model_id: "text-embedding-3-small",
					endpoint_type: "embeddings",
				}),
			]);

			server.use(
				http.get("/api/logs", ({ request }) => {
					const url = new URL(request.url);
					const endpointType = url.searchParams.get("endpoint_type");
					if (endpointType === "embeddings") {
						return HttpResponse.json(mockLogs);
					}
					return HttpResponse.json(createMockLogs([]));
				}),
			);

			const { user } = renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getAllByText("Requests")[0]).toBeInTheDocument();
			});

			// Open endpoint dropdown and select Embeddings
			const endpointButton = screen.getByRole("button", { name: "Endpoint" });
			await user.click(endpointButton);

			const optionEmbeddings = screen.getByText("Embeddings");
			await user.click(optionEmbeddings);

			// Wait for filter to apply
			await waitFor(() => {
				expect(screen.getByText("text-embedding-3-small")).toBeInTheDocument();
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

			// Select start and end dates via mock calendar.
			// Use fireEvent for interactions inside the portaled popover to avoid
			// the mousedown-based click-outside handler closing the popover.
			const selectBtn = screen.getByTestId("select-date-btn");
			fireEvent.click(selectBtn); // Select Start
			fireEvent.click(selectBtn); // Select End

			// Wait for Apply button to appear
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /apply/i }),
				).toBeInTheDocument();
			});
			// Click Apply button (from DateRangePicker)
			const applyButton = screen.getByRole("button", { name: /apply/i });
			fireEvent.click(applyButton);

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

			// Select start and end dates via mock calendar.
			// Use fireEvent for interactions inside the portaled popover.
			const selectBtn = screen.getByTestId("select-date-btn");
			fireEvent.click(selectBtn); // Select Start
			fireEvent.click(selectBtn); // Select End

			// Wait for Clear button to appear
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /clear/i }),
				).toBeInTheDocument();
			});
			// Click Clear button (from DateRangePicker)
			const clearButton = screen.getByRole("button", { name: /clear/i });
			fireEvent.click(clearButton);

			// Date picker should close
			await waitFor(() => {
				expect(screen.queryByTestId("accent-calendar")).not.toBeInTheDocument();
			});
		});
	});
});
