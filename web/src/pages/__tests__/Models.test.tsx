import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { Model } from "../../api/types";
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

			await waitFor(() => {
				expect(screen.getByText("3 Models")).toBeInTheDocument();
			});

			// Badge should show breakdown
			expect(screen.getByText("2 enabled")).toBeInTheDocument();
			expect(screen.getByText("1 disabled")).toBeInTheDocument();
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

			await waitFor(() => {
				expect(screen.getByText("2 Models")).toBeInTheDocument();
			});

			// No breakdown badge when all same state
			expect(screen.queryByText("disabled")).not.toBeInTheDocument();
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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
			await user.click(screen.getByText("Test Model"));

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
});
