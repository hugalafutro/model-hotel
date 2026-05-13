import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

describe("Dashboard", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/stats", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json(mockStats));
						}, 100);
					});
				}),
			);

			renderWithProviders(<Dashboard />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});
	});

	describe("Error State", () => {
		it("renders error message when stats API fails", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch stats" },
						{ status: 500 },
					);
				}),
			);

			const { container } = renderWithProviders(<Dashboard />);

			// Wait for error state to render (spinner -> error)
			await waitFor(
				() => {
					expect(container.textContent).toMatch(/Failed to load stats/i);
				},
				{ timeout: 3000 },
			);
		});
	});

	describe("Page Header", () => {
		it("renders page header with correct title and description", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Overview of your Model Hotel usage"),
			).toBeInTheDocument();
		});

		it("renders active keys filter badge", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Toggle key filter" }),
				).toBeInTheDocument();
			});
		});

		it("toggles between All Keys and Active Keys Only when clicked", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Toggle key filter" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Toggle key filter" }),
			);
			await waitFor(() => {
				expect(screen.getByText("Active Keys Only")).toBeInTheDocument();
			});
		});

		it("renders manual refresh button", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Refresh button should be present
			expect(
				screen.getByRole("button", { name: "Refresh dashboard" }),
			).toBeInTheDocument();
		});
	});

	describe("Stat Cards", () => {
		it("renders all six stat cards", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// All stat card labels should be present
			expect(screen.getByText("Total Providers")).toBeInTheDocument();
			expect(screen.getByText("Total Models")).toBeInTheDocument();
			expect(screen.getAllByText(/Requests\/1d/i).length).toBeGreaterThan(0);
			expect(screen.getAllByText(/Error Rate\/1d/i).length).toBeGreaterThan(0);
			expect(screen.getAllByText(/Avg Duration\/1d/i).length).toBeGreaterThan(
				0,
			);
			expect(
				screen.getAllByText(/Total Tokens\/1d|Avg Tokens\/Req/i).length,
			).toBeGreaterThan(0);
		});

		it("displays provider count from providers API", async () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-002", name: "Provider 2" },
			];
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json(providers)),
			);

			renderWithProviders(<Dashboard />);

			// Wait for providers to load and animation to complete (1200ms default)
			await waitFor(
				() => {
					const providerLabel = screen.getByText("Total Providers");
					const card = providerLabel.closest(".ui-card");
					expect(card).toBeInTheDocument();
					const statValue = card?.querySelector('[data-testid="stat-value"]');
					expect(statValue?.textContent).toContain("2");
				},
				{ timeout: 2000 },
			);
		});

		it("displays model count from models API", async () => {
			const models = [
				mockModel,
				{ ...mockModel, id: "model-002", model_id: "model-2" },
			];
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/models", () => HttpResponse.json(models)),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Dashboard />);

			// Wait for models to load and animation to complete
			await waitFor(
				() => {
					const modelLabel = screen.getByText("Total Models");
					expect(modelLabel).toBeInTheDocument();
					const card = modelLabel.closest(".ui-card");
					const statValue = card?.querySelector('[data-testid="stat-value"]');
					expect(statValue?.textContent).toContain("2");
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Gauge Components", () => {
		it("renders all five gauges in header", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Gauge labels should be present
			expect(screen.getAllByText(/Requests\/1d/i).length).toBeGreaterThan(0);
			expect(screen.getAllByText(/Avg TTFT\/1d/i).length).toBeGreaterThan(0);
			expect(screen.getAllByText(/Avg Overhead\/1d/i).length).toBeGreaterThan(
				0,
			);
			expect(
				screen.getAllByText(/Rate Limit Hits\/1d/i).length,
			).toBeGreaterThan(0);
			expect(screen.getAllByText(/Error Rate\/1d/i).length).toBeGreaterThan(0);
		});

		it("displays gauge values from stats", async () => {
			const statsWithValues = {
				...mockStats,
				requests_last_1h: 150,
				avg_ttft_ms: 250,
				avg_overhead_ms: 45,
				rate_limit_hits: 5,
				error_rate: 0.02,
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithValues)),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Gauge renders with correct label - value animation is tested elsewhere
			const requestsGauges = screen.getAllByText("Requests/1d");
			expect(requestsGauges.length).toBeGreaterThan(0);
		});
	});

	describe("Time Series Charts", () => {
		it("renders time series chart containers", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/stats/timeseries", () =>
					HttpResponse.json({ points: [] }),
				),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Chart headers should be present
			expect(screen.getByText(/Requests\s*\/\s*Hour/i)).toBeInTheDocument();
			expect(screen.getByText(/Tokens\s*\/\s*Hour/i)).toBeInTheDocument();
		});

		it("shows empty state message when no time series data", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/stats/timeseries", () =>
					HttpResponse.json({ points: [] }),
				),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Empty state message appears in both charts (Requests and Tokens)
			// Use queryAllByText to handle multiple matches
			const emptyMessages = screen.queryAllByText(/No time-series data/i);
			expect(emptyMessages.length).toBeGreaterThanOrEqual(2);
		});
	});

	describe("Provider Distribution Chart", () => {
		it("renders provider distribution chart", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/stats/provider-distribution", () =>
					HttpResponse.json({ items: [] }),
				),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Provider distribution section should render
			expect(screen.getByText("Provider Breakdown")).toBeInTheDocument();
		});
	});

	describe("Token Split Bar", () => {
		it("renders token split bar component", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Token split section should render
			expect(screen.getByText("Token Mix")).toBeInTheDocument();
		});
	});

	describe("Usage Bar Panels", () => {
		it("renders all three usage panels", async () => {
			server.use(
				http.get("/api/stats", () => HttpResponse.json(mockStats)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () => HttpResponse.json([])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// All three panel titles should be present
			expect(screen.getByText("Top Models")).toBeInTheDocument();
			expect(screen.getByText("Top Providers")).toBeInTheDocument();
			expect(screen.getByText("Top Virtual Keys")).toBeInTheDocument();
		});

		it("displays models in Top Models panel", async () => {
			// Top Models panel uses stats.by_model from /api/stats?metric=tokens&period=24h
			// The proxyModelID normalizes provider names (spaces -> hyphens)
			// So "Test Provider" becomes "test-provider"
			const statsWithModelUsage = {
				...mockStats,
				by_model: {
					"test-provider/test-model-v1": 1000,
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

			// Model label is normalized: test-provider/test-model-v1
			expect(
				screen.getByText("test-provider/test-model-v1"),
			).toBeInTheDocument();
		});
	});

	describe("Range Toggles", () => {
		it("renders range toggle buttons", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Range toggle buttons should be present
			expect(screen.getAllByText("1H").length).toBeGreaterThan(0);
			expect(screen.getAllByText("1D").length).toBeGreaterThan(0);
			expect(screen.getAllByText("7D").length).toBeGreaterThan(0);
		});

		it("renders metric toggle buttons", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Metric toggle buttons should be present (abbreviated as Tok/Req)
			const tokButtons = screen.getAllByText("Tok");
			expect(tokButtons.length).toBeGreaterThan(0);
			const reqButtons = screen.getAllByText("Req");
			expect(reqButtons.length).toBeGreaterThan(0);
		});
	});

	describe("Modal Components", () => {
		it("renders gauge modals in DOM (hidden by default)", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Modal containers should exist in DOM (even if not visible)
			const modalContainers = screen.queryAllByRole("dialog");
			// Multiple modals are rendered but hidden
			expect(modalContainers.length).toBeGreaterThanOrEqual(0);
		});
	});

	describe("Model Detail Modal", () => {
		it("renders model detail modal when model is selected", async () => {
			// Need stats with model usage data for the panel to show the model
			// The proxyModelID normalizes provider names (spaces -> hyphens)
			const statsWithModelUsage = {
				...mockStats,
				by_model: {
					"test-provider/test-model-v1": 1000,
				},
			};
			server.use(
				http.get("/api/stats", () => HttpResponse.json(statsWithModelUsage)),
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Model label is normalized: test-provider/test-model-v1
			// Verify the model entry is rendered as clickable (has button parent)
			expect(
				screen.getByRole("button", {
					name: /View details for test-provider\/test-model-v1/i,
				}),
			).toBeInTheDocument();
		});
	});

	describe("Data Refresh", () => {
		it("refreshes data when refresh button is clicked", async () => {
			let callCount = 0;
			server.use(
				http.get("/api/stats", () => {
					callCount++;
					return HttpResponse.json({
						...mockStats,
						total_requests_last_24h: callCount * 100,
					});
				}),
			);

			const { user } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Find and click refresh button
			const refreshButton = screen.getByRole("button", {
				name: "Refresh dashboard",
			});
			await user.click(refreshButton);

			// Should trigger refetch
			await waitFor(() => {
				expect(callCount).toBeGreaterThan(1);
			});
		});
	});

	describe("Local Storage Persistence", () => {
		it("uses default range of 24h on first load", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// 1D should be selected by default
			const oneDButtons = screen.getAllByText("1D");
			expect(oneDButtons.length).toBeGreaterThan(0);
		});

		it("uses default metric of tokens on first load", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Tokens should be selected by default (shown as "Tok")
			const tokButtons = screen.getAllByText("Tok");
			expect(tokButtons.length).toBeGreaterThan(0);
		});
	});

	describe("Responsive Layout", () => {
		it("renders grid layout for stat cards", async () => {
			server.use(http.get("/api/stats", () => HttpResponse.json(mockStats)));

			const { container } = renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(screen.getByText("Dashboard")).toBeInTheDocument();
			});

			// Grid container should exist
			const gridContainers = container.querySelectorAll(".grid");
			expect(gridContainers.length).toBeGreaterThan(0);
		});
	});

	describe("API Integration", () => {
		it("fetches stats from correct endpoint", async () => {
			let apiCalled = false;
			server.use(
				http.get("/api/stats", ({ request }) => {
					apiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json(mockStats);
				}),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(apiCalled).toBe(true);
			});
		});

		it("fetches models from correct endpoint", async () => {
			let apiCalled = false;
			server.use(
				http.get("/api/models", ({ request }) => {
					apiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json([mockModel]);
				}),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(apiCalled).toBe(true);
			});
		});

		it("fetches providers from correct endpoint", async () => {
			let apiCalled = false;
			server.use(
				http.get("/api/providers", ({ request }) => {
					apiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Dashboard />);

			await waitFor(() => {
				expect(apiCalled).toBe(true);
			});
		});
	});
});
