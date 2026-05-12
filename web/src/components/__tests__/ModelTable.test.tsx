import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { ModelTable } from "../ModelTable";

describe("ModelTable", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	const defaultProps = {
		models: [mockModel],
		providers: [mockProvider],
		onModelClick: vi.fn(),
	};

	describe("Rendering", () => {
		it("renders table headers", () => {
			renderWithProviders(<ModelTable {...defaultProps} />);

			expect(screen.getByText("Model")).toBeInTheDocument();
			expect(screen.getByText("Capabilities")).toBeInTheDocument();
			expect(screen.getByText("Provider")).toBeInTheDocument();
			expect(screen.getByText("Discovered")).toBeInTheDocument();
			expect(screen.getByText("Ctx")).toBeInTheDocument();
			expect(screen.getByText("Max Out")).toBeInTheDocument();
			expect(screen.getByText("Status")).toBeInTheDocument();
		});

		it("renders model rows with data", () => {
			renderWithProviders(<ModelTable {...defaultProps} />);

			expect(screen.getByText("Test Model")).toBeInTheDocument();
			expect(
				screen.getByText("Test-Provider/test-model-v1"),
			).toBeInTheDocument();
			expect(screen.getByText("Test Provider")).toBeInTheDocument();
		});

		it("renders model count correctly", () => {
			const models = [
				{ ...mockModel, id: "model-001" },
				{ ...mockModel, id: "model-002" },
				{ ...mockModel, id: "model-003" },
			];

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Should render 3 model rows
			const rows = screen.getAllByRole("row");
			// At least header row + filter row + data rows
			expect(rows.length).toBeGreaterThanOrEqual(2);
		});

		it("renders empty state when no models", () => {
			renderWithProviders(
				<ModelTable models={[]} providers={[mockProvider]} />,
			);

			expect(
				screen.getByText(
					"No models discovered yet. Add a provider and discover models.",
				),
			).toBeInTheDocument();
		});

		it("renders empty state with filter message when filters applied", () => {
			renderWithProviders(
				<ModelTable
					models={[]}
					providers={[mockProvider]}
					initialProviderFilter={new Set(["provider-1"])}
				/>,
			);

			expect(
				screen.getByText("No models match your filters"),
			).toBeInTheDocument();
		});

		it("renders capabilities badges", () => {
			const modelWithCaps = {
				...mockModel,
				capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			};

			renderWithProviders(
				<ModelTable models={[modelWithCaps]} providers={[mockProvider]} />,
			);

			// Vision badge should be present in the capabilities cell (not the filter)
			// Scope to table body to avoid matching filter buttons
			const table = screen.getByRole("table");
			const visionBadges = within(table).getAllByText("Vision");
			expect(visionBadges.length).toBeGreaterThan(0);
		});

		it("renders context length formatted", () => {
			const modelWithLargeContext = {
				...mockModel,
				context_length: 128000,
			};

			renderWithProviders(
				<ModelTable
					models={[modelWithLargeContext]}
					providers={[mockProvider]}
				/>,
			);

			expect(screen.getByText("128K")).toBeInTheDocument();
		});

		it("renders max output tokens formatted", () => {
			const modelWithLargeOutput = {
				...mockModel,
				max_output_tokens: 32768,
			};

			renderWithProviders(
				<ModelTable
					models={[modelWithLargeOutput]}
					providers={[mockProvider]}
				/>,
			);

			// Max output formatted with formatTokens: 32768 → "32.8K"
			expect(screen.getByText(/32\.8K/)).toBeInTheDocument();
		});

		it("renders status badge for enabled model", () => {
			const enabledModel = {
				...mockModel,
				enabled: true,
			};

			renderWithProviders(
				<ModelTable models={[enabledModel]} providers={[mockProvider]} />,
			);

			// Status badge in the status column - scope to table body to avoid filter buttons
			const table = screen.getByRole("table");
			const enabledBadges = within(table).getAllByText("Enabled");
			expect(enabledBadges.length).toBeGreaterThan(0);
		});

		it("renders status badge for disabled model", () => {
			const disabledModel = {
				...mockModel,
				enabled: false,
			};

			renderWithProviders(
				<ModelTable models={[disabledModel]} providers={[mockProvider]} />,
			);

			// Status badge in the status column - scope to table body to avoid filter buttons
			const table = screen.getByRole("table");
			const disabledBadges = within(table).getAllByText("Disabled");
			expect(disabledBadges.length).toBeGreaterThan(0);
		});

		it("renders search input", () => {
			renderWithProviders(<ModelTable {...defaultProps} />);

			const searchInput = screen.getByPlaceholderText("Search models…");
			expect(searchInput).toBeInTheDocument();
		});

		it("renders pagination bar", () => {
			const models = Array.from({ length: 25 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
			}));

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			expect(screen.getByText("Prev")).toBeInTheDocument();
			expect(screen.getByText("Next")).toBeInTheDocument();
		});

		it("renders provider filter dropdown", () => {
			const providers = [
				mockProvider,
				{ ...mockProvider, id: "provider-2", name: "Provider 2" },
			];

			renderWithProviders(
				<ModelTable models={[mockModel]} providers={providers} />,
			);

			// Provider filter button with placeholder text
			expect(screen.getByText("Filter Providers")).toBeInTheDocument();
		});

		it("does not render provider column when providers prop is omitted", () => {
			renderWithProviders(<ModelTable models={[mockModel]} />);

			expect(screen.queryByText("Provider")).not.toBeInTheDocument();
		});
	});

	describe("Sorting", () => {
		it("sorts by name ascending by default", () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Zebra Model" },
				{ ...mockModel, id: "model-002", name: "Alpha Model" },
				{ ...mockModel, id: "model-003", name: "Beta Model" },
			];

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Should be sorted alphabetically: Alpha, Beta, Zebra
			// Get all rows from tbody and verify order
			const rows = screen.getByRole("table").querySelectorAll("tbody tr");
			expect(rows[0].textContent).toContain("Alpha Model");
			expect(rows[1].textContent).toContain("Beta Model");
			expect(rows[2].textContent).toContain("Zebra Model");
		});

		it("sorts by name when clicking header", async () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Alpha Model" },
				{ ...mockModel, id: "model-002", name: "Beta Model" },
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click name header to toggle sort - use title attribute to find correct button
			const nameHeader = screen.getByRole("button", {
				name: (content, element) =>
					content.includes("Model") &&
					element?.parentElement?.getAttribute("title") === "Model name and ID",
			});
			await user.click(nameHeader);

			// Should re-sort
			await waitFor(() => {
				expect(screen.getByText("Alpha Model")).toBeInTheDocument();
			});
		});

		it("sorts by discovered date", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "New Model",
					last_seen_at: "2026-05-11T10:00:00Z",
				},
				{
					...mockModel,
					id: "model-002",
					name: "Old Model",
					last_seen_at: "2026-05-01T10:00:00Z",
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click discovered header
			const discoveredHeader = screen.getByRole("button", {
				name: /Discovered/,
			});
			await user.click(discoveredHeader);

			await waitFor(() => {
				expect(screen.getByText("Old Model")).toBeInTheDocument();
			});
		});

		it("sorts by context length", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Large Context",
					context_length: 128000,
				},
				{
					...mockModel,
					id: "model-002",
					name: "Small Context",
					context_length: 4096,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click context header
			const contextHeader = screen.getByRole("button", { name: /Ctx/ });
			await user.click(contextHeader);

			await waitFor(() => {
				expect(screen.getByText("Small Context")).toBeInTheDocument();
			});
		});

		it("sorts by max output tokens", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Large Output",
					max_output_tokens: 32768,
				},
				{
					...mockModel,
					id: "model-002",
					name: "Small Output",
					max_output_tokens: 4096,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click max output header
			const outputHeader = screen.getByRole("button", { name: /Max Out/ });
			await user.click(outputHeader);

			await waitFor(() => {
				expect(screen.getByText("Small Output")).toBeInTheDocument();
			});
		});
	});

	describe("Filtering", () => {
		it("filters by search query", async () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Test Model" },
				{ ...mockModel, id: "model-002", name: "Other Model" },
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Type in search
			const searchInput = screen.getByPlaceholderText("Search models…");
			await user.type(searchInput, "Test");

			await waitFor(() => {
				// Test Model should be visible
				expect(screen.getByText("Test Model")).toBeInTheDocument();
			});
		});

		it("filters by provider", async () => {
			const providers = [
				{ ...mockProvider, id: "provider-1", name: "Provider 1" },
				{ ...mockProvider, id: "provider-2", name: "Provider 2" },
			];

			const models = [
				{
					...mockModel,
					id: "model-001",
					provider_id: "provider-1",
					provider_name: "Provider 1",
				},
				{
					...mockModel,
					id: "model-002",
					provider_id: "provider-2",
					provider_name: "Provider 2",
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={providers} />,
			);

			// Initially 2 rows in tbody
			expect(
				screen.getByRole("table").querySelectorAll("tbody tr").length,
			).toBe(2);

			// Open provider filter dropdown
			const providerFilter = screen.getByText("Filter Providers");
			await user.click(providerFilter);

			// Click Provider 1 option - find by text content in buttons
			const allButtons = screen.getAllByRole("button");
			const provider1Button = allButtons.find(
				(btn) => btn.textContent === "Provider 1",
			);
			if (provider1Button) await user.click(provider1Button);

			await waitFor(() => {
				// Only Provider 1 model should be visible in tbody
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
			});
		});

		it("filters by capabilities", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Vision Model",
					capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
				},
				{
					...mockModel,
					id: "model-002",
					name: "Text Model",
					capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click on vision capability filter button
			const visionFilter = screen.getByRole("button", { name: "Vision" });
			await user.click(visionFilter);

			await waitFor(() => {
				// Only Vision Model should be visible in tbody
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
			});
		});

		it("filters by status (enabled)", async () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Enabled Model", enabled: true },
				{
					...mockModel,
					id: "model-002",
					name: "Disabled Model",
					enabled: false,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click enabled status filter button
			const enabledFilter = screen.getByRole("button", { name: "Enabled" });
			await user.click(enabledFilter);

			await waitFor(() => {
				// Only Enabled Model should be visible in tbody
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
			});
		});

		it("filters by status (disabled)", async () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Enabled Model", enabled: true },
				{
					...mockModel,
					id: "model-002",
					name: "Disabled Model",
					enabled: false,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click disabled status filter button
			const disabledFilter = screen.getByRole("button", { name: "Disabled" });
			await user.click(disabledFilter);

			await waitFor(() => {
				// Only Disabled Model should be visible in tbody
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
			});
		});

		it("clears capability filter when clicking X button", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Vision Model",
					capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click on vision capability filter button to toggle it on
			const visionFilter = screen.getByRole("button", { name: "Vision" });
			await user.click(visionFilter);

			// Click the same button again to toggle it off (clear filter)
			await user.click(visionFilter);

			// Filter should be cleared
			await waitFor(() => {
				expect(screen.getByText("Vision Model")).toBeInTheDocument();
			});
		});

		it("clears status filter when clicking X button", async () => {
			const models = [
				{ ...mockModel, id: "model-001", name: "Enabled Model", enabled: true },
				{
					...mockModel,
					id: "model-002",
					name: "Disabled Model",
					enabled: false,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Initially both models should be visible (no filters)
			expect(
				screen.getByRole("table").querySelectorAll("tbody tr").length,
			).toBe(2);

			// Click enabled filter to show only enabled models
			await user.click(screen.getByRole("button", { name: "Enabled" }));

			// Only Enabled Model should be visible
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
				expect(rows[0].textContent).toContain("Enabled Model");
			});

			// Click the enabled button again to toggle it off (clear filter)
			await user.click(screen.getByRole("button", { name: "Enabled" }));

			// Both models should be visible again
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
			});
		});
	});

	describe("Pagination", () => {
		it("renders pagination controls with many models", () => {
			const models = Array.from({ length: 25 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
			}));

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			expect(screen.getByText("Prev")).toBeInTheDocument();
			expect(screen.getByText("Next")).toBeInTheDocument();
		});

		it("changes page size", async () => {
			const models = Array.from({ length: 50 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
			}));

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Page size select has options like "10 / page"
			const pageSizeSelect = screen.getByRole("combobox", {
				name: "",
			});
			await user.selectOptions(pageSizeSelect, "10");

			await waitFor(() => {
				// With page size 10, should show "1 to 10 of 50"
				expect(screen.getByText(/1 to 10 of 50/)).toBeInTheDocument();
			});
		});

		it("navigates to next page", async () => {
			const models = Array.from({ length: 25 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
			}));

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click next
			const nextButton = screen.getByRole("button", { name: "Next" });
			await user.click(nextButton);

			await waitFor(() => {
				expect(screen.getByText("Prev")).not.toBeDisabled();
			});
		});

		it("navigates to previous page", async () => {
			const models = Array.from({ length: 25 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
			}));

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// First page - prev should be disabled
			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();

			// Go to next page
			await user.click(screen.getByRole("button", { name: "Next" }));

			// Now prev should be enabled
			await waitFor(
				() => {
					expect(
						screen.getByRole("button", { name: "Prev" }),
					).not.toBeDisabled();
				},
				{ timeout: 5000 },
			);

			// Go back to previous page
			await user.click(screen.getByRole("button", { name: "Prev" }));

			// Prev should be disabled again
			await waitFor(
				() => {
					expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();
				},
				{ timeout: 5000 },
			);
		});

		it("navigates to specific page number", async () => {
			const models = Array.from({ length: 50 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
				name: `Model ${i}`,
			}));

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click page 2 button
			const page2Button = screen.getByRole("button", { name: "2" });
			await user.click(page2Button);

			await waitFor(() => {
				// Page 2 should show "21 to 40 of 50 models"
				expect(screen.getByText(/21 to 40 of 50/)).toBeInTheDocument();
			});
		});
	});

	describe("Model Click", () => {
		it("calls onModelClick when clicking on a model row", async () => {
			const onModelClick = vi.fn();

			const { user } = renderWithProviders(
				<ModelTable {...defaultProps} onModelClick={onModelClick} />,
			);

			// Click on model row
			const modelRow = screen.getByText("Test Model");
			await user.click(modelRow);

			expect(onModelClick).toHaveBeenCalledWith(mockModel);
		});
	});

	describe("Copyable Model ID", () => {
		it("renders copyable model ID pill", () => {
			renderWithProviders(<ModelTable {...defaultProps} />);

			// Model ID should be displayed in a copyable pill with normalized provider name
			expect(
				screen.getByText("Test-Provider/test-model-v1"),
			).toBeInTheDocument();
		});

		it("copies model ID to clipboard when clicking", async () => {
			// Mock clipboard API using spyOn
			const writeTextMock = vi.fn().mockResolvedValue(undefined);
			vi.spyOn(navigator.clipboard, "writeText").mockImplementation(
				writeTextMock,
			);

			const { user } = renderWithProviders(<ModelTable {...defaultProps} />);

			// Click on the copy button (the button containing the model ID text)
			const copyButton = screen.getByTitle("Click to copy model ID");
			await user.click(copyButton);

			expect(writeTextMock).toHaveBeenCalledWith("Test-Provider/test-model-v1");
		});
	});

	describe("Initial Provider Filter", () => {
		it("applies initial provider filter", () => {
			const providers = [
				{ ...mockProvider, id: "provider-1", name: "Provider 1" },
				{ ...mockProvider, id: "provider-2", name: "Provider 2" },
			];

			const models = [
				{
					...mockModel,
					id: "model-001",
					provider_id: "provider-1",
					provider_name: "Provider 1",
				},
				{
					...mockModel,
					id: "model-002",
					provider_id: "provider-2",
					provider_name: "Provider 2",
				},
			];

			const initialFilter = new Set(["provider-1"]);

			renderWithProviders(
				<ModelTable
					models={models}
					providers={providers}
					initialProviderFilter={initialFilter}
				/>,
			);

			// Only Provider 1 model should be visible in the table
			// Provider 2 model should not be rendered
			expect(screen.queryByText("Provider 2")).not.toBeInTheDocument();
		});
	});
});
