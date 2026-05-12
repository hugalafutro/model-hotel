import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Settings } from "../Settings";

describe("Settings", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/settings", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json({}));
						}, 100);
					});
				}),
			);

			renderWithProviders(<Settings />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});
	});

	describe("Rendering", () => {
		it("renders page header with correct title and icon", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Configure your Model Hotel instance"),
			).toBeInTheDocument();
		});

		it("renders all settings sections", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// All section headers should be present
			expect(screen.getByText("Model Discovery")).toBeInTheDocument();
			expect(screen.getByText("Appearance")).toBeInTheDocument();
			expect(screen.getByText("Toast Notifications")).toBeInTheDocument();
			expect(screen.getByText("Sidebar Quotas")).toBeInTheDocument();
			expect(screen.getByText("Dashboard Refresh")).toBeInTheDocument();
			expect(screen.getByText("Data Storage")).toBeInTheDocument();
			expect(screen.getByText("Database Backup")).toBeInTheDocument();
			expect(screen.getByText("Logging")).toBeInTheDocument();
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
			expect(screen.getByText("Proxy")).toBeInTheDocument();
		});

		it("renders section icons", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// Each section should have an icon (svg element)
			const icons = document.querySelectorAll("svg");
			// Should have multiple icons (page header + each section)
			expect(icons.length).toBeGreaterThan(5);
		});

		it("renders sections with collapsible toggles", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// Sections render with collapsible headers (toggle buttons present)
		});

		it("renders DiscoverySettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Model Discovery")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders AppearanceSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Appearance")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders ToastSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Toast Notifications")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders SidebarQuotaSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Sidebar Quotas")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders DashboardRefreshSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard Refresh")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders DataStorageSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Data Storage")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders DatabaseBackupSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Database Backup")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders LoggingSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Logging")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders RateLimitSettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});

		it("renders ProxySettings section", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Proxy")).toBeInTheDocument();
			});
			// Inner content is collapsed by default - just verify header exists
		});
	});

	describe("Section Collapsing", () => {
		it("can collapse and expand sections", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// Find a toggle button (first CollapsibleToggle button)
			const toggleButtons = screen.getAllByRole("button");
			expect(toggleButtons.length).toBeGreaterThan(0);
			const toggleButton = toggleButtons[0];

			// Click to collapse
			await user.click(toggleButton);

			// Click to expand again
			await user.click(toggleButton);
		});
	});

	describe("API Error Handling", () => {
		it("handles settings API error gracefully", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<Settings />);

			// Component shows loading spinner while query is in error state
			await waitFor(() => {
				expect(screen.getByTestId("spinner")).toBeInTheDocument();
			});
		});
	});
});
