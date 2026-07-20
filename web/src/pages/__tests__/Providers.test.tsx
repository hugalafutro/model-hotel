import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Providers } from "../Providers";

describe("Providers", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/providers", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json([mockProvider]));
						}, 100);
					});
				}),
			);

			renderWithProviders(<Providers />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});
	});

	describe("Rendering", () => {
		it("renders page header with provider count", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("1 Provider")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Manage your provider configurations"),
			).toBeInTheDocument();
		});

		it("renders 'Discover All Models' button", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});
		});

		it("renders 'Refresh Quotas/Balances' button", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Refresh Quotas/Balances" }),
				).toBeInTheDocument();
			});
		});

		it("renders '+ Add Provider' button", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Add Provider" }),
				).toBeInTheDocument();
			});
		});

		it("shows filter input for name filtering", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter providers…"),
				).toBeInTheDocument();
			});
		});

		it("shows sort button", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /Sorted A-Z|Sorted Z-A/i }),
				).toBeInTheDocument();
			});
		});

		it("shows provider type filter dropdown", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Provider type" }),
				).toBeInTheDocument();
			});
		});

		it("renders provider cards after data loads", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});
		});

		it("shows empty state when no providers exist", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No providers configured. Add your first provider to get started.",
					),
				).toBeInTheDocument();
			});
		});

		it("shows empty state when filter matches nothing", async () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-002", name: "Other Provider" },
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Type a filter that matches nothing
			const filterInput = screen.getByPlaceholderText("Filter providers…");
			await user.type(filterInput, "NonExistentProvider");

			await waitFor(() => {
				expect(
					screen.getByText("No providers match the selected filter."),
				).toBeInTheDocument();
			});
		});
	});

	describe("Filtering", () => {
		it("name filter filters providers", async () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-002", name: "Alpha Provider" },
				{ ...mockProvider, id: "provider-003", name: "Beta Provider" },
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
				expect(screen.getByText("Alpha Provider")).toBeInTheDocument();
				expect(screen.getByText("Beta Provider")).toBeInTheDocument();
			});

			// Filter by "Alpha"
			const filterInput = screen.getByPlaceholderText("Filter providers…");
			await user.type(filterInput, "Alpha");

			await waitFor(() => {
				expect(screen.getByText("Alpha Provider")).toBeInTheDocument();
				expect(screen.queryByText("Test Provider")).not.toBeInTheDocument();
				expect(screen.queryByText("Beta Provider")).not.toBeInTheDocument();
			});
		});

		it("type filter filters by provider type", async () => {
			// Create providers with different types based on base_url
			// mockProvider has base_url "https://api.test-provider.com/v1" which is "custom" type
			const providers = [
				mockProvider,
				{
					...mockProvider,
					id: "provider-002",
					name: "Ollama Provider",
					base_url: "http://localhost:11434", // This is recognized as "ollama" type
				},
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
				expect(screen.getByText("Ollama Provider")).toBeInTheDocument();
			});

			// Open type filter dropdown
			const typeFilterButton = screen.getByRole("button", {
				name: "Provider type",
			});
			await user.click(typeFilterButton);

			// Verify dropdown opens and shows "Ollama" option (text is split across elements)
			await waitFor(() => {
				expect(screen.getByText("Ollama")).toBeInTheDocument();
			});

			// Click the Ollama option - it's a button containing the text "Ollama"
			// The dropdown options have a specific structure with the count in a child span
			const ollamaButtons = screen.getAllByRole("button");
			const ollamaOption = ollamaButtons.find(
				(btn) =>
					btn.textContent?.includes("Ollama") &&
					btn.classList.contains("w-full"),
			);
			if (ollamaOption) {
				await user.click(ollamaOption);
			}

			await waitFor(() => {
				expect(screen.getByText("Ollama Provider")).toBeInTheDocument();
				expect(screen.queryByText("Test Provider")).not.toBeInTheDocument();
			});
		});
	});

	describe("Sorting", () => {
		it("sort toggle reverses order", async () => {
			const providers = [
				{ ...mockProvider, id: "provider-001", name: "Alpha Provider" },
				{ ...mockProvider, id: "provider-002", name: "Beta Provider" },
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Provider")).toBeInTheDocument();
				expect(screen.getByText("Beta Provider")).toBeInTheDocument();
			});

			// Initial order should be A-Z (Alpha first)
			const providerNames = screen.getAllByText(/(Alpha|Beta) Provider/);
			expect(providerNames[0].textContent).toBe("Alpha Provider");
			expect(providerNames[1].textContent).toBe("Beta Provider");

			// Click sort button to reverse
			const sortButton = screen.getByRole("button", {
				name: /Sorted A-Z|Sorted Z-A/i,
			});
			await user.click(sortButton);

			// Order should now be Z-A (Beta first)
			await waitFor(() => {
				const reversedNames = screen.getAllByText(/(Alpha|Beta) Provider/);
				expect(reversedNames[0].textContent).toBe("Beta Provider");
				expect(reversedNames[1].textContent).toBe("Alpha Provider");
			});
		});
	});

	describe("Discover All Models", () => {
		it("'Discover All Models' button calls API", async () => {
			let discoverAllCalled = false;

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					discoverAllCalled = true;
					return HttpResponse.json({
						succeeded: 1,
						failed: 0,
						discovered: 5,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			await waitFor(() => {
				expect(discoverAllCalled).toBe(true);
			});
		});

		it("opens the discovery summary modal after discover all", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					return HttpResponse.json({
						succeeded: 1,
						failed: 0,
						discovered: 5,
						results: [
							{
								provider_name: mockProvider.name,
								discovered: 5,
								diff: {
									added: [{ model_id: "brand-new-model", reason: "new_model" }],
									disabled: [
										{ model_id: "vanished-model", reason: "not_listed" },
									],
								},
							},
						],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			await waitFor(() => {
				expect(screen.getByTestId("discovery-summary")).toBeInTheDocument();
			});
			expect(screen.getByTestId("discovery-summary-added")).toHaveTextContent(
				"brand-new-model",
			);
			expect(
				screen.getByTestId("discovery-summary-disabled"),
			).toHaveTextContent("vanished-model");

			// Closing the summary clears it.
			await user.click(screen.getByRole("button", { name: "Close" }));
			await waitFor(() => {
				expect(
					screen.queryByTestId("discovery-summary"),
				).not.toBeInTheDocument();
			});
		});

		it("shows error toast when discover all fails for all providers", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					return HttpResponse.json({
						succeeded: 0,
						failed: 3,
						discovered: 0,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			await waitFor(() => {
				expect(
					screen.getByText("Discovery failed for all 3 providers"),
				).toBeInTheDocument();
			});
		});

		it("shows no toast when discover all has partial success", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					return HttpResponse.json({
						succeeded: 2,
						failed: 1,
						discovered: 10,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			// Wait for mutation to settle (button re-enables)
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).not.toBeDisabled();
			});

			expect(screen.queryByText(/discovery failed/i)).not.toBeInTheDocument();
		});

		it("shows no toast when discover all succeeds completely", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					return HttpResponse.json({
						succeeded: 3,
						failed: 0,
						discovered: 15,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			// Wait for mutation to settle (button re-enables)
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).not.toBeDisabled();
			});

			expect(screen.queryByText(/discovery failed/i)).not.toBeInTheDocument();
		});

		it("shows error toast when discover all mutation errors", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", () => {
					return HttpResponse.json(
						{ error: "Internal server error" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Discover All Models" }),
			);

			// Error message includes full HTTP response
			await waitFor(() => {
				expect(
					screen.getByText(/Discovery failed:.*Internal server error/),
				).toBeInTheDocument();
			});
		});

		it("disables button and shows spinner when discover all is pending", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/discover-all", async () => {
					await new Promise((resolve) => setTimeout(resolve, 100));
					return HttpResponse.json({
						succeeded: 1,
						failed: 0,
						discovered: 5,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover All Models" }),
				).toBeInTheDocument();
			});

			const button = screen.getByRole("button", {
				name: "Discover All Models",
			});
			await user.click(button);

			// Button should be disabled and show "Discovering..."
			await waitFor(() => {
				const discoveringBtn = screen.getByRole("button", {
					name: /discovering\.\.\./i,
				});
				expect(discoveringBtn).toBeDisabled();
			});
		});

		it("disables discover all button when discovering individual provider", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/:id/discover", async () => {
					await new Promise((resolve) => setTimeout(resolve, 100));
					return HttpResponse.json({ discovered: 5 });
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click "Discover Models" on the provider card
			const discoverBtn = screen.getByRole("button", {
				name: "Discover Models",
			});
			await user.click(discoverBtn);

			// Discover All button should be disabled
			await waitFor(() => {
				const discoverAllBtn = screen.getByRole("button", {
					name: "Discover All Models",
				});
				expect(discoverAllBtn).toBeDisabled();
			});
		});
	});

	describe("Add Provider Modal", () => {
		it("'+ Add Provider' opens AddProviderModal", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Add Provider" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Add Provider" }));

			await waitFor(() => {
				// Check for modal heading (h2) specifically
				expect(
					screen.getByRole("heading", { name: "Add Provider" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("Edit Flow", () => {
		it("Edit flow opens EditProviderModal", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click Edit button on the provider card
			const editButton = screen.getByRole("button", { name: "Edit" });
			await user.click(editButton);

			await waitFor(() => {
				expect(screen.getByText("Edit Provider")).toBeInTheDocument();
			});
		});
	});

	describe("Delete Flow", () => {
		it("Delete flow opens DeleteConfirmModal", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click Delete button on the provider card
			const deleteButton = screen.getByRole("button", { name: "Delete" });
			await user.click(deleteButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});
		});

		it("deletes provider and shows success toast", async () => {
			let deleteCalled = false;

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.delete("/api/providers/:id", () => {
					deleteCalled = true;
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click Delete button on the card
			const deleteButton = screen.getByRole("button", { name: "Delete" });
			await user.click(deleteButton);

			// Wait for modal to appear
			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button in modal
			const modal = screen.getByRole("dialog");
			const modalDeleteButton = within(modal).getByRole("button", {
				name: "Delete",
			});
			await user.click(modalDeleteButton);

			await waitFor(() => {
				expect(deleteCalled).toBe(true);
				expect(screen.getByText("Provider deleted")).toBeInTheDocument();
			});
		});

		it("shows error toast when delete fails", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.delete("/api/providers/:id", () => {
					return HttpResponse.json(
						{ error: "Failed to delete provider" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click Delete button on the card
			const deleteButton = screen.getByRole("button", { name: "Delete" });
			await user.click(deleteButton);

			// Wait for modal to appear
			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button in modal
			const modal = screen.getByRole("dialog");
			const modalDeleteButton = within(modal).getByRole("button", {
				name: "Delete",
			});
			await user.click(modalDeleteButton);

			// Error message includes full HTTP response
			await waitFor(() => {
				expect(
					screen.getByText(/Failed to delete:.*Failed to delete provider/),
				).toBeInTheDocument();
			});
		});

		it("closes delete modal after deletion completes", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.delete("/api/providers/:id", () => {
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click Delete button on the card
			const deleteButton = screen.getByRole("button", { name: "Delete" });
			await user.click(deleteButton);

			// Wait for modal to appear
			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button in modal
			const modal = screen.getByRole("dialog");
			const modalDeleteButton = within(modal).getByRole("button", {
				name: "Delete",
			});
			await user.click(modalDeleteButton);

			// Modal should close after deletion
			await waitFor(() => {
				expect(
					screen.queryByText(/Are you sure you want to delete/),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Models Modal", () => {
		it("Models button opens ProviderModelsModal", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click the models count button (e.g., "1 model")
			const modelsButton = screen.getByRole("button", {
				name: /model/,
			});
			await user.click(modelsButton);

			await waitFor(() => {
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});
		});
	});

	describe("Refresh Quotas/Balances", () => {
		it("shows success toast when all quotas refresh successfully", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/refresh-quotas", () => {
					return HttpResponse.json({
						refreshed: 3,
						failed: 0,
						skipped: 0,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Refresh Quotas/Balances",
					}),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", {
					name: "Refresh Quotas/Balances",
				}),
			);

			await waitFor(() => {
				expect(
					screen.getByText("Refreshed 3 quotas/balances"),
				).toBeInTheDocument();
			});
		});

		it("shows warning toast when some quotas fail", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/refresh-quotas", () => {
					return HttpResponse.json({
						refreshed: 2,
						failed: 1,
						skipped: 1,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Refresh Quotas/Balances",
					}),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", {
					name: "Refresh Quotas/Balances",
				}),
			);

			await waitFor(() => {
				expect(
					screen.getByText("Refreshed 2 quotas (1 failed, 1 unsupported)"),
				).toBeInTheDocument();
			});
		});

		it("shows info toast when no providers with quota support found", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/refresh-quotas", () => {
					return HttpResponse.json({
						refreshed: 0,
						failed: 0,
						skipped: 0,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Refresh Quotas/Balances",
					}),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", {
					name: "Refresh Quotas/Balances",
				}),
			);

			await waitFor(() => {
				expect(
					screen.getByText("No providers with quota/balance support found"),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when refresh quotas mutation fails", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/refresh-quotas", () => {
					return HttpResponse.json(
						{ error: "Quota service unavailable" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Refresh Quotas/Balances",
					}),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", {
					name: "Refresh Quotas/Balances",
				}),
			);

			// Error message includes full HTTP response
			await waitFor(() => {
				expect(
					screen.getByText(/Refresh quotas failed:.*Quota service unavailable/),
				).toBeInTheDocument();
			});
		});

		it("disables button and shows spinner when refresh is pending", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/refresh-quotas", async () => {
					await new Promise((resolve) => setTimeout(resolve, 100));
					return HttpResponse.json({
						refreshed: 1,
						failed: 0,
						skipped: 0,
						results: [],
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Refresh Quotas/Balances",
					}),
				).toBeInTheDocument();
			});

			const button = screen.getByRole("button", {
				name: "Refresh Quotas/Balances",
			});
			await user.click(button);

			// Button should be disabled and show "Refreshing..."
			await waitFor(() => {
				const refreshingBtn = screen.getByRole("button", {
					name: /refreshing\.\.\./i,
				});
				expect(refreshingBtn).toBeDisabled();
			});
		});
	});

	describe("Error Handling", () => {
		it("server error shows error toast", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch providers" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<Providers />);

			// Should handle error gracefully - shows empty grid when query fails
			await waitFor(() => {
				expect(screen.getByText("Providers")).toBeInTheDocument();
			});
		});
	});

	describe("Multiple Providers", () => {
		it("renders page header with correct plural count", async () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-002", name: "Second Provider" },
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("2 Providers")).toBeInTheDocument();
			});
		});

		it("renders multiple provider cards", async () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-002", name: "Second Provider" },
				{ ...mockProvider, id: "provider-003", name: "Third Provider" },
			];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json(providers);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
				expect(screen.getByText("Second Provider")).toBeInTheDocument();
				expect(screen.getByText("Third Provider")).toBeInTheDocument();
			});
		});
	});

	describe("Provider Card Details", () => {
		it("shows provider base URL", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByText("https://api.test-provider.com/v1"),
				).toBeInTheDocument();
			});
		});

		it("shows provider masked API key", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByText("sk_test_••••••••••••••••••••••••"),
				).toBeInTheDocument();
			});
		});

		it("shows model count badge", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([mockModel]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /model/ }),
				).toBeInTheDocument();
			});
		});

		it("shows 'Discover Models' button on each card", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
			);

			renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Discover Models" }),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when discover mutation fails", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.post("/api/providers/:id/discover", () => {
					return HttpResponse.json(
						{ error: "Discovery service unavailable" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			const discoverBtn = screen.getByRole("button", {
				name: "Discover Models",
			});
			await user.click(discoverBtn);

			// Error message includes the full response
			await waitFor(() => {
				expect(
					screen.getByText(/Discovery failed:.*Discovery service unavailable/),
				).toBeInTheDocument();
			});
		});

		it("counts all models (enabled and disabled) in model count badge", async () => {
			const enabledModel = { ...mockModel, id: "model-001", enabled: true };
			const disabledModel = {
				...mockModel,
				id: "model-002",
				enabled: false,
			};

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([enabledModel, disabledModel]);
				}),
			);

			renderWithProviders(<Providers />);

			// Should show "2 models" (total, not just enabled)
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /2 model/ }),
				).toBeInTheDocument();
			});
		});

		it("does not show model count button when provider has no models", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([]);
				}),
			);

			renderWithProviders(<Providers />);

			// When modelCount is 0, the button is not rendered
			await waitFor(() => {
				expect(
					screen.queryByRole("button", { name: "0 models" }),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Type Filter Options", () => {
		it("sorts custom type first regardless of display name", async () => {
			const customProvider = {
				...mockProvider,
				id: "provider-001",
				name: "Custom Provider",
				base_url: "https://custom.example.com/v1",
			};
			const anthropicProvider = {
				...mockProvider,
				id: "provider-002",
				name: "Anthropic Provider",
				base_url: "https://api.anthropic.com",
			};
			const openaiProvider = {
				...mockProvider,
				id: "provider-003",
				name: "OpenAI Provider",
				base_url: "https://api.openai.com/v1",
			};

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([
						customProvider,
						anthropicProvider,
						openaiProvider,
					]);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			await waitFor(() => {
				expect(screen.getByText("Custom Provider")).toBeInTheDocument();
			});

			// Open type filter dropdown
			const typeFilterButton = screen.getByRole("button", {
				name: "Provider type",
			});
			await user.click(typeFilterButton);

			// Get all options in the dropdown
			await waitFor(() => {
				// Custom should be first (before Anthropic and OpenAI alphabetically)
				const options = screen.getAllByRole("button", {
					name: /custom|anthropic|openai/i,
				});
				// First option should be Custom
				expect(options[0].textContent).toContain("Custom");
			});
		});
	});

	describe("handleDeleteDisabledModels", () => {
		it("deletes all disabled models and shows success toast", async () => {
			const enabledModel = { ...mockModel, id: "model-001", enabled: true };
			const disabledModel1 = {
				...mockModel,
				id: "model-002",
				enabled: false,
				name: "Disabled Model 1",
			};
			const disabledModel2 = {
				...mockModel,
				id: "model-003",
				enabled: false,
				name: "Disabled Model 2",
			};

			let bulkCallCount = 0;
			const deletedIds: string[] = [];

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([
						enabledModel,
						disabledModel1,
						disabledModel2,
					]);
				}),
				http.post("/api/models/bulk-delete", async ({ request }) => {
					bulkCallCount++;
					const body = (await request.json()) as { ids: string[] };
					deletedIds.push(...body.ids);
					return HttpResponse.json({
						requested: body.ids.length,
						deleted: body.ids.length,
					});
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			// Wait for providers to load
			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click the models count button to open ProviderModelsModal
			const modelsButton = screen.getByRole("button", {
				name: /3 model/,
			});
			await user.click(modelsButton);

			// Wait for modal to open and show models
			await waitFor(() => {
				expect(screen.getByText("Disabled Model 1")).toBeInTheDocument();
			});

			// Click "Delete 2 disabled" button
			const deleteDisabledBtn = screen.getByRole("button", {
				name: "Delete 2 disabled models",
			});
			await user.click(deleteDisabledBtn);

			// Wait for ConfirmDialog to appear
			await waitFor(() => {
				expect(screen.getByText("Delete Disabled Models")).toBeInTheDocument();
			});

			// Find the ConfirmDialog (it contains "Delete Disabled Models" heading)
			const dialogs = screen.getAllByRole("dialog");
			const confirmDialog = dialogs.find((d) =>
				d.querySelector("h2")?.textContent?.includes("Delete Disabled Models"),
			);
			if (!confirmDialog) {
				throw new Error("ConfirmDialog not found");
			}
			const confirmBtn = within(confirmDialog).getByRole("button", {
				name: "Delete",
			});
			await user.click(confirmBtn);

			// Verify all disabled models were deleted in a single bulk request
			await waitFor(() => {
				expect(bulkCallCount).toBe(1);
				expect(deletedIds).toContain("model-002");
				expect(deletedIds).toContain("model-003");
				expect(
					screen.getByText(/Deleted 2 disabled model/),
				).toBeInTheDocument();
			});
		});

		it("shows error toast when the bulk delete request fails", async () => {
			const enabledModel = { ...mockModel, id: "model-001", enabled: true };
			const disabledModel1 = {
				...mockModel,
				id: "model-002",
				enabled: false,
				name: "Disabled Model 1",
			};
			const disabledModel2 = {
				...mockModel,
				id: "model-003",
				enabled: false,
				name: "Disabled Model 2",
			};
			const disabledModel3 = {
				...mockModel,
				id: "model-004",
				enabled: false,
				name: "Disabled Model 3",
			};

			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([mockProvider]);
				}),
				http.get("/api/models", () => {
					return HttpResponse.json([
						enabledModel,
						disabledModel1,
						disabledModel2,
						disabledModel3,
					]);
				}),
				http.post("/api/models/bulk-delete", () => {
					return HttpResponse.json(
						{ error: "Failed to delete models" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Providers />);

			// Wait for providers to load
			await waitFor(() => {
				expect(screen.getByText("Test Provider")).toBeInTheDocument();
			});

			// Click the models count button to open ProviderModelsModal
			const modelsButton = screen.getByRole("button", {
				name: /4 model/,
			});
			await user.click(modelsButton);

			// Wait for modal to open and show models
			await waitFor(() => {
				expect(screen.getByText("Disabled Model 1")).toBeInTheDocument();
			});

			// Click "Delete 3 disabled" button
			const deleteDisabledBtn = screen.getByRole("button", {
				name: "Delete 3 disabled models",
			});
			await user.click(deleteDisabledBtn);

			// Wait for ConfirmDialog to appear
			await waitFor(() => {
				expect(screen.getByText("Delete Disabled Models")).toBeInTheDocument();
			});

			// Find the ConfirmDialog (it contains "Delete Disabled Models" heading)
			const dialogs = screen.getAllByRole("dialog");
			const confirmDialog = dialogs.find((d) =>
				d.querySelector("h2")?.textContent?.includes("Delete Disabled Models"),
			);
			if (!confirmDialog) {
				throw new Error("ConfirmDialog not found");
			}
			const confirmBtn = within(confirmDialog).getByRole("button", {
				name: "Delete",
			});
			await user.click(confirmBtn);

			// Verify error toast (the whole request failed atomically)
			await waitFor(() => {
				expect(screen.getByText(/Failed to delete/)).toBeInTheDocument();
			});
		});
	});
});
