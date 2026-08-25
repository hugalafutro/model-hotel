import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults, mockModelsCursor } from "../../../test/helpers";
import { mockModel, mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Models } from "../../Models";

describe("Models", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.setItem("modelsViewMode", "paginate");
	});

	describe("View Mode Toggle", () => {
		it("starts in scroll mode by default and shows VirtualModelTable", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(...mockAllDefaults());

			renderWithProviders(<Models />);

			// Title should be "Models" (not count label)
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Toggle glyph reflects scroll mode
			expect(
				screen
					.getByTitle(
						"Click to toggle between pagination and infinite scrolling.",
					)
					.querySelector(".icon-infinite-scroll"),
			).toBeInTheDocument();

			// Badge should not be shown in scroll mode
			expect(screen.queryByText(/\d+ enabled/)).not.toBeInTheDocument();
		});

		it("switches from scroll to paginate mode when clicking toggle", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(...mockAllDefaults());

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Click toggle to switch to paginate mode
			await user.click(
				screen.getByTitle(
					"Click to toggle between pagination and infinite scrolling.",
				),
			);

			// Should now show count label
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Toggle glyph now reflects paginate mode
			expect(
				screen
					.getByTitle(
						"Click to toggle between pagination and infinite scrolling.",
					)
					.querySelector(".icon-pages"),
			).toBeInTheDocument();
		});

		it("switches from paginate to scroll mode when clicking toggle", async () => {
			server.use(...mockAllDefaults());

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Click toggle to switch to scroll mode
			await user.click(
				screen.getByTitle(
					"Click to toggle between pagination and infinite scrolling.",
				),
			);

			// Should now show "Models" without count
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Badge should not be shown in scroll mode
			expect(screen.queryByText(/\d+ enabled/)).not.toBeInTheDocument();
		});

		it("does not show loading spinner in scroll mode even when models query is disabled", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(...mockAllDefaults());

			renderWithProviders(<Models />);

			// Should not show spinner - query is disabled in scroll mode
			expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();

			// Should show title immediately
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});
		});

		it("shows model count in header when cursor API returns total", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(
				...mockAllDefaults(),
				...mockModelsCursor({
					body: {
						entries: [mockModel],
						total: 42,
						enabled_total: 42,
						parked_total: 0,
						disabled_total: 0,
						has_before: false,
						has_after: false,
					},
				}),
			);

			renderWithProviders(<Models />);

			// Title should show count from cursor total
			await waitFor(() => {
				expect(screen.getByText("42 Models")).toBeInTheDocument();
			});
		});
		it("renders the provider filter dropdown above the table", async () => {
			server.use(...mockAllDefaults());

			renderWithProviders(<Models />);

			// Shared FilterDropdown shows the all-providers label until a
			// provider is picked (1 provider in mock data → "All (1) Providers")
			await waitFor(() => {
				expect(screen.getByText("All (1) Providers")).toBeInTheDocument();
			});
		});
	});

	describe("Provider scope", () => {
		const onProvider = {
			...mockProvider,
			id: "prov-on",
			name: "On",
			enabled: true,
		};
		const offProvider = {
			...mockProvider,
			id: "prov-off",
			name: "Off",
			enabled: false,
		};

		function serveScoped(requested: string[]) {
			server.use(
				http.get("/api/models", ({ request }) => {
					const scope = new URL(request.url).searchParams.get(
						"provider_enabled",
					);
					requested.push(scope ?? "none");
					const rows = [
						{ ...mockModel, id: "m-on", provider_id: "prov-on" },
						{
							...mockModel,
							id: "m-off",
							provider_id: "prov-off",
							provider_enabled: false,
						},
					];
					if (scope === "true") return HttpResponse.json([rows[0]]);
					if (scope === "false") return HttpResponse.json([rows[1]]);
					return HttpResponse.json(rows);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([onProvider, offProvider]);
				}),
			);
		}

		it("defaults to active providers so the count matches what the proxy serves", async () => {
			const requested: string[] = [];
			serveScoped(requested);

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});
			expect(requested).toContain("true");
			expect(requested).not.toContain("none");
			// The provider dropdown only offers enabled providers in this scope.
			expect(screen.getByText("All (1) Providers")).toBeInTheDocument();
		});

		it("places the scope picker in the title row beside the badge", async () => {
			const requested: string[] = [];
			serveScoped(requested);

			const { container } = renderWithProviders(<Models />);
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			const titleRow = container.querySelector(".page-header-title-row");
			expect(titleRow).not.toBeNull();
			expect(
				within(titleRow as HTMLElement).getByRole("button", {
					name: "Filter: Active providers",
				}),
			).toBeInTheDocument();
		});

		it("switches scope and requests the parked rows", async () => {
			const user = userEvent.setup();
			const requested: string[] = [];
			serveScoped(requested);

			renderWithProviders(<Models />);
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Filter: Active providers" }),
			);
			await user.click(screen.getByText("Disabled providers"));

			await waitFor(() => {
				expect(requested).toContain("false");
			});
			// A parked row is listed but not usable: no count in the title, the
			// badge says why.
			await waitFor(() => {
				expect(screen.getByText("1 parked")).toBeInTheDocument();
			});
			expect(screen.queryByText(/\d+ disabled/)).not.toBeInTheDocument();
			expect(
				screen.getByRole("heading", { name: "Models" }),
			).toBeInTheDocument();

			await user.click(
				screen.getByRole("button", { name: "Filter: Disabled providers" }),
			);
			await user.click(screen.getByText("All providers"));

			// "All" lists the parked row too, but a parked model is not usable, so
			// the title stays at 1 and the badge carries the parked one.
			await waitFor(() => {
				expect(screen.getByText("All (2) Providers")).toBeInTheDocument();
			});
			expect(requested).toContain("none");
			expect(screen.getByText("1 Model")).toBeInTheDocument();
			expect(screen.getByText("1 parked")).toBeInTheDocument();
		});

		it("titles scroll mode with the server's usable count, not the row total", async () => {
			localStorage.setItem("modelsViewMode", "scroll");
			server.use(
				http.get("/api/models/cursor", () => {
					return HttpResponse.json({
						entries: [],
						total: 1000,
						enabled_total: 950,
						parked_total: 30,
						disabled_total: 0,
						has_before: false,
						has_after: false,
					});
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([onProvider]);
				}),
			);

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("950 Models")).toBeInTheDocument();
			});
			// 1000 rows = 950 usable + 20 switched off + 30 parked.
			expect(screen.getByText("20 disabled")).toBeInTheDocument();
			expect(screen.getByText("30 parked")).toBeInTheDocument();
		});

		it("drops a picked provider that falls outside the new scope", async () => {
			const user = userEvent.setup();
			serveScoped([]);

			renderWithProviders(<Models />);
			await waitFor(() => {
				expect(screen.getByText("All (1) Providers")).toBeInTheDocument();
			});

			// Pick the only active provider, then narrow the scope to disabled ones:
			// "On" is no longer offered, so the filter resets to all.
			await user.click(
				screen.getByRole("button", { name: "All (1) Providers" }),
			);
			await user.click(screen.getByText("On"));
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "All (1) Providers: On" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Filter: Active providers" }),
			);
			await user.click(screen.getByText("Disabled providers"));

			await waitFor(() => {
				expect(screen.getByText("All (1) Providers")).toBeInTheDocument();
			});
			expect(
				screen.queryByRole("button", { name: "All (1) Providers: On" }),
			).not.toBeInTheDocument();
		});
	});

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/models", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json([mockModel]));
						}, 100);
					});
				}),
			);

			renderWithProviders(<Models />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});
	});

	describe("Rendering", () => {
		it("renders page header with correct title and icon", async () => {
			server.use(...mockAllDefaults());

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Discovered models from your providers"),
			).toBeInTheDocument();
		});

		it("renders model count badge with enabled/disabled breakdown", async () => {
			const models = [
				{ ...mockModel, id: "model-001", enabled: true },
				{ ...mockModel, id: "model-002", enabled: true },
				{ ...mockModel, id: "model-003", enabled: false },
			];

			server.use(...mockAllDefaults({ models }));

			renderWithProviders(<Models />);

			// Title counts usable rows (model AND provider enabled); the badge
			// carries the remainder so the rows in view still add up.
			await waitFor(() => {
				expect(screen.getByText("2 Models")).toBeInTheDocument();
			});
			expect(screen.getByText("1 disabled")).toBeInTheDocument();
			expect(screen.queryByText(/\d+ enabled/)).not.toBeInTheDocument();
		});

		it("renders model table with models", async () => {
			server.use(...mockAllDefaults());

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Table should have headers
			expect(screen.getByText("Model")).toBeInTheDocument();
			expect(screen.getByText("Capabilities")).toBeInTheDocument();
			expect(screen.getByText("Provider")).toBeInTheDocument();
			expect(screen.getByText("Discovered")).toBeInTheDocument();
			expect(screen.getByText("Ctx")).toBeInTheDocument();
			expect(screen.getByText("Max Out")).toBeInTheDocument();
			expect(screen.getByText("Status")).toBeInTheDocument();
		});

		it("renders empty state when no models", async () => {
			server.use(...mockAllDefaults({ models: [] }));

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No models discovered yet. Add a provider and discover models.",
					),
				).toBeInTheDocument();
			});
		});

		it("renders model count in header correctly", async () => {
			const models = Array.from({ length: 5 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `test-model-${i}`,
			}));

			server.use(...mockAllDefaults({ models }));

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("5 Models")).toBeInTheDocument();
			});
		});

		it("shows all models enabled badge when all are enabled", async () => {
			const models = [
				{ ...mockModel, id: "model-001", enabled: true },
				{ ...mockModel, id: "model-002", enabled: true },
			];

			server.use(...mockAllDefaults({ models }));

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("2 Models")).toBeInTheDocument();
			});

			// No breakdown badge when all same state
			expect(screen.queryByText(/\d+ enabled/)).not.toBeInTheDocument();
		});

		it("shows all models disabled badge when all are disabled", async () => {
			const models = [
				{ ...mockModel, id: "model-001", enabled: false },
				{ ...mockModel, id: "model-002", enabled: false },
			];

			server.use(...mockAllDefaults({ models }));

			renderWithProviders(<Models />);

			// Nothing usable: the title has no count and the badge explains why.
			await waitFor(() => {
				expect(screen.getByText("2 disabled")).toBeInTheDocument();
			});
			expect(
				screen.getByRole("heading", { name: "Models" }),
			).toBeInTheDocument();
		});
	});

	describe("countLabel", () => {
		it("shows 'Models' (without count) when 0 models in paginate mode", async () => {
			// Ensure paginate mode is set
			localStorage.setItem("modelsViewMode", "paginate");

			server.use(...mockAllDefaults({ models: [] }));

			renderWithProviders(<Models />);

			// countLabel returns just "Models" for 0 count (not "0 Models")
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Verify paginate mode is active (toggle shows the pages glyph)
			expect(
				screen
					.getByTitle(
						"Click to toggle between pagination and infinite scrolling.",
					)
					.querySelector(".icon-pages"),
			).toBeInTheDocument();
		});

		it("passes the plural base key, so the header pluralises the noun", async () => {
			// The header must hand countLabel "models.page_title" and let
			// i18next choose the suffix. Handing it an already-resolved form
			// (t("models.page_title_one")) still typechecks, but that string is
			// not a key, so the title would read "2 Model" here and no locale
			// with a third plural category could ever reach its own form.
			localStorage.setItem("modelsViewMode", "paginate");

			server.use(
				...mockAllDefaults({
					models: [
						{ ...mockModel, id: "model-alpha", model_id: "alpha" },
						{ ...mockModel, id: "model-beta", model_id: "beta" },
					],
				}),
			);

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("2 Models")).toBeInTheDocument();
			});
		});
	});

	describe("API Error Handling", () => {
		it("handles models API error gracefully", async () => {
			server.use(
				...mockAllDefaults({
					models: { status: 500, body: { error: "Failed to fetch" } },
				}),
			);

			renderWithProviders(<Models />);

			// On query error, models is undefined, so models ?? [] = []
			// Component renders empty state
			await waitFor(() => {
				expect(
					screen.getByText(
						"No models discovered yet. Add a provider and discover models.",
					),
				).toBeInTheDocument();
			});
		});

		it("handles providers API error gracefully", async () => {
			server.use(
				...mockAllDefaults({
					providers: { status: 500, body: { error: "Failed to fetch" } },
				}),
			);

			renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Should still render models without provider data
			expect(screen.getByText("Test Model")).toBeInTheDocument();
		});
	});
});
