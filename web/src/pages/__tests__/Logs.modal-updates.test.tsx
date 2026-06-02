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
			// Text may be split across nodes, so check key parts
			expect(screen.getByText("of")).toBeInTheDocument();
			expect(screen.getByText("50")).toBeInTheDocument();
			expect(screen.getByText("entries")).toBeInTheDocument();
			// Page navigation buttons - use getAllByRole since buttons may appear multiple times
			const prevButtons = screen.getAllByRole("button", { name: "Prev" });
			const nextButtons = screen.getAllByRole("button", { name: "Next" });
			expect(prevButtons.length).toBeGreaterThan(0);
			expect(nextButtons.length).toBeGreaterThan(0);
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
});
