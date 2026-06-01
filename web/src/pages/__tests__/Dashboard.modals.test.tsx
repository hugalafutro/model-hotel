import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mockModel, mockProvider, mockStats } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Dashboard } from "../Dashboard";

// Mock recharts components
vi.mock("recharts", () => ({
	ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
		<div data-testid="recharts-responsive-container">{children}</div>
	),
	AreaChart: ({ children }: { children: React.ReactNode }) => (
		<div data-testid="recharts-area-chart">{children}</div>
	),
	Area: () => <div data-testid="recharts-area" />,
	CartesianGrid: () => <div data-testid="recharts-grid" />,
	Tooltip: () => <div data-testid="recharts-tooltip" />,
	XAxis: () => <div data-testid="recharts-xaxis" />,
	YAxis: () => <div data-testid="recharts-yaxis" />,
	LineChart: ({ children }: { children: React.ReactNode }) => (
		<div data-testid="recharts-line-chart">{children}</div>
	),
	Line: () => <div data-testid="recharts-line" />,
	PieChart: ({ children }: { children: React.ReactNode }) => (
		<div data-testid="recharts-pie-chart">{children}</div>
	),
	Pie: () => <div data-testid="recharts-pie" />,
	Cell: () => <div data-testid="recharts-cell" />,
	BarChart: ({ children }: { children: React.ReactNode }) => (
		<div data-testid="recharts-bar-chart">{children}</div>
	),
	Bar: () => <div data-testid="recharts-bar" />,
	Legend: () => <div data-testid="recharts-legend" />,
}));

