import { screen, waitFor } from "@testing-library/react";
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
vi.mock("../Logs/AccentCalendar", () => ({
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
	ttft_ms: number;
	duration_ms: number;
	proxy_overhead_ms: number;
	state: "completed" | "pending";
	error_message: string;
	parse_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
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
		ttft_ms: 0,
		duration_ms: 0,
		proxy_overhead_ms: 0,
		state: "completed",
		error_message: "",
		parse_ms: 0,
		model_lookup_ms: 0,
		provider_lookup_ms: 0,
		key_decrypt_ms: 0,
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
			const statusButton = screen.getByText("Status");
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
			const modelHeader = screen.getByRole("button", { name: "Model" });

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
				// Wait for the sort header to actually render (not just the filter)
				const thead = document.querySelector("thead");
				if (!thead) throw new Error("Table header not yet rendered");
				expect(thead.textContent).toContain("Status");
			});

			// "Status" text appears in both the SortableHeader button and the FilterDropdown.
			// The other sort tests use getByRole with unique names, but "Status" is ambiguous.
			// We scope to the <thead> to isolate the sort header from filter controls.
			const getStatusHeader = () => {
				const thead = document.querySelector("thead");
				if (!thead) throw new Error("No thead found");
				const buttons = thead.querySelectorAll("button");
				const statusBtn = Array.from(buttons).find(
					(b) => b.textContent?.includes("Status"),
				);
				if (!statusBtn) throw new Error("No Status button in thead");
				return statusBtn;
			};

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
				expect(screen.getByText("100+200")).toBeInTheDocument();
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
});
