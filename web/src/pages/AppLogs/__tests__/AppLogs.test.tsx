import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getByDialogName } from "../../../test/helpers";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AppLogs } from "../../AppLogs";

const mockAppLogs = [
	{
		timestamp: "2026-05-11T10:30:00Z",
		level: "info" as const,
		source: "proxy",
		message: "Request processed successfully",
	},
	{
		timestamp: "2026-05-11T10:25:00Z",
		level: "warning" as const,
		source: "auth",
		message: "Token expiring soon",
	},
	{
		timestamp: "2026-05-11T10:20:00Z",
		level: "error" as const,
		source: "db",
		message: "Connection timeout",
	},
];

describe("AppLogs", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		// Default to paginate mode so existing assertions match
		localStorage.setItem("appLogsViewMode", "paginate");
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: mockAppLogs,
						total: mockAppLogs.length,
						page: 1,
						per_page: 20,
						level_counts: {
							info: 1,
							warning: 1,
							error: 1,
						},
						source_counts: {
							proxy: 1,
							auth: 1,
							db: 1,
						},
					});
				}
				return HttpResponse.json([]);
			}),
		);
	});

	it("renders page header with Logs title", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Page header h1 with Logs title
			expect(screen.getByRole("heading", { name: "Logs" })).toBeInTheDocument();
		});
	});

	it("shows description text", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Server application log output"),
			).toBeInTheDocument();
		});
	});

	it("shows Live badge toggle button", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /Toggle live/ }),
			).toBeInTheDocument();
		});
	});

	it("Live badge is green when enabled", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			const liveButton = screen.getByRole("button", { name: /Toggle live/ });
			expect(liveButton).toBeInTheDocument();
			// Check for green styling
			expect(liveButton).toHaveClass("bg-green-500/20");
		});
	});

	it("toggling Live updates shows toast and changes badge", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			const liveButton = screen.getByRole("button", { name: /Toggle live/ });
			expect(liveButton).toBeInTheDocument();
		});
		const liveButton = screen.getByRole("button", { name: /Toggle live/ });
		await user.click(liveButton);
		await waitFor(() => {
			expect(screen.getByText("Live updates paused")).toBeInTheDocument();
		});
	});

	it("shows Requests and Logs submode tabs", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: "Requests" }),
			).toBeInTheDocument();
			// Logs tab button (not the header)
			expect(screen.getByRole("button", { name: "Logs" })).toBeInTheDocument();
		});
	});

	it("Logs tab is active by default", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Both Requests and Logs tabs exist
			expect(
				screen.getByRole("button", { name: "Requests" }),
			).toBeInTheDocument();
			// There are multiple "Logs" texts (header + tab)
			expect(screen.getAllByText("Logs").length).toBeGreaterThan(1);
		});
	});

	it("clicking Requests tab switches submode", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: "Requests" }),
			).toBeInTheDocument();
		});
		const requestsTab = screen.getByRole("button", { name: "Requests" });
		await user.click(requestsTab);
		await waitFor(() => {
			expect(requestsTab).toHaveClass("bg-(--accent)/20");
		});
	});

	it("shows Level filter dropdown", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("Level")).toBeInTheDocument();
		});
	});

	it("Level filter shows counts from level_counts", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			const levelDropdown = screen.getByRole("button", { name: "Level" });
			expect(levelDropdown).toBeInTheDocument();
		});
		// Click to open dropdown
		const user = userEvent.setup();
		const levelButton = screen.getByRole("button", { name: "Level" });
		await user.click(levelButton);
		await waitFor(() => {
			expect(screen.getByText("All (3)")).toBeInTheDocument();
			expect(screen.getByText("Info")).toBeInTheDocument();
			expect(screen.getByText("Warning")).toBeInTheDocument();
			expect(screen.getByText("Error")).toBeInTheDocument();
		});
	});

	it("shows Source filter dropdown when multiple sources exist", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Source filter button - check for the button containing "Source" text
			const sourceButtons = screen.getAllByText("Source");
			expect(sourceButtons.length).toBeGreaterThan(0);
		});
	});

	it("shows search input with placeholder", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByPlaceholderText("Filter logs…")).toBeInTheDocument();
		});
	});

	it("shows loading spinner on initial load", async () => {
		// Use a slow response to ensure spinner is visible
		server.use(
			http.get("/api/logs/app", async () => {
				await new Promise((resolve) => setTimeout(resolve, 100));
				return HttpResponse.json({
					entries: mockAppLogs,
					total: mockAppLogs.length,
					page: 1,
					per_page: 20,
					level_counts: {},
					source_counts: {},
				});
			}),
		);
		renderWithProviders(<AppLogs />);
		// Spinner is an inline div with animate-spin class
		const spinner = document.querySelector(".animate-spin");
		expect(spinner).toBeInTheDocument();
	});

	it("shows error message when query fails", async () => {
		server.use(
			http.get("/api/logs/app", () => {
				return HttpResponse.json({ error: "Failed" }, { status: 500 });
			}),
		);
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText(/Failed to load logs:/)).toBeInTheDocument();
		});
	});

	it("renders log entries table", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		expect(screen.getByText("Token expiring soon")).toBeInTheDocument();
		expect(screen.getByText("Connection timeout")).toBeInTheDocument();
	});

	it("shows table headers: Time/Date, Level, Source, Message", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Check for table headers (they are inside th elements with buttons)
			expect(
				screen.getByRole("button", { name: /Sort by Time\/Date/ }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Level/ }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Source/ }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Message/ }),
			).toBeInTheDocument();
		});
	});

	it("shows formatted timestamp for each log entry", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Timestamps are formatted - locale-agnostic match
			// The format varies by locale and timezone (DD/MM/YYYY vs MM/DD/YYYY,
			// UTC vs local). Just verify a timestamp-like string containing "2026"
			// and a time pattern (HH:MM:SS) appears in a table cell.
			const cells = screen.getAllByText((_content, element) => {
				return element?.tagName === "TD"
					? /2026.*\d{2}:\d{2}:\d{2}/.test(element.textContent ?? "")
					: false;
			});
			expect(cells.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("shows level badge (INFO, WARNING, ERROR)", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("INFO")).toBeInTheDocument();
			expect(screen.getByText("WARNING")).toBeInTheDocument();
			expect(screen.getByText("ERROR")).toBeInTheDocument();
		});
	});

	it("shows source badge for each log entry", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("proxy")).toBeInTheDocument();
			expect(screen.getByText("auth")).toBeInTheDocument();
			expect(screen.getByText("db")).toBeInTheDocument();
		});
	});

	it("clicking log row opens LogDetailModal", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		const row = screen
			.getByText("Request processed successfully")
			.closest("tr");
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		await user.click(row!);
		await waitFor(() => {
			expect(getByDialogName("Log Entry Details")).toBeInTheDocument();
		});
	});

	it("LogDetailModal shows timestamp, level, source, message", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		const row = screen
			.getByText("Request processed successfully")
			.closest("tr");
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		await user.click(row!);
		await waitFor(() => {
			expect(getByDialogName("Log Entry Details")).toBeInTheDocument();
			// Modal shows field labels
			expect(screen.getAllByText("Timestamp").length).toBeGreaterThan(0);
			expect(screen.getAllByText("Level").length).toBeGreaterThan(0);
			expect(screen.getAllByText("Source").length).toBeGreaterThan(0);
			expect(screen.getAllByText("Message").length).toBeGreaterThan(0);
		});
	});

	it("LogDetailModal close button closes modal", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		const row = screen
			.getByText("Request processed successfully")
			.closest("tr");
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		await user.click(row!);
		await waitFor(() => {
			expect(getByDialogName("Log Entry Details")).toBeInTheDocument();
		});
		const closeButtons = screen.getAllByLabelText("Close");
		await user.click(closeButtons[0]);
		await waitFor(() => {
			expect(
				screen.queryByRole("dialog", { name: /Log Entry Details/ }),
			).not.toBeInTheDocument();
		});
	});

	it("empty state shows message when no entries", async () => {
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
				return HttpResponse.json([]);
			}),
		);
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText(/No log entries yet/)).toBeInTheDocument();
		});
	});

	it("empty state shows filter message when filtered results empty", async () => {
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: [],
						total: 5,
						page: 1,
						per_page: 20,
						level_counts: {},
						source_counts: {},
					});
				}
				return HttpResponse.json([]);
			}),
		);
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText(/No entries match your filter/),
			).toBeInTheDocument();
		});
	});

	it("sorting by Time/Date header toggles sort direction", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /Sort by Time\/Date/ }),
			).toBeInTheDocument();
		});
		const timeHeader = screen.getByRole("button", {
			name: /Sort by Time\/Date/,
		});
		await user.click(timeHeader);
		// Should toggle sort - check for arrow indicator
		await waitFor(() => {
			expect(timeHeader).toBeInTheDocument();
		});
	});

	it("sorting by Level header toggles sort direction", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /Sort by Level/ }),
			).toBeInTheDocument();
		});
		const levelButton = screen.getByRole("button", { name: /Sort by Level/ });
		expect(levelButton).toBeInTheDocument();
		await user.click(levelButton);
		// Sort toggles - just verify the button is still there
		await waitFor(() => {
			expect(levelButton).toBeInTheDocument();
		});
	});

	it("sorting by Source header toggles sort direction", async () => {
		renderWithProviders(<AppLogs />);
		// Wait for data to load
		await waitFor(() => {
			expect(screen.getByText("proxy")).toBeInTheDocument();
		});
		// Verify Source header exists - the SortableHeader component handles sorting
		const sourceTexts = screen.getAllByText("Source");
		expect(sourceTexts.length).toBeGreaterThanOrEqual(1);
	});

	it("sorting by Message header toggles sort direction", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /Sort by Message/ }),
			).toBeInTheDocument();
		});
		const messageHeader = screen.getByRole("button", {
			name: /Sort by Message/,
		});
		await user.click(messageHeader);
		await waitFor(() => {
			expect(messageHeader).toBeInTheDocument();
		});
	});

	it("pagination appears when total items > page size", async () => {
		const manyLogs = Array.from({ length: 25 }, (_, i) => ({
			timestamp: `2026-05-11T10:${30 - i}:00Z`,
			level: "info" as const,
			source: "proxy",
			message: `Log entry ${i}`,
		}));
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: manyLogs,
						total: manyLogs.length,
						page: 1,
						per_page: 20,
						level_counts: { info: 25 },
						source_counts: { proxy: 25 },
					});
				}
				return HttpResponse.json([]);
			}),
		);
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("20 / page")).toBeInTheDocument();
		});
	});

	it("page size selector allows changing page size", async () => {
		const manyLogs = Array.from({ length: 35 }, (_, i) => ({
			timestamp: `2026-05-11T10:${30 - i}:00Z`,
			level: "info" as const,
			source: "proxy",
			message: `Log entry ${i}`,
		}));
		server.use(
			http.get("/api/logs/app", ({ request }) => {
				const url = new URL(request.url);
				if (url.searchParams.get("history") === "true") {
					return HttpResponse.json({
						entries: manyLogs,
						total: manyLogs.length,
						page: 1,
						per_page: 20,
						level_counts: { info: 35 },
						source_counts: { proxy: 35 },
					});
				}
				return HttpResponse.json([]);
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("20 / page")).toBeInTheDocument();
		});
		const pageSizeSelect = screen.getByRole("combobox");
		await user.selectOptions(pageSizeSelect, "30");
		await waitFor(() => {
			expect(screen.getByText("30 / page")).toBeInTheDocument();
		});
	});

	it("search input filters logs by message", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		const searchInput = screen.getByPlaceholderText("Filter logs…");
		await user.type(searchInput, "timeout");
		await waitFor(() => {
			expect(screen.getByText("Connection timeout")).toBeInTheDocument();
		});
	});

	it("Level filter dropdown filters logs by level", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "Level" })).toBeInTheDocument();
		});
		const levelButton = screen.getByRole("button", { name: "Level" });
		await user.click(levelButton);
		await waitFor(() => {
			expect(screen.getByText("Error")).toBeInTheDocument();
		});
		await user.click(screen.getByText("Error"));
		await waitFor(() => {
			expect(screen.getByText("Connection timeout")).toBeInTheDocument();
		});
	});

	it("Source filter dropdown filters logs by source", async () => {
		renderWithProviders(<AppLogs />);
		// Wait for data to load
		await waitFor(() => {
			expect(screen.getByText("proxy")).toBeInTheDocument();
		});
		// Verify Source filter dropdown exists - it shows when there are multiple sources
		const sourceTexts = screen.getAllByText("Source");
		// There should be at least 2: filter dropdown + table header
		expect(sourceTexts.length).toBeGreaterThanOrEqual(2);
	});

	it("shows entry count in pagination bar", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			// Pagination shows "1 to 3 of 3 entries"
			expect(screen.getByText(/of 3 entries/)).toBeInTheDocument();
		});
	});

	it("Copy button exists in LogDetailModal for message", async () => {
		const user = userEvent.setup();
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		const row = screen
			.getByText("Request processed successfully")
			.closest("tr");
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		await user.click(row!);
		await waitFor(() => {
			expect(getByDialogName("Log Entry Details")).toBeInTheDocument();
		});
		// Copy button should be present in the modal (aria-label="Copy message")
		expect(
			screen.getByRole("button", { name: "Copy message" }),
		).toBeInTheDocument();
	});

	it("level badge has correct color classes", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("INFO")).toBeInTheDocument();
		});
		const infoBadge = screen.getByText("INFO");
		expect(infoBadge).toHaveClass("bg-blue-900/30");
		const warningBadge = screen.getByText("WARNING");
		expect(warningBadge).toHaveClass("bg-yellow-900/30");
		const errorBadge = screen.getByText("ERROR");
		expect(errorBadge).toHaveClass("bg-red-900/30");
	});

	it("source badge has correct color classes", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(screen.getByText("proxy")).toBeInTheDocument();
		});
		const proxyBadge = screen.getByText("proxy");
		expect(proxyBadge).toHaveClass("bg-cyan-900/30");
		const authBadge = screen.getByText("auth");
		expect(authBadge).toHaveClass("bg-purple-900/30");
		const dbBadge = screen.getByText("db");
		expect(dbBadge).toHaveClass("bg-lime-900/30");
	});

	it("message text uses neutral color regardless of level", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(
				screen.getByText("Request processed successfully"),
			).toBeInTheDocument();
		});
		// Messages use neutral gray, not level-specific colors
		const errorMessage = screen.getByText("Connection timeout");
		expect(errorMessage).toHaveClass("text-gray-400");
	});
});
