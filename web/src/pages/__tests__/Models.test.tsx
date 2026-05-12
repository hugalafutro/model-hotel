import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Models } from "../Models";

describe("Models", () => {
	beforeEach(() => {
		server.resetHandlers();
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
			expect(screen.queryByText("enabled")).not.toBeInTheDocument();
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
