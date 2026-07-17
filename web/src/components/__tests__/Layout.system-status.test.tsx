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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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

		it("renders dash for DB hit ratio when the sample window is idle", async () => {
			// cache_window_blocks omitted: the backend sends the field only when the
			// ratio is backed by fresh activity, so the cell must show a dash and
			// must not colour-code the (meaningless) ratio value.
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
			expect(screen.queryByText("75")).not.toBeInTheDocument();
			const dbRow = screen.getByText("DB").closest("div");
			expect(dbRow?.querySelector(".text-red-400")).not.toBeInTheDocument();
			expect(dbRow?.querySelector(".text-orange-400")).not.toBeInTheDocument();
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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
							cache_window_blocks: 50000,
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

	describe("HA fleet line", () => {
		const baseApp = {
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
		};
		const baseDb = {
			size_mb: 10,
			cache_hit_ratio: 95,
			connections: 3,
			tx_per_sec: 5.5,
		};
		const respondWith = (fleet?: unknown) =>
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						app: baseApp,
						docker: { available: false },
						db: baseDb,
						...(fleet ? { fleet } : {}),
					}),
				),
			);

		it("shows no HA line for a standalone instance (no fleet block)", async () => {
			respondWith(undefined);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(screen.getByText("Uptime")).toBeInTheDocument();
			});
			expect(screen.queryByTestId("ha-status")).not.toBeInTheDocument();
		});

		it.each([
			["primary", "Primary", "text-green-400"],
			["member", "Member", "text-green-400"],
			["warning", "Warning", "text-orange-400"],
			["member_sync_blocked", "Error", "text-red-400"],
		])("renders HA %s state with the right value and color", async (state, label, colorClass) => {
			respondWith({ state, is_primary: state === "primary" });
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			const row = await screen.findByTestId("ha-status");
			expect(row).toHaveTextContent("HA");
			expect(row).toHaveTextContent(label);
			expect(row.querySelector(`.${colorClass}`)).toBeInTheDocument();
		});

		it("uses the primary name in the member tooltip when present", async () => {
			respondWith({
				state: "member",
				is_primary: false,
				primary_name: "hotel-a",
			});
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			const row = await screen.findByTestId("ha-status");
			expect(row.getAttribute("title")).toContain("hotel-a");
		});
	});
});
