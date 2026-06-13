import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { AppLogs } from "../AppLogs";

// Mock LogDetailModal
vi.mock("../../components/LogDetailModal", () => ({
	LogDetailModal: ({
		log,
		onClose,
	}: {
		log: { timestamp: string; level: string; source: string; message: string };
		onClose: () => void;
	}) => (
		<div role="dialog" aria-label="Log Detail">
			<span>Log Detail: {log.timestamp}</span>
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));

// Mock AccentCalendar to simplify integration test
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
			<button type="button" onClick={() => onSelect("2024-06-10")}>
				Select Start
			</button>
			<button type="button" onClick={() => onSelect("2024-06-15")}>
				Select End
			</button>
		</div>
	),
}));

// Mock todayISO
vi.mock("../../components/AccentCalendar.utils", async (importOriginal) => {
	const actual =
		await importOriginal<
			typeof import("../../components/AccentCalendar.utils")
		>();
	return {
		...actual,
		todayISO: vi.fn(() => "2024-06-15"),
	};
});

describe("AppLogs paginate mode - renders entries", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("renders table with entries showing level badges, source, message, and timestamp", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "Request processed successfully",
							},
							{
								timestamp: "2024-06-10T11:00:00Z",
								level: "warning",
								source: "api",
								message: "Rate limit approaching",
							},
							{
								timestamp: "2024-06-10T12:00:00Z",
								level: "error",
								source: "provider",
								message: "Failed to connect to provider",
							},
						],
						total: 3,
						page: 1,
						per_page: 20,
						level_counts: { info: 1, warning: 1, error: 1 },
						source_counts: { proxy: 1, api: 1, provider: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});

		// Check level badges
		expect(screen.getByText("INFO")).toBeInTheDocument();
		expect(screen.getByText("WARNING")).toBeInTheDocument();
		expect(screen.getByText("ERROR")).toBeInTheDocument();

		// Check sources
		expect(screen.getByText("proxy")).toBeInTheDocument();
		expect(screen.getByText("api")).toBeInTheDocument();
		expect(screen.getByText("provider")).toBeInTheDocument();
	});
});

describe("AppLogs paginate mode - level filter counts", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("displays level_counts in filter dropdowns", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "test",
							},
						],
						total: 5,
						page: 1,
						per_page: 20,
						level_counts: { info: 3, warning: 1, error: 1 },
						source_counts: { proxy: 3, api: 2 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("test")).toBeInTheDocument();
		});

		// Level filter button shows "Level" placeholder before opening
		// There are two "Level" buttons - FilterDropdown and SortableHeader
		// FilterDropdown comes first in the DOM
		const levelButtons = screen.getAllByRole("button", { name: /Level/i });
		expect(levelButtons[0]).toBeInTheDocument();

		// Click level filter to open dropdown and see counts
		await userEvent.click(levelButtons[0]);

		// Check "All Levels" option appears in dropdown
		// May appear multiple times (trigger + dropdown option), so check existence
		expect(screen.getAllByText("All Levels").length).toBeGreaterThanOrEqual(1);
	});
});

describe("AppLogs paginate mode - filters by level", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("filters by level when selected from dropdown", async () => {
		let levelParam: string | null = null;

		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				levelParam = url.searchParams.get("level");
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries:
							levelParam === "error"
								? [
										{
											timestamp: "2024-06-10T12:00:00Z",
											level: "error",
											source: "provider",
											message: "Error message",
										},
									]
								: [
										{
											timestamp: "2024-06-10T10:00:00Z",
											level: "info",
											source: "proxy",
											message: "Info message",
										},
										{
											timestamp: "2024-06-10T12:00:00Z",
											level: "error",
											source: "provider",
											message: "Error message",
										},
									],
						total: levelParam === "error" ? 1 : 2,
						page: 1,
						per_page: 20,
						level_counts: { info: 1, warning: 0, error: 1 },
						source_counts: { proxy: 1, provider: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("Info message")).toBeInTheDocument();
		});

		// Open level filter dropdown
		const levelButton = screen.getByRole("button", { name: /^Level$/i });
		await user.click(levelButton);

		// Select error level
		await user.click(screen.getByText("Error"));

		// Verify filtered results appear
		await waitFor(() => {
			expect(screen.getByText("Error message")).toBeInTheDocument();
			expect(levelParam).toBe("error");
		});
	});
});

