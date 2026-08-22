import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { Model } from "../../api/types";
import { Layout } from "../../components/Layout";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Models } from "../Models";

describe("Models", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.setItem("modelsViewMode", "paginate");
	});

	describe("View Mode Toggle", () => {
		it("starts in scroll mode by default and shows VirtualModelTable", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Models />);

			// Title should be "Models" (not count label)
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Toggle button should show "⬡ Pages" in scroll mode
			expect(
				screen.getByRole("button", { name: "Switch to pagination mode" }),
			).toHaveTextContent("⬡ Pages");

			// Badge should not be shown in scroll mode
			expect(screen.queryByText(/\d+ enabled/)).not.toBeInTheDocument();
		});

		it("renders the provider filter dropdown above the table", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Models />);

			// Shared FilterDropdown shows the all-providers label until a
			// provider is picked (1 provider in mock data → "All (1) Providers")
			await waitFor(() => {
				expect(screen.getByText("All (1) Providers")).toBeInTheDocument();
			});
		});

		it("switches from scroll to paginate mode when clicking toggle", async () => {
			localStorage.removeItem("modelsViewMode");

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Click toggle to switch to paginate mode
			await user.click(
				screen.getByRole("button", { name: "Switch to pagination mode" }),
			);

			// Should now show count label
			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Toggle button should now show "⇊ Scroll"
			expect(
				screen.getByRole("button", { name: "Switch to scroll mode" }),
			).toHaveTextContent("⇊ Scroll");
		});

		it("switches from paginate to scroll mode when clicking toggle", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("1 Model")).toBeInTheDocument();
			});

			// Click toggle to switch to scroll mode
			await user.click(
				screen.getByRole("button", { name: "Switch to scroll mode" }),
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

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Models />);

			// Should not show spinner - query is disabled in scroll mode
			expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();

			// Should show title immediately
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
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
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json(models);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json(models);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json(models);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json(models);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

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

	describe("Model Interactions", () => {
		it("opens model detail modal when clicking on a model", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Click on the model row
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			// Modal should open
			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});
		});

		it("handles updateMutation success via ModelDetailModal", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.patch("/api/models/:id", async ({ request, params }) => {
					const body = (await request.json()) as Partial<Model>;
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						...body,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click edit button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Edit" }));

			// Change display name - find the display name input (first textbox)
			const inputs = within(modal).getAllByRole("textbox");
			const displayNameField = inputs[0];
			await user.clear(displayNameField);
			await user.type(displayNameField, "Updated Model Name");

			// Click save
			await user.click(
				within(modal).getByRole("button", { name: "Save Changes" }),
			);

			// Should show success toast
			await waitFor(() => {
				expect(screen.getByText("Model updated")).toBeInTheDocument();
			});
		});

		it("handles updateMutation error via ModelDetailModal", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.patch("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database connection failed" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click edit button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Edit" }));

			// Change display name - find the display name input (first textbox)
			const inputs = within(modal).getAllByRole("textbox");
			const displayNameField = inputs[0];
			await user.clear(displayNameField);
			await user.type(displayNameField, "Updated Model Name");

			// Click save
			await user.click(
				within(modal).getByRole("button", { name: "Save Changes" }),
			);

			// Should show error toast - check for partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to update model:/)).toBeInTheDocument();
			});
		});

		it("handles deleteMutation success via ModelDetailModal", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.delete("/api/models/:id", () => {
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click delete button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Delete" }));

			// Click confirm delete
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);

			// Should show success toast
			await waitFor(() => {
				expect(
					screen.getByText("Model deleted successfully"),
				).toBeInTheDocument();
			});
		});

		it("handles deleteMutation error via ModelDetailModal", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.delete("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database constraint violation" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click delete button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Delete" }));

			// Click confirm delete
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);

			// Should show error toast - partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to delete model:/)).toBeInTheDocument();
			});
		});

		it("calls handleDiscover and invalidates queries", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json({ discovered: 5 });
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click "Update info" button (discover)
			const modal = screen.getByRole("dialog");
			const updateButton = within(modal).getByRole("button", {
				name: "Update info",
			});
			await user.click(updateButton);

			// After discovery completes, should show cooldown
			await waitFor(
				() => {
					expect(updateButton).toHaveTextContent(/Update \(\d+s\)/);
				},
				{ timeout: 5000 },
			);
		});

		it("calls handleTest and shows success toast", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json({
						success: true,
						ttft_ms: 150,
						duration_ms: 800,
						streaming: true,
						response: "This is a test response from the model",
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			const testButton = within(modal).getByRole("button", { name: "Test" });
			await user.click(testButton);

			// Should show success toast
			await waitFor(
				() => {
					expect(screen.getByText(/^Success \|/)).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("calls handleTest and shows error toast on failure", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json({
						success: false,
						ttft_ms: 0,
						duration_ms: 0,
						streaming: false,
						response: "",
						error: "Model timeout",
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Test" }));

			// Should show error toast
			await waitFor(() => {
				expect(screen.getByText(/Test failed:/)).toBeInTheDocument();
			});
		});

		it("calls handleTest and shows error toast on exception", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/models/:id/test", () => {
					return HttpResponse.json(
						{ error: "Connection refused" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Click Test button
			const modal = screen.getByRole("dialog");
			await user.click(within(modal).getByRole("button", { name: "Test" }));

			// Should show error toast
			await waitFor(() => {
				expect(screen.getByText(/Test failed:/)).toBeInTheDocument();
			});
		});

		it("toggles model enabled/disabled state", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.patch("/api/models/:id", async ({ params }) => {
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						enabled: false,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: /Enabled|Disabled/i,
			});
			await user.click(toggleButton);

			// Should show toast
			await waitFor(() => {
				expect(screen.getByText("Model disabled")).toBeInTheDocument();
			});
		});

		it("handles toggleMutation onError", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.patch("/api/models/:id", () => {
					return HttpResponse.json(
						{ error: "Database connection failed" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: /Enabled|Disabled/i,
			});
			await user.click(toggleButton);

			// Should show error toast - partial match
			await waitFor(() => {
				expect(screen.getByText(/Failed to update model:/)).toBeInTheDocument();
			});
		});

		it("updates detailModel state on toggle success", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.patch("/api/models/:id", async ({ params }) => {
					return HttpResponse.json({
						...mockModel,
						id: params.id as string,
						enabled: false,
					});
				}),
			);

			const { user } = renderWithProviders(<Models />);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});

			// Open detail modal
			await user.click(
				screen.getByText("Test Model").closest("tr") as HTMLElement,
			);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Test Model v1" }),
				).toBeInTheDocument();
			});

			// Find and click the toggle button in the modal
			const modal = screen.getByRole("dialog");
			const toggleButton = within(modal).getByRole("button", {
				name: "Enabled",
			});
			await user.click(toggleButton);

			// After toggle, button should now show "Disabled"
			await waitFor(() => {
				expect(
					within(modal).getByRole("button", { name: "Disabled" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("countLabel", () => {
		it("shows 'Models' (without count) when 0 models in paginate mode", async () => {
			// Ensure paginate mode is set
			localStorage.setItem("modelsViewMode", "paginate");

			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Models />);

			// countLabel returns just "Models" for 0 count (not "0 Models")
			await waitFor(() => {
				expect(screen.getByText("Models")).toBeInTheDocument();
			});

			// Verify paginate mode is active (toggle button should show "⇊ Scroll")
			expect(
				screen.getByRole("button", { name: "Switch to scroll mode" }),
			).toBeInTheDocument();
		});
	});

	describe("API Error Handling", () => {
		it("handles models API error gracefully", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch" },
						{ status: 500 },
					);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Models />);

			// Should handle error gracefully - may show empty state or error
			await waitFor(() => {
				expect(
					screen.queryByText(/No models|Failed:|Error/),
				).toBeInTheDocument();
			});
		});

		it("handles providers API error gracefully", async () => {
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch" },
						{ status: 500 },
					);
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

	/**
	 * The Models nav badge lives in Layout, on its own 60s poll on
	 * ["discovery-status"]: this page's `["models"]` invalidations never reach
	 * it. So these mount the page inside the real Layout and read the real badge
	 * rather than a probe query, and each serves a `claim_count` that actually
	 * falls once the write lands — a fixed payload would be right by accident
	 * whether or not anything re-read it.
	 */
	describe("Models nav badge", () => {
		const status = (claim_count: number) => ({
			claims: [],
			group_claims: [],
			informational: [],
			claim_count,
			informational_unseen: 0,
		});

		const disabledModel = {
			...mockModel,
			id: "model-002",
			model_id: "test-model-v2",
			name: "Retired Model",
			display_name: "Retired Model v2",
			enabled: false,
		};

		function renderInLayout() {
			return renderWithProviders(
				<Layout>
					<Models />
				</Layout>,
			);
		}

		/** Opens the detail modal for the enabled mock model. */
		async function openDetailModal(
			user: ReturnType<typeof renderWithProviders>["user"],
		) {
			await user.click(
				(await screen.findByText("Test Model")).closest("tr") as HTMLElement,
			);
			return screen.findByRole("dialog");
		}

		it("re-reads the badge after a model is deleted", async () => {
			// Deleting a gone model is one way to resolve its claim, and it is done
			// from this page with the badge in view.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [] : [mockModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 0 : 1)),
				),
				http.delete("/api/models/:id", () => {
					deleted = true;
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("1");

			const modal = await openDetailModal(user);
			await user.click(within(modal).getByRole("button", { name: "Delete" }));
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);
			await waitFor(() => expect(deleted).toBe(true));

			// Gone entirely, which the stale poll response would never produce.
			await waitFor(() =>
				expect(screen.queryByTestId("discovery-status-badge")).toBeNull(),
			);
		});

		it("clears the table row too when a delete reports failure", async () => {
			// Re-reading only the badge would leave the page contradicting itself:
			// the count drops to reflect a model that is gone while its row sits
			// there claiming it still exists. Both re-read on settle.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [] : [mockModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 0 : 1)),
				),
				http.delete("/api/models/:id", () => {
					deleted = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("1");

			const modal = await openDetailModal(user);
			await user.click(within(modal).getByRole("button", { name: "Delete" }));
			await user.click(
				within(modal).getByRole("button", { name: "Confirm delete" }),
			);
			await screen.findByText(/Failed to delete/);

			await waitFor(() => expect(screen.queryByText("Test Model")).toBeNull());
			expect(screen.queryByTestId("discovery-status-badge")).toBeNull();
		});

		it("re-reads the badge after the disabled models are bulk deleted", async () => {
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [mockModel] : [mockModel, disabledModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 1 : 2)),
				),
				http.post("/api/models/bulk-delete", () => {
					deleted = true;
					return HttpResponse.json({ requested: 1, deleted: 1 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			await user.click(await screen.findByText("Delete 1 disabled"));
			// ConfirmDialog confirms through the modal's fade-out, so the request is
			// a timer away from the click, not a microtask.
			await user.click(within(screen.getByRole("dialog")).getByText("Delete"));
			await waitFor(() => expect(deleted).toBe(true));

			// The enabled model's claim survives, so this pins a re-read rather than
			// a badge that merely happens to be empty.
			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a model is toggled", async () => {
			// Toggling reclassifies rather than removes: buildProviderClaims puts an
			// enabled model in Suspect, and Suspect is not counted, so re-enabling a
			// Gone model drops claim_count without deleting anything.
			let enabled = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json([{ ...mockModel, enabled }]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(enabled ? 1 : 2)),
				),
				http.patch("/api/models/:id", () => {
					enabled = true;
					return HttpResponse.json({ ...mockModel, enabled: true });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			const modal = await openDetailModal(user);
			await user.click(
				within(modal).getByRole("button", { name: /Enabled|Disabled/i }),
			);
			await waitFor(() => expect(enabled).toBe(true));

			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a bulk delete that reports failure", async () => {
			// A rejected request does not prove the write did not land: the server
			// can commit and the response be lost. The toast reports the failure,
			// but the badge must not keep asserting a count it can no longer back.
			let deleted = false;
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json(deleted ? [mockModel] : [mockModel, disabledModel]),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(deleted ? 1 : 2)),
				),
				http.post("/api/models/bulk-delete", () => {
					deleted = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");

			await user.click(await screen.findByText("Delete 1 disabled"));
			await user.click(within(screen.getByRole("dialog")).getByText("Delete"));
			await screen.findByText(/Failed to delete/);

			await waitFor(() =>
				expect(screen.getByTestId("discovery-status-badge")).toHaveTextContent(
					"1",
				),
			);
		});

		it("re-reads the badge after a discover run from the detail modal", async () => {
			// A discover run can clear a claim by finding the model listed again, or
			// raise one by confirming it missing. Either way the badge is as stale
			// as it is after a dismissal.
			let discovered = false;
			server.use(
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status(discovered ? 0 : 3)),
				),
				http.post("/api/providers/:id/discover", () => {
					discovered = true;
					return HttpResponse.json({ discovered: 1, diff: {} });
				}),
			);

			const { user } = renderInLayout();
			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("3");

			const modal = await openDetailModal(user);
			await user.click(
				within(modal).getByRole("button", { name: "Update info" }),
			);
			await waitFor(() => expect(discovered).toBe(true));

			await waitFor(() =>
				expect(screen.queryByTestId("discovery-status-badge")).toBeNull(),
			);
		});
	});
});
