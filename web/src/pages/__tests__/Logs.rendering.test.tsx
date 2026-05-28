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

		it("displays hotel/ model_id with resolved_model_id showing both with tooltip", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								model_id: "hotel/my-failover-group",
								resolved_model_id: "openai/gpt-4o",
								provider_name: "Test",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("hotel/my-failover-group")).toBeInTheDocument();
			});
			expect(screen.getByText("(openai/gpt-4o)")).toBeInTheDocument();
		});

		it("displays non-hotel/ model_id with resolved_model_id not showing resolved", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "abc123",
								model_id: "anthropic/claude-3-5-sonnet",
								resolved_model_id: "anthropic/claude-3-5-sonnet",
								provider_name: "Test",
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("claude-3-5-sonnet")).toBeInTheDocument();
			});
			// Should NOT show the "(resolved: ...)" text
			expect(screen.queryByText("(resolved:")).not.toBeInTheDocument();
		});

		it("shows muted TPS color when request has prompt cache hits", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "cache-hit-hash",
								provider_name: "Test Provider",
								tokens_per_second: 50,
								tokens_prompt_cache_hit: 500,
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("50.0")).toBeInTheDocument();
			});

			// Find the span with the TPS value and check its class
			const tpsSpan = screen.getByText("50.0");
			expect(tpsSpan).toHaveClass("text-(--text-tertiary)");
			expect(tpsSpan).toHaveAttribute("title", "Inflated by prompt cache hits");
		});

		it("shows default TPS color when request has no prompt cache hits", async () => {
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs([
							createMockLogEntry({
								request_hash: "no-cache-hash",
								provider_name: "Test Provider",
								tokens_per_second: 75,
								tokens_prompt_cache_hit: 0,
								duration_ms: 1000,
								proxy_overhead_ms: 10,
							}),
						]),
					),
				),
			);

			renderWithProviders(<Logs />);

			await waitFor(() => {
				expect(screen.getByText("75.0")).toBeInTheDocument();
			});

			// Find the span with the TPS value and check its class
			const tpsSpan = screen.getByText("75.0");
			expect(tpsSpan).toHaveClass("text-gray-400");
			expect(tpsSpan).not.toHaveAttribute("title");
		});
	});
});