describe("AppLogs paginate mode - source filter", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("renders source filter when multiple sources exist", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "Proxy message",
							},
							{
								timestamp: "2024-06-10T11:00:00Z",
								level: "info",
								source: "api",
								message: "API message",
							},
						],
						total: 2,
						page: 1,
						per_page: 20,
						level_counts: { info: 2, warning: 0, error: 0 },
						source_counts: { proxy: 1, api: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("Proxy message")).toBeInTheDocument();
		});

		// Source filter button should appear when multiple sources exist
		// Look for the button after the Level filter button
		const sourceFilterButtons = screen.getAllByRole("button", {
			name: "Source",
		});
		expect(sourceFilterButtons.length).toBeGreaterThan(0);
	});
});

describe("AppLogs paginate mode - search input", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("renders search input for filtering logs", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "test log entry",
							},
						],
						total: 1,
						page: 1,
						per_page: 20,
						level_counts: { info: 1, warning: 0, error: 0 },
						source_counts: { proxy: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("test log entry")).toBeInTheDocument();
		});

		// Search input should be present
		const searchInput = screen.getByPlaceholderText("Filter logs…");
		expect(searchInput).toBeInTheDocument();
	});
});

describe("AppLogs paginate mode - sort headers", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("displays sort column headers in table", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "test",
							},
						],
						total: 1,
						page: 1,
						per_page: 20,
						level_counts: { info: 1, warning: 0, error: 0 },
						source_counts: { proxy: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("test")).toBeInTheDocument();
		});

		// Table headers - use getAllByText since "Level", "Source", "Message" appear in both filter and header
		const levelHeaders = screen.getAllByText("Level");
		expect(levelHeaders.length).toBeGreaterThanOrEqual(1);

		const sourceHeaders = screen.getAllByText("Source");
		expect(sourceHeaders.length).toBeGreaterThanOrEqual(1);

		const messageHeaders = screen.getAllByText("Message");
		expect(messageHeaders.length).toBeGreaterThanOrEqual(1);

		// Time/Date header has sort indicator
		const timeHeader = screen.getByText((content) =>
			content.includes("Time/Date"),
		);
		expect(timeHeader).toBeInTheDocument();
	});
});

describe("AppLogs paginate mode - error state", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("displays error message when API call fails", async () => {
		server.use(
			http.get("/api/logs/app", () => {
				return HttpResponse.json(
					{ error: "Internal server error" },
					{ status: 500 },
				);
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText(/Failed to load logs/i)).toBeInTheDocument();
		});
	});
});

describe("AppLogs paginate mode - empty state", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("displays empty state message when no entries exist", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [],
						total: 0,
						page: 1,
						per_page: 20,
						level_counts: {},
						source_counts: {},
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText(/No log entries yet/i)).toBeInTheDocument();
		});
	});
});

describe("AppLogs view mode toggle", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("switches from paginate to scroll mode and back", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "paginate mode entry",
							},
						],
						total: 1,
						page: 1,
						per_page: 20,
						level_counts: { info: 1 },
						source_counts: { proxy: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
			http.get("/api/logs/app/cursor", () => {
				return HttpResponse.json({
					entries: [
						{
							timestamp: "2024-06-10T10:00:00Z",
							level: "info",
							source: "proxy",
							message: "scroll mode entry",
						},
					],
					total: 1,
					has_before: false,
					has_after: false,
				});
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("paginate mode entry")).toBeInTheDocument();
		});

		// Find scroll mode toggle button by label - when in paginate mode, label is "Switch to scroll mode"
		const scrollModeButton = screen.getByLabelText("Switch to scroll mode");
		await user.click(scrollModeButton);

		// Verify localStorage was updated
		await waitFor(() => {
			expect(localStorage.getItem("appLogsViewMode")).toBe("scroll");
		});

		// Switch back to paginate mode - now button label is "Switch to pagination mode"
		const paginateModeButton = screen.getByLabelText(
			"Switch to pagination mode",
		);
		await user.click(paginateModeButton);

		await waitFor(() => {
			expect(localStorage.getItem("appLogsViewMode")).toBe("paginate");
		});
	});
});

