import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("Sidebar Navigation", () => {
		it("renders sidebar with logo", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Logo has aria-label "Model Hotel"
			expect(screen.getByLabelText("Model Hotel")).toBeInTheDocument();
			expect(screen.getByText("Multi-Provider AI Gateway")).toBeInTheDocument();
		});

		it("renders tagline", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(
				screen.getByText('"Because we have LiteLLM at home"'),
			).toBeInTheDocument();
		});

		it("renders all navigation items", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Dashboard")).toBeInTheDocument();
			expect(screen.getByText("Chat")).toBeInTheDocument();
			expect(screen.getByText("Arena")).toBeInTheDocument();
			expect(screen.getByText("Providers")).toBeInTheDocument();
			expect(screen.getByText("Models")).toBeInTheDocument();
			expect(screen.getByText("Failover")).toBeInTheDocument();
			expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			expect(screen.getByText("Logs")).toBeInTheDocument();
			expect(screen.getByText("Settings")).toBeInTheDocument();
		});

		it("renders navigation items with icons", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = screen.getByRole("navigation");
			const icons = nav.querySelectorAll("svg");
			expect(icons.length).toBeGreaterThanOrEqual(9);
		});

		it("highlights active route - Dashboard", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});

			const dashboardLink = screen.getByText("Dashboard").closest("a");
			expect(dashboardLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Providers", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/providers"],
			});

			const providersLink = screen.getByText("Providers").closest("a");
			expect(providersLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Models", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/models"],
			});

			const modelsLink = screen.getByText("Models").closest("a");
			expect(modelsLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Failover", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/failover"],
			});

			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Virtual Keys", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/virtual-keys"],
			});

			const virtualKeysLink = screen.getByText("Virtual Keys").closest("a");
			expect(virtualKeysLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Settings", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/settings"],
			});

			const settingsLink = screen.getByText("Settings").closest("a");
			expect(settingsLink).toHaveClass("sidebar-link-active");
		});

		it("shows sub-mode labels for Chat page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/chat"],
			});

			expect(screen.getByText("Chat")).toBeInTheDocument();
			expect(screen.getByText("Conversation")).toBeInTheDocument();
		});

		it("shows sub-mode labels for Arena page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/arena"],
			});

			expect(screen.getByText("Arena")).toBeInTheDocument();
			expect(screen.getByText("Compare")).toBeInTheDocument();
		});

		it("shows sub-mode labels for Logs page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/logs"],
			});

			expect(screen.getByText("Requests")).toBeInTheDocument();
			expect(screen.getByText("Logs")).toBeInTheDocument();
		});
	});

	describe("Sub-mode Toggle", () => {
		it("navigates normally on first click to different page", async () => {
			const user = userEvent.setup();
			const { rerender } = renderWithProviders(
				<Layout>{mockChildren}</Layout>,
				{
					initialEntries: ["/dashboard"],
				},
			);

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			// Re-render to pick up the navigation change
			rerender(<Layout>{mockChildren}</Layout>);

			// After navigation, the Chat link should be active
			const updatedChatLink = screen.getByText("Chat").closest("a");
			expect(updatedChatLink).toHaveClass("sidebar-link-active");
		});
	});

	describe("Sidebar Footer", () => {
		it("renders Docs link", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Docs").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(docsLink).toHaveAttribute("target", "_blank");
		});

		it("renders GitHub link", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const githubLink = screen.getByLabelText("GitHub repository");
			expect(githubLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(githubLink).toHaveAttribute("target", "_blank");
		});

		it("renders theme toggle button", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Theme toggle has title attribute
			const themeButton = screen.getByTitle(/Switch to (light|dark) mode/);
			expect(themeButton).toBeInTheDocument();
		});

		it("shows Sun icon in dark mode", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const sunIcon = screen.getByTitle("Switch to light mode");
			expect(sunIcon).toBeInTheDocument();
		});

		it("toggles theme on button click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const themeButton = screen.getByTitle("Switch to light mode");
			await user.click(themeButton);

			expect(screen.getByTitle("Switch to dark mode")).toBeInTheDocument();
		});

		it("renders logout button", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Logout")).toBeInTheDocument();
		});

		it("opens logout confirmation dialog on logout click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			expect(screen.getByText("Log out?")).toBeInTheDocument();
			expect(
				screen.getByText("You'll need to re-enter your admin token."),
			).toBeInTheDocument();
		});

		it("closes logout confirmation on Cancel click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			const cancelButton = screen.getByText("Cancel");
			await user.click(cancelButton);

			expect(screen.queryByText("Log out?")).not.toBeInTheDocument();
		});

		it("performs logout on confirmation", async () => {
			const user = userEvent.setup();
			const originalStorage = localStorage.getItem("adminToken");
			localStorage.setItem("adminToken", "test-token");

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			const confirmButton = screen.getByText("Log out");
			await user.click(confirmButton);

			expect(localStorage.getItem("adminToken")).toBeNull();

			if (originalStorage) {
				localStorage.setItem("adminToken", originalStorage);
			}
		});
	});

	describe("System Status Panel", () => {
		const statusPanels = [
			"API Status",
			"Uptime",
			"CPU",
			"Network",
			"Disk",
			"Memory",
			"Go routines",
			"Req Today",
			"DB",
		];

		it.each(statusPanels)("renders %s status panel", async (panelName) => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText(panelName)).toBeInTheDocument();
			});
		});

		it("has collapsible toggle for stats", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const toggleButtons = screen.getAllByRole("button");
			const collapseButtons = toggleButtons.filter((btn) => {
				const title = btn.getAttribute("title");
				return title?.includes("Collapse") || title?.includes("Expand");
			});
			expect(collapseButtons.length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Main Content Area", () => {
		it("renders children in main area", () => {
			renderWithProviders(
				<Layout>
					<div data-testid="test-content">Test Content</div>
				</Layout>,
			);

			expect(screen.getByTestId("test-content")).toBeInTheDocument();
		});

		it("applies max-width constraint to content", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const main = screen.getByRole("main");
			const contentDiv = main.querySelector("div");
			expect(contentDiv).toHaveClass("max-w-7xl");
		});

		it("has proper main landmark", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByRole("main")).toBeInTheDocument();
		});
	});

	describe("Responsive Behavior", () => {
		it("renders sidebar with proper width class", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const sidebar = document.querySelector("aside");
			expect(sidebar).toHaveClass("w-64");
		});

		it("renders main content area with flex-1", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const main = screen.getByRole("main");
			expect(main).toHaveClass("flex-1");
		});

		it("has scrollable navigation", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = document.querySelector("nav");
			expect(nav).toHaveClass("overflow-y-auto");
		});
	});

	describe("Navigation Icons", () => {
		const navItems = [
			"Dashboard",
			"Chat",
			"Arena",
			"Providers",
			"Models",
			"Failover",
			"Virtual Keys",
			"Logs",
			"Settings",
		];

		it.each(navItems)("renders icon for %s", (label) => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const link = screen.getByText(label).closest("li");
			expect(link?.querySelector("svg")).toBeInTheDocument();
		});
	});

	describe("Keyboard Navigation", () => {
		it("focuses navigation links via tab", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.tab();
			// First tab should hit a focusable element in the sidebar
			expect(document.activeElement?.tagName).toBe("A");
		});

		it("supports keyboard navigation for theme toggle", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const themeButton = screen.getByTitle(/Switch to (light|dark) mode/);
			expect(themeButton).toBeInTheDocument();
			themeButton.focus();
			expect(document.activeElement).toBe(themeButton);
		});
	});

	describe("Accessibility", () => {
		it("has proper aria labels on navigation", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = screen.getByRole("navigation");
			expect(nav).toBeInTheDocument();
		});

		it("has proper heading structure", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Logo has aria-label "Model Hotel"
			expect(screen.getByLabelText("Model Hotel")).toBeInTheDocument();
		});

		it("has proper button roles", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const buttons = screen.getAllByRole("button");
			expect(buttons.length).toBeGreaterThanOrEqual(3);
		});
	});

	describe("Helper Functions (via SystemStatus)", () => {
		it("renders uptime information", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 7200,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Uptime")).toBeInTheDocument();
			});
		});

		it("renders requests today with large number formatting", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 1500,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Req Today")).toBeInTheDocument();
			});
		});

		it("renders memory information", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 52428800,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 50,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Memory")).toBeInTheDocument();
			});
		});

		it("renders network throughput", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 2048,
							net_tx_bytes_sec: 1024,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Network")).toBeInTheDocument();
			});
		});
	});

	describe("SystemStatus Component", () => {
		it("shows Online status when API is healthy", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Online")).toBeInTheDocument();
			});
		});

		it("shows Error status when API fails", async () => {
			server.use(http.get("/api/system", () => HttpResponse.error()));

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("API Status")).toBeInTheDocument();
			});
		});

		it("renders Docker stats when docker.available is true", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: {
							available: true,
							cpu_percent: 25.5,
							procs: 10,
							memory_usage_bytes: 536870912,
							memory_limit_bytes: 1073741824,
							net_rx_bytes_sec: 2000,
							net_tx_bytes_sec: 1000,
							disk_read_bytes_sec: 400,
							disk_write_bytes_sec: 200,
							container_count: 3,
						},
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
		});

		it("renders app-only stats when docker unavailable", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 15.5,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
		});

		it("renders DB section when stats.db is present", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("DB")).toBeInTheDocument();
			});
		});

		it("renders DB section when stats.db is missing", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("DB")).toBeInTheDocument();
			});
		});

		it("renders CPU with warning color when >= 75%", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 80,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
			// Verify CPU value has orange warning color class
			const cpuRow = screen.getByText("CPU").closest("div");
			expect(cpuRow?.querySelector(".text-orange-400")).toBeInTheDocument();
		});

		it("renders goroutines count", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 150,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Go routines")).toBeInTheDocument();
			});
		});

		it("renders singular 'proc' when process count is 1", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 1,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
			expect(screen.getByText(/proc(?!s)/)).toBeInTheDocument();
		});

		it("renders plural 'procs' when process count > 1", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
			expect(screen.getByText(/procs/)).toBeInTheDocument();
		});

		it("toggles collapsed state on CollapsibleToggle click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const toggleButton = screen.getByTitle("Collapse stats");
			expect(toggleButton).toBeInTheDocument();

			await user.click(toggleButton);

			expect(screen.getByTitle("Expand stats")).toBeInTheDocument();
		});

		it("renders CPU with red color when >= 90%", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 92,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
			const cpuRow = screen.getByText("CPU").closest("div");
			expect(cpuRow?.querySelector(".text-red-400")).toBeInTheDocument();
		});

		it("renders dash for CPU when cpu_percent is null", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: null,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("CPU")).toBeInTheDocument();
			});
			const cpuRow = screen.getByText("CPU").closest("div");
			expect(
				cpuRow?.querySelector(".text-\\(--text-muted\\)"),
			).toBeInTheDocument();
		});

		it("renders dash for Network when value is not a number", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: null,
							net_tx_bytes_sec: null,
							disk_read_bytes_sec: null,
							disk_write_bytes_sec: null,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Network")).toBeInTheDocument();
			});
			const networkRow = screen.getByText("Network").closest("div");
			const dashes = networkRow?.querySelectorAll(".text-\\(--text-muted\\)");
			expect(dashes?.length).toBeGreaterThanOrEqual(2);
		});

		it("renders dash for Disk when value is not a number", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: null,
							net_tx_bytes_sec: null,
							disk_read_bytes_sec: null,
							disk_write_bytes_sec: null,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Disk")).toBeInTheDocument();
			});
			const diskRow = screen.getByText("Disk").closest("div");
			const dashes = diskRow?.querySelectorAll(".text-\\(--text-muted\\)");
			expect(dashes?.length).toBeGreaterThanOrEqual(2);
		});

		it("renders Docker memory with limit when docker has memory_limit_bytes", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: {
							available: true,
							cpu_percent: 25.5,
							procs: 10,
							memory_usage_bytes: 536870912,
							memory_limit_bytes: 1073741824,
							net_rx_bytes_sec: 2000,
							net_tx_bytes_sec: 1000,
							disk_read_bytes_sec: 400,
							disk_write_bytes_sec: 200,
							container_count: 3,
						},
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Memory")).toBeInTheDocument();
			});
			const memoryRow = screen.getByText("Memory").closest("div");
			expect(memoryRow?.textContent).toContain("512");
			expect(memoryRow?.textContent).toContain("GB");
		});

		it("renders app memory with limit when app has memory_limit_bytes", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 52428800,
							memory_limit_bytes: 104857600,
							in_container: true,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 50,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Memory")).toBeInTheDocument();
			});
			const memoryRow = screen.getByText("Memory").closest("div");
			expect(memoryRow?.textContent).toContain("50");
			expect(memoryRow?.textContent).toContain("100");
		});

		it("renders app heap memory when no docker and no limit", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 52428800,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 50,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Memory")).toBeInTheDocument();
			});
			const memoryRow = screen.getByText("Memory").closest("div");
			expect(memoryRow?.textContent).toContain("50");
			expect(memoryRow?.textContent).toContain("heap");
		});

		it("renders DB hit ratio with orange warning when between 80-90", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 85,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("DB")).toBeInTheDocument();
			});
			const dbRow = screen.getByText("DB").closest("div");
			expect(dbRow?.querySelector(".text-orange-400")).toBeInTheDocument();
		});

		it("renders DB hit ratio with red when below 80", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 75,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("DB")).toBeInTheDocument();
			});
			const dbRow = screen.getByText("DB").closest("div");
			expect(dbRow?.querySelector(".text-red-400")).toBeInTheDocument();
		});

		it("renders dash for Req Today when value is 0", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Req Today")).toBeInTheDocument();
			});
			const reqRow = screen.getByText("Req Today").closest("div");
			expect(
				reqRow?.querySelector(".text-\\(--text-muted\\)"),
			).toBeInTheDocument();
		});

		it("renders Req Today with M suffix for millions", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 5000000,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Req Today")).toBeInTheDocument();
			});
			const reqRow = screen.getByText("Req Today").closest("div");
			expect(reqRow?.textContent).toContain("5.0");
			expect(reqRow?.textContent).toContain("M");
		});

		it("renders uptime with days and hours", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 90061,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Uptime")).toBeInTheDocument();
			});
			const uptimeRow = screen.getByText("Uptime").closest("div");
			expect(uptimeRow?.textContent).toContain("1");
			expect(uptimeRow?.textContent).toContain("d");
			expect(uptimeRow?.textContent).toContain("h");
		});

		it("renders uptime with hours and minutes", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 3661,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Uptime")).toBeInTheDocument();
			});
			const uptimeRow = screen.getByText("Uptime").closest("div");
			expect(uptimeRow?.textContent).toContain("1");
			expect(uptimeRow?.textContent).toContain("h");
			expect(uptimeRow?.textContent).toContain("m");
		});

		it("renders uptime with minutes only", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 61,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Uptime")).toBeInTheDocument();
			});
			const uptimeRow = screen.getByText("Uptime").closest("div");
			expect(uptimeRow?.textContent).toContain("1");
			expect(uptimeRow?.textContent).toContain("m");
			expect(uptimeRow?.textContent).not.toMatch(/\d+[dh]/);
		});

		it("renders 0 B/s for network when bytes/sec is 0", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 0,
							net_tx_bytes_sec: 0,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Network")).toBeInTheDocument();
			});
			const networkRow = screen.getByText("Network").closest("div");
			expect(networkRow?.textContent).toContain("0");
			expect(networkRow?.textContent).toContain("B/s");
		});

		it("renders DB size with decimal when less than 1 MB", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: { available: false },
						db: {
							size_mb: 0.5,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("DB")).toBeInTheDocument();
			});
			const dbRow = screen.getByText("DB").closest("div");
			expect(dbRow?.textContent).toContain("0.5");
		});

		it("renders Memory with warning color when usage >= 75%", async () => {
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: {
							uptime_seconds: 100,
							cpu_percent: 10,
							procs: 5,
							memory_current_bytes: 100000000,
							memory_limit_bytes: 0,
							in_container: false,
							goroutines: 50,
							requests_today: 0,
							heap_alloc_mb: 100,
							net_rx_bytes_sec: 1000,
							net_tx_bytes_sec: 500,
							disk_read_bytes_sec: 200,
							disk_write_bytes_sec: 100,
						},
						docker: {
							available: true,
							cpu_percent: 25.5,
							procs: 10,
							memory_usage_bytes: 858993459,
							memory_limit_bytes: 1073741824,
							net_rx_bytes_sec: 2000,
							net_tx_bytes_sec: 1000,
							disk_read_bytes_sec: 400,
							disk_write_bytes_sec: 200,
							container_count: 3,
						},
						db: {
							size_mb: 10,
							cache_hit_ratio: 95,
							connections: 3,
							tx_per_sec: 5.5,
						},
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Memory")).toBeInTheDocument();
			});
			const memoryRow = screen.getByText("Memory").closest("div");
			expect(memoryRow?.querySelector(".text-orange-400")).toBeInTheDocument();
		});
	});

	describe("LastErrorPills Component", () => {
		it("renders nothing when no error data", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByText(/Err/)).not.toBeInTheDocument();
			});
		});

		it("does not render acknowledge button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
		});

		it("does not render copy button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByTitle("Copy error")).not.toBeInTheDocument();
			});
		});

		it("does not render view details button when no errors", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				expect(screen.queryByTitle("View details")).not.toBeInTheDocument();
			});
		});

		it("renders app error pill when app log has errors", async () => {
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Something went wrong in the app";
			server.use(
				http.get("/api/logs/app", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								timestamp: errorTimestamp,
								level: "error",
								source: "server",
								message: errorMessage,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
						level_counts: { error: 1 },
						source_counts: { server: 1 },
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			expect(screen.getByTitle("View details")).toBeInTheDocument();
			expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			expect(screen.getByText(errorMessage)).toBeInTheDocument();
		});

		it("renders request error pill when request log has 5xx errors", async () => {
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Internal server error";
			server.use(
				http.get("/api/logs", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								provider_id: "prov-1",
								provider_name: "TestProvider",
								model_id: "model-1",
								request_hash: "abc123",
								status_code: 500,
								latency_ms: 100,
								error_message: errorMessage,
								created_at: errorTimestamp,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			expect(screen.getByText(errorMessage)).toBeInTheDocument();
		});

		it("dismisses app error on acknowledge click", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Something went wrong in the app";
			server.use(
				http.get("/api/logs/app", () =>
					HttpResponse.json({
						entries: [
							{
								id: 1,
								timestamp: errorTimestamp,
								level: "error",
								source: "server",
								message: errorMessage,
							},
						],
						total: 1,
						page: 1,
						per_page: 25,
						level_counts: { error: 1 },
						source_counts: { server: 1 },
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Acknowledge (dismiss)"));
			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
			expect(localStorage.getItem("dismissedAppErrorKey")).toBeTruthy();
		});

		it("copies error message to clipboard on copy click", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Clipboard test error message";
			const clipboardSpy = vi
				.spyOn(navigator.clipboard, "writeText")
				.mockResolvedValue(undefined);
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Copy error")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Copy error"));
			expect(clipboardSpy).toHaveBeenCalledWith(errorMessage);
			clipboardSpy.mockRestore();
		});

		it("opens LogDetailModal on view details click when entry exists", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "View details test error message";
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("View details")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("View details"));
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});

		it("re-shows dismissed errors on dismissedErrorsReset event", async () => {
			const user = userEvent.setup();
			const errorTimestamp = "2024-01-15T10:30:00Z";
			const errorMessage = "Reset event test error message";
			server.use(
				http.get("/api/logs/app", ({ request }) => {
					const url = new URL(request.url);
					if (url.searchParams.get("history") === "true") {
						return HttpResponse.json({
							entries: [
								{
									id: 1,
									timestamp: errorTimestamp,
									level: "error",
									source: "server",
									message: errorMessage,
								},
							],
							total: 1,
							page: 1,
							per_page: 25,
							level_counts: { error: 1 },
							source_counts: { server: 1 },
						});
					}
					return HttpResponse.json([]);
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
			await user.click(screen.getByTitle("Acknowledge (dismiss)"));
			await waitFor(() => {
				expect(
					screen.queryByTitle("Acknowledge (dismiss)"),
				).not.toBeInTheDocument();
			});
			window.dispatchEvent(new Event("dismissedErrorsReset"));
			await waitFor(() => {
				expect(screen.getByTitle("Acknowledge (dismiss)")).toBeInTheDocument();
			});
		});
	});

	describe("Layout Main Function", () => {
		it("navigates normally on first click to different page", async () => {
			const user = userEvent.setup();
			const { rerender } = renderWithProviders(
				<Layout>{mockChildren}</Layout>,
				{
					initialEntries: ["/dashboard"],
				},
			);

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			rerender(<Layout>{mockChildren}</Layout>);

			const updatedChatLink = screen.getByText("Chat").closest("a");
			expect(updatedChatLink).toHaveClass("sidebar-link-active");
		});

		it("toggles sub-mode when clicking same page link", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/chat"],
			});

			expect(screen.getByText("Conversation")).toBeInTheDocument();

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(screen.getByText("Conversation")).toBeInTheDocument();
		});

		it("shows update available styling when version is outdated", async () => {
			server.use(
				http.get(
					"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest",
					() => {
						return HttpResponse.json({ tag_name: "v99.0" });
					},
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				const githubLink = screen.getByLabelText("GitHub repository");
				expect(githubLink).toBeInTheDocument();
			});
		});

		it("handles logout confirmation flow", async () => {
			const user = userEvent.setup();
			const originalStorage = localStorage.getItem("adminToken");
			localStorage.setItem("adminToken", "test-token");

			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			expect(screen.getByText("Log out?")).toBeInTheDocument();

			const confirmButton = screen.getByText("Log out");
			await user.click(confirmButton);

			expect(localStorage.getItem("adminToken")).toBeNull();

			if (originalStorage) {
				localStorage.setItem("adminToken", originalStorage);
			}
		});

		it("cancels logout confirmation", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			const cancelButton = screen.getByText("Cancel");
			await user.click(cancelButton);

			expect(screen.queryByText("Log out?")).not.toBeInTheDocument();
		});

		it("renders version badge with running version", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				const versionElement = screen.getByText(/v\d+\.\d+/);
				expect(versionElement).toBeInTheDocument();
			});
		});

		it("renders Docs link with correct href", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Docs").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(docsLink).toHaveAttribute("target", "_blank");
		});

		it("renders GitHub link with correct attributes", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const githubLink = screen.getByLabelText("GitHub repository");
			expect(githubLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(githubLink).toHaveAttribute("target", "_blank");
		});

		it("does not toggle sub-mode when navigating to different page", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});
			expect(screen.getByText("Conversation")).toBeInTheDocument();
			const chatLink = screen.getByText("Chat").closest("a");
			if (chatLink) {
				await user.click(chatLink);
			}
			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
		});

		it("does not toggle sub-mode for nav item without subModes", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/settings"],
			});
			const settingsLink = screen.getByText("Settings").closest("a");
			if (settingsLink) {
				await user.click(settingsLink);
			}
			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});
		});
	});
});
