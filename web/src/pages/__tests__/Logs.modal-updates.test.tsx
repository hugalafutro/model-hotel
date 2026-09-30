import { act, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MockLogEntry } from "../../test/logFixtures";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Logs } from "../Logs";

// Mock LogDetailModal component
vi.mock("../../components/LogDetailModal", () => ({
	LogDetailModal: ({
		log,
		nav,
		clock,
		onClose,
	}: {
		log: { id: string; state: string };
		nav?: { onNext: () => void };
		clock?: { nowMs: number };
		onClose: () => void;
	}) => (
		<div data-testid="log-detail-modal">
			<span>Log Detail: {log.id}</span>
			<span>Modal state: {log.state}</span>
			<span>Modal clock: {clock ? "yes" : "no"}</span>
			{nav && (
				<button type="button" onClick={nav.onNext}>
					Next row
				</button>
			)}
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
				expect(screen.getByText("test-model")).toBeInTheDocument();
			});

			// Click on the row
			const row = screen.getByText("test-model").closest("tr");
			expect(row).not.toBeNull();
			await user.click(row as HTMLElement);
			await waitFor(() => {
				expect(screen.getByTestId("log-detail-modal")).toBeInTheDocument();
			});
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
				expect(screen.getByText("test-model")).toBeInTheDocument();
			});

			// Click on the row
			const row = screen.getByText("test-model").closest("tr");
			expect(row).not.toBeNull();
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
		it("shows the row's live state, not the snapshot it opened with", async () => {
			localStorage.setItem("requestLogsViewMode", "scroll");
			const pending = createMockLogEntry({
				id: "log-1",
				request_hash: "live1",
				state: "pending",
				status_code: 0,
				duration_ms: 0,
			});
			server.use(
				http.get("/api/logs/cursor", () =>
					HttpResponse.json({
						entries: [pending],
						total: 1,
						has_before: false,
						has_after: false,
					}),
				),
				http.get("/api/logs/log-1", () =>
					HttpResponse.json({
						...pending,
						state: "completed",
						status_code: 200,
					}),
				),
			);

			const { user } = renderWithProviders(<Logs />);
			await user.click(await screen.findByText("live1"));
			expect(
				await screen.findByText("Modal state: pending"),
			).toBeInTheDocument();

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

			expect(
				await screen.findByText("Modal state: completed"),
			).toBeInTheDocument();
			expect(screen.getByText("Modal clock: yes")).toBeInTheDocument();
		});

		it("keeps the newest state once a refetch pushes the open row off the page", async () => {
			const row = (state: "pending" | "completed") =>
				createMockLogEntry({
					id: "log-1",
					model_id: "live-model",
					state,
					status_code: state === "completed" ? 200 : 0,
					duration_ms: state === "completed" ? 6000 : 0,
				});
			const other = createMockLogEntry({
				id: "log-2",
				model_id: "other-model",
			});
			// Each phase is one refetch of the page the Logs view is showing.
			const pages = [
				[row("pending"), other],
				[row("completed"), other],
				[createMockLogEntry({ id: "log-3", model_id: "newer-model" })],
			];
			let phase = 0;
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(pages[Math.min(phase, pages.length - 1)]),
					),
				),
			);
			const refetch = async () => {
				phase++;
				await act(async () => {
					window.dispatchEvent(
						new CustomEvent("server-event", {
							detail: {
								type: "request.streaming",
								metadata: { request_id: "log-1", model_id: "live-model" },
							},
						}),
					);
				});
			};

			const { user } = renderWithProviders(<Logs />);
			const cell = await screen.findByText("live-model");
			await user.click(cell.closest("tr") as HTMLElement);
			expect(
				await screen.findByText("Modal state: pending"),
			).toBeInTheDocument();

			await refetch();
			expect(
				await screen.findByText("Modal state: completed"),
			).toBeInTheDocument();

			await refetch();
			await screen.findByText("newer-model");
			expect(screen.getByText("Modal state: completed")).toBeInTheDocument();
			expect(
				screen.queryByText("Modal state: pending"),
			).not.toBeInTheDocument();
		});

		// Paginate mode: each refetch serves the next page in `pages`, and the
		// open row ("live-model") starts pending.
		const openAndRefetch = async (pages: MockLogEntry[][]) => {
			let phase = 0;
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json(
						createMockLogs(pages[Math.min(phase, pages.length - 1)]),
					),
				),
			);
			const { user } = renderWithProviders(<Logs />);
			const cell = await screen.findByText("live-model");
			await user.click(cell.closest("tr") as HTMLElement);
			expect(
				await screen.findByText("Modal state: pending"),
			).toBeInTheDocument();
			return async () => {
				phase++;
				await act(async () => {
					window.dispatchEvent(
						new CustomEvent("server-event", {
							detail: {
								type: "request.streaming",
								metadata: { request_id: "log-1", model_id: "live-model" },
							},
						}),
					);
				});
			};
		};
		const liveRow = (state: "pending" | "completed") =>
			createMockLogEntry({
				id: "log-1",
				model_id: "live-model",
				state,
				status_code: state === "completed" ? 200 : 0,
				duration_ms: state === "completed" ? 6000 : 0,
			});

		it("ignores a page that still carries an older copy of the open row", async () => {
			const refetch = await openAndRefetch([
				[liveRow("pending")],
				[liveRow("completed")],
				[
					liveRow("pending"),
					createMockLogEntry({ id: "log-4", model_id: "late-page" }),
				],
			]);

			await refetch();
			expect(
				await screen.findByText("Modal state: completed"),
			).toBeInTheDocument();

			await refetch();
			await screen.findByText("late-page");
			expect(screen.getByText("Modal state: completed")).toBeInTheDocument();
		});

		it("stops the live clock once a still-running row leaves the page", async () => {
			const refetch = await openAndRefetch([
				[liveRow("pending")],
				[createMockLogEntry({ id: "log-3", model_id: "newer-model" })],
			]);
			expect(screen.getByText("Modal clock: yes")).toBeInTheDocument();

			await refetch();
			await screen.findByText("newer-model");
			expect(screen.getByText("Modal state: pending")).toBeInTheDocument();
			expect(screen.getByText("Modal clock: no")).toBeInTheDocument();
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
			expect(liveBadge).not.toBeNull();
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
									model_id: `hash${i}`,
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

			// Pagination now renders as single text node via i18n:
			// "1 to 20 of 50 entries" (multi-page) or "1–20 of 50 entries" (single-page range)
			expect(
				screen.getByText("1 to 20 of 50 entries", { exact: true }),
			).toBeInTheDocument();
			// Page navigation buttons use i18n translations
			expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
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
									model_id: `hash${i}`,
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

			// Click Next button to go to page 2
			await user.click(screen.getByRole("button", { name: "Next" }));

			// Should navigate to page 2 - pagination shows "21 to 40 of 50 entries"
			await waitFor(() => {
				expect(
					screen.getByText("21 to 40 of 50 entries", { exact: true }),
				).toBeInTheDocument();
			});
		});
	});
});