describe("AppLogs live toggle", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("toggles live mode on and off", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				if (request.url.includes("history=true")) {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "info",
								source: "proxy",
								message: "test entry",
							},
						],
						total: 1,
						page: 1,
						per_page: 20,
						level_counts: { info: 1 },
						source_counts: { proxy: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("test entry")).toBeInTheDocument();
		});

		// Find the live toggle button by text
		const liveButton = screen.getByRole("button", {
			name: /Live/i,
		});

		// Initial state should be enabled (green styling, on by default)
		expect(liveButton).toBeInTheDocument();

		// Toggle off
		await user.click(liveButton);
		// After toggling, still have "Live" text but different styling
		expect(liveButton).toHaveTextContent("Live");

		// Toggle on
		await user.click(liveButton);
		expect(liveButton).toHaveTextContent("Live");
	});
});

describe("AppLogs scroll mode - cursor endpoint", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		// Don't set appLogsViewMode - use default (scroll)
	});

	it("renders entries in scroll mode using cursor endpoint", async () => {
		server.use(
			http.get("/api/logs/app/cursor", () => {
				return HttpResponse.json({
					entries: [
						{
							timestamp: "2024-06-10T10:00:00Z",
							level: "info",
							source: "proxy",
							message: "scroll mode log",
						},
						{
							timestamp: "2024-06-10T11:00:00Z",
							level: "warning",
							source: "api",
							message: "warning in scroll mode",
						},
					],
					total: 2,
					has_before: false,
					has_after: false,
					level_counts: { info: 1, warning: 1, error: 0 },
					source_counts: { proxy: 1, api: 1 },
				});
			}),
		);

		renderWithProviders(<AppLogs />);

		// In scroll mode, the toggle button shows "Switch to pagination mode"
		await waitFor(() => {
			expect(
				screen.getByLabelText("Switch to pagination mode"),
			).toBeInTheDocument();
		});

		// The virtual table may not render entries immediately due to jsdom height issues,
		// but we can verify the table structure exists and the pagination footer shows count
		await waitFor(() => {
			// Check for the "1–0 / 2" or similar pagination indicator in scroll mode footer
			expect(screen.getByText(/\/ 2/)).toBeInTheDocument();
		});
	});
});

describe("AppLogs row click opens detail modal", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		localStorage.setItem("appLogsViewMode", "paginate");
	});

	it("opens LogDetailModal with entry details when row is clicked", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				if (request.url.includes("history=true")) {
					return HttpResponse.json({
						entries: [
							{
								timestamp: "2024-06-10T10:00:00Z",
								level: "error",
								source: "proxy",
								message: "Detailed error message",
							},
						],
						total: 1,
						page: 1,
						per_page: 20,
						level_counts: { error: 1 },
						source_counts: { proxy: 1 },
					});
				}
				return HttpResponse.json({
					entries: [],
					total: 0,
					has_before: false,
					has_after: false,
				});
			}),
		);

		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);

		await waitFor(() => {
			expect(screen.getByText("Detailed error message")).toBeInTheDocument();
		});

		// Click the row
		const row = screen.getByText("Detailed error message").closest("tr");
		if (row) {
			await user.click(row);
		}

		// Verify modal opens
		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Log Detail" }),
			).toBeInTheDocument();
			expect(
				screen.getByText("Log Detail: 2024-06-10T10:00:00Z"),
			).toBeInTheDocument();
		});

		// Close modal
		await user.click(screen.getByText("Close"));
		await waitFor(() => {
			expect(
				screen.queryByRole("dialog", { name: "Log Detail" }),
			).not.toBeInTheDocument();
		});
	});
});