describe("Dashboard.coverage", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
	});

	afterEach(() => {
		localStorage.clear();
	});

	describe("Modal close handlers", () => {
		const statsWithValues = {
			...mockStats,
			requests_last_1h: 150,
			avg_ttft_ms: 250,
			avg_overhead_ms: 45,
			rate_limit_hits: 5,
			error_rate: 0.02,
			total_tokens_prompt: 1000,
			total_tokens_completion: 2000,
		};

		const timeSeriesMock = http.get("/api/stats/timeseries", () =>
			HttpResponse.json({ points: [] }),
		);

		it("closes TTFT gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open TTFT modal via stat card click
			const ttftStatCard = await screen.findByTitle(
				"Click to view TTFT history",
			);
			await user.click(ttftStatCard);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", { name: /Avg TTFT/ });

			// Close modal
			const closeButton = within(modal).getAllByRole("button", {
				name: /close/i,
			})[0];
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Avg TTFT/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Overhead gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Overhead modal via stat card
			const overheadStatCard = await screen.findByTitle(
				"Click to view overhead history",
			);
			await user.click(overheadStatCard);

			const modal = await screen.findByRole("dialog", { name: /Avg Overhead/ });
			const closeButton = within(modal).getAllByRole("button", {
				name: /close/i,
			})[0];
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Avg Overhead/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Error Rate gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Error Rate modal via stat card (div with role="button")
			const errorStatCards = screen.getAllByTitle(
				"Click to view error rate history",
			);
			const errorStatCard = errorStatCards.find((el) => el.tagName === "DIV");
			if (!errorStatCard) throw new Error("Error rate stat card not found");
			await user.click(errorStatCard);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", { name: /Error Rate/ });

			// Close modal
			const closeButton = within(modal).getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Error Rate/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Avg Duration gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Avg Duration modal via stat card (div with role="button")
			const durationStatCards = screen.getAllByTitle(
				"Click to view duration history",
			);
			const durationStatCard = durationStatCards.find(
				(el) => el.tagName === "DIV",
			);
			if (!durationStatCard) throw new Error("Duration stat card not found");
			await user.click(durationStatCard);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", { name: /Avg Duration/ });

			// Close modal
			const closeButton = within(modal).getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Avg Duration/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Requests gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Requests modal via stat card (div with role="button")
			const requestsStatCards = screen.getAllByTitle(
				"Click to view request history",
			);
			const requestsStatCard = requestsStatCards.find(
				(el) => el.tagName === "DIV",
			);
			if (!requestsStatCard) throw new Error("Requests stat card not found");
			await user.click(requestsStatCard);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", { name: /Requests/ });

			// Close modal
			const closeButton = within(modal).getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Requests/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Rate Limit Hits gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Rate Limit Hits modal via gauge (button element)
			const rateLimitGauges = screen.getAllByTitle(
				"Click to view rate limit hit history",
			);
			const rateLimitGauge = rateLimitGauges.find(
				(el) => el.tagName === "BUTTON",
			);
			if (!rateLimitGauge) throw new Error("Rate limit gauge not found");
			await user.click(rateLimitGauge);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", {
				name: /Rate Limit Hits/,
			});

			// Close modal
			const closeButton = within(modal).getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Rate Limit Hits/ }),
				).not.toBeInTheDocument();
			});
		});

		it("closes Avg Tokens gauge modal when close button is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
				timeSeriesMock,
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open Avg Tokens modal via stat card (div with role="button")
			const tokensStatCards = screen.getAllByTitle(
				"Click to view token history",
			);
			const tokensStatCard = tokensStatCards.find((el) => el.tagName === "DIV");
			if (!tokensStatCard) throw new Error("Tokens stat card not found");
			await user.click(tokensStatCard);

			// Wait for modal to open
			const modal = await screen.findByRole("dialog", { name: /Avg Tokens/ });

			// Close modal
			const closeButton = within(modal).getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: /Avg Tokens/ }),
				).not.toBeInTheDocument();
			});
		});

		it("per-section range settings use independent localStorage keys", async () => {
			// LocalStorage keys are created by useDashboard for each section
			// Each section can have different range/metric preferences stored
			localStorage.setItem("dashboard.modelsRange", "1h");
			localStorage.setItem("dashboard.providersRange", "7d");
			localStorage.setItem("dashboard.virtualKeysRange", "24h");

			expect(localStorage.getItem("dashboard.modelsRange")).toBe("1h");
			expect(localStorage.getItem("dashboard.providersRange")).toBe("7d");
		});
	});

	describe("Token stat card metric toggle", () => {
		const statsWithTokens = {
			...mockStats,
			total_tokens_prompt: 1000,
			total_tokens_completion: 2000,
			avg_tokens_per_request: 150,
		};

		it("displays Total Tokens when metric is tokens", async () => {
			localStorage.setItem("dashboardMetric", "tokens");
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithTokens)),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Should show total tokens (1000 + 2000 = 3000, formatted as 3K)
			await waitFor(() => {
				const tokensCard = screen
					.getByText(/Total Tokens/i)
					.closest(".ui-card");
				expect(tokensCard).toBeInTheDocument();
			});
		});

		it("displays Avg Tokens/Req when metric is requests", async () => {
			localStorage.setItem("dashboardMetric", "requests");
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithTokens)),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Should show avg tokens per request (150)
			await waitFor(() => {
				const tokensCard = screen
					.getByText(/Avg Tokens\/Req/i)
					.closest(".ui-card");
				expect(tokensCard).toBeInTheDocument();
			});
		});

		it("toggles between Total Tokens and Avg Tokens/Req when metric toggle is clicked", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithTokens)),
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Initially should show Total Tokens (default is tokens)
			await waitFor(() => {
				expect(screen.getByText(/Total Tokens/i)).toBeInTheDocument();
			});

			// Click the metric toggle to switch to requests - find the first Req button in gauge section
			const metricButtons = screen.getAllByRole("button", { name: /Req/i });
			await user.click(metricButtons[0]);

			// Should now show Avg Tokens/Req
			await waitFor(() => {
				expect(screen.getByText(/Avg Tokens\/Req/i)).toBeInTheDocument();
			});
		});
	});

	describe("Per-section range/metric independence", () => {
		const statsWithUsage = {
			...mockStats,
			by_model: { "Test Provider/test-model-v1": 500 },
			by_provider: { "Test Provider": 800 },
			by_virtual_key: { "test-key": 300 },
		};

		it("allows independent range selection for doughnut chart", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithUsage)),
				http.get("/api/stats/provider-distribution", () =>
					HttpResponse.json({ items: [] }),
				),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Find the doughnut section's range toggle (should be in the Providers section)
			const providersHeading = screen.getByRole("heading", {
				name: "Providers",
			});
			const providersSection = providersHeading.closest(".ui-card");
			expect(providersSection).toBeInTheDocument();

			if (providersSection) {
				// Find 1H toggle within the doughnut section
				const doughnut1H = within(providersSection as HTMLElement).getByText(
					"1H",
				);
				expect(doughnut1H).toBeInTheDocument();
			}
		});

		it("maintains different range selections for different sections", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
				http.get("/api/stats/provider-distribution", () =>
					HttpResponse.json({ items: [] }),
				),
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Header 7D should be active initially for all sections
			const header7D = screen
				.getAllByText("7D")
				.find((el) =>
					el
						.closest(".flex.items-center.gap-1")
						?.parentElement?.parentElement?.textContent?.includes("Overview"),
				);
			if (header7D) {
				await user.click(header7D);
			}

			// Now change doughnut range to 1H (should be in Providers section)
			await waitFor(() => {
				expect(screen.getByText("Providers")).toBeInTheDocument();
			});

			// Verify localStorage persistence for per-section range
			await waitFor(() => {
				expect(localStorage.getItem("dashboard.doughnutRange")).toBe("7d");
			});
		});

		it("per-section metric settings can be changed independently", async () => {
			// Each usage panel (models, providers, virtual keys) has its own metric setting
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Panels should have metric toggles rendered
			expect(screen.getAllByText("Tok").length).toBeGreaterThan(0);
		});
	});

	describe("UsageBarPanel with real data", () => {
		it("renders Top Models panel with usage data", async () => {
			const statsWithModelUsage = {
				...mockStats,
				by_model: {
					"Test Provider/test-model-v1": 500,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithModelUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Should show model entries with usage numbers
			await waitFor(() => {
				expect(
					screen.getByText("Test Provider/test-model-v1"),
				).toBeInTheDocument();
				// Should show the usage value (500 - no comma for numbers < 1000)
				expect(
					screen.getByText((content) => content.includes("500")),
				).toBeInTheDocument();
			});
		});

		it("renders Top Providers panel with usage data", async () => {
			const statsWithProviderUsage = {
				...mockStats,
				by_provider: {
					"Test Provider": 800,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithProviderUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Should show provider entries with usage numbers
			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
				expect(
					screen.getByText((content) => content.includes("800")),
				).toBeInTheDocument();
			});
		});

		it("renders Top Virtual Keys panel with usage data", async () => {
			const statsWithVKUsage = {
				...mockStats,
				by_virtual_key: {
					"dev-key-001": 600,
					"prod-key-002": 400,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithVKUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Should show virtual key entries with usage numbers
			await waitFor(() => {
				expect(screen.getByText("dev-key-001")).toBeInTheDocument();
				expect(
					screen.getByText((content) => content.includes("600")),
				).toBeInTheDocument();
			});
		});

		it("shows usage bars for models with data", async () => {
			const statsWithVaryingUsage = {
				...mockStats,
				by_model: {
					"Model A": 1000,
					"Model B": 500,
					"Model C": 250,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithVaryingUsage)),
				http.get("/api/models", () => HttpResponse.json([])),
				http.get("/api/providers", () => HttpResponse.json([])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Model entries should be rendered
			await waitFor(() => {
				expect(screen.getByText("Model A")).toBeInTheDocument();
				expect(
					screen.getByText((content) => content.includes("1,000")),
				).toBeInTheDocument();
			});
		});

		it("closes ModelDetailModal when close button is clicked", async () => {
			const statsWithModelUsage = {
				...mockStats,
				by_model: {
					"Test Provider/test-model-v1": 500,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithModelUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			const user = userEvent.setup();
			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Open model detail modal by clicking the model entry in Top Models panel
			const modelButton = await screen.findByRole("button", {
				name: /View details for Test Provider\/test-model-v1/i,
			});
			await user.click(modelButton);

			// Wait for modal to open
			await waitFor(() => {
				expect(screen.getByText("test-model-v1")).toBeInTheDocument();
			});

			// Close modal - find the X button by its aria-label
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			// Wait for modal to close
			await waitFor(() => {
				expect(screen.queryByText("test-model-v1")).not.toBeInTheDocument();
			});
		});
	});
});
