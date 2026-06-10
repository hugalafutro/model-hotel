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

import { createMockLogEntry, createMockLogs } from "../../test/logFixtures";

describe("Logs", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		// Default to paginate mode so existing assertions match
		localStorage.setItem("requestLogsViewMode", "paginate");
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
});
