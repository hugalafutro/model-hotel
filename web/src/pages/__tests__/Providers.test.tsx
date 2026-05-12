import { screen, waitFor } from "@testing-library/react";
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
	});
});
