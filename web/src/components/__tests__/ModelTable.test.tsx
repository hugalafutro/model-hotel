import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { formatDate } from "../../utils/format";
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
					providerFilter="provider-1"
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

			// Status badge in the status column - scope to table body
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

			// Status badge in the status column - scope to table body
			const table = screen.getByRole("table");
			const disabledBadges = within(table).getAllByText("Disabled");
			expect(disabledBadges.length).toBeGreaterThan(0);
		});

		it("shows discovery tooltip on badge for discovery-disabled model", () => {
			const discoveryDisabledModel = {
				...mockModel,
				enabled: false,
				disabled_manually: false,
			};

			renderWithProviders(
				<ModelTable
					models={[discoveryDisabledModel]}
					providers={[mockProvider]}
				/>,
			);

			const badge = screen.getByTestId("disabled-by-discovery");
			// Tooltip mentions the model's last_seen_at date in the user's locale.
			expect(badge).toHaveAttribute(
				"title",
				expect.stringContaining(formatDate(mockModel.last_seen_at)),
			);
		});

		it("does not show discovery tooltip for manually disabled model", () => {
			const manuallyDisabledModel = {
				...mockModel,
				enabled: false,
				disabled_manually: true,
			};

			renderWithProviders(
				<ModelTable
					models={[manuallyDisabledModel]}
					providers={[mockProvider]}
				/>,
			);

			expect(
				screen.queryByTestId("disabled-by-discovery"),
			).not.toBeInTheDocument();
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

			// Exactly one pagination bar: a copy-paste once rendered it twice,
			// and this test's getAllByRole leniency let that slip through.
			expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
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

		it("filters by provider", () => {
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

			const { rerender } = renderWithProviders(
				<ModelTable models={models} providers={providers} />,
			);

			// Initially 2 rows in tbody
			expect(
				screen.getByRole("table").querySelectorAll("tbody tr").length,
			).toBe(2);

			// Page-owned provider filter narrows the table
			rerender(
				<ModelTable
					models={models}
					providers={providers}
					providerFilter="provider-1"
				/>,
			);

			expect(
				screen.getByRole("table").querySelectorAll("tbody tr").length,
			).toBe(1);
			expect(screen.queryByText("Provider 2")).not.toBeInTheDocument();
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

		it("sorts by status (enabled first ascending)", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Disabled Model",
					enabled: false,
				},
				{
					...mockModel,
					id: "model-002",
					name: "Enabled Model",
					enabled: true,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			const statusHeader = screen.getByRole("button", { name: /Status/ });
			await user.click(statusHeader);

			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
				expect(rows[0].textContent).toContain("Enabled Model");
				expect(rows[1].textContent).toContain("Disabled Model");
			});
		});

		it("sorts by status (disabled first descending)", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Disabled Model",
					enabled: false,
				},
				{
					...mockModel,
					id: "model-002",
					name: "Enabled Model",
					enabled: true,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			const statusHeader = screen.getByRole("button", { name: /Status/ });
			await user.click(statusHeader);
			await user.click(statusHeader);

			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
				expect(rows[0].textContent).toContain("Disabled Model");
				expect(rows[1].textContent).toContain("Enabled Model");
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

		it("toggles status sort direction on repeated clicks", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Disabled Model",
					enabled: false,
				},
				{
					...mockModel,
					id: "model-002",
					name: "Enabled Model",
					enabled: true,
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Initially both models should be visible
			expect(
				screen.getByRole("table").querySelectorAll("tbody tr").length,
			).toBe(2);

			const statusHeader = screen.getByRole("button", { name: /Status/ });

			// Click once: ascending (enabled first)
			await user.click(statusHeader);
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows[0].textContent).toContain("Enabled Model");
			});

			// Click again: descending (disabled first)
			await user.click(statusHeader);
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows[0].textContent).toContain("Disabled Model");
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

			// Use getAllByText since "Prev"/"Next" may appear in multiple places
			const prevButtons = screen.getAllByRole("button", { name: "Prev" });
			const nextButtons = screen.getAllByRole("button", { name: "Next" });
			expect(prevButtons.length).toBeGreaterThan(0);
			expect(nextButtons.length).toBeGreaterThan(0);
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
			const pageSizeSelect = screen.getAllByRole("combobox", {
				name: "",
			})[0];
			await user.selectOptions(pageSizeSelect, "10");

			await waitFor(() => {
				// With page size 10, should show pagination text with "1 to 10 of 50"
				// Text may be split, so check for key patterns
				const paginationText = document.querySelector(".text-sm.text-gray-500");
				expect(paginationText?.textContent).toMatch(/1 to 10 of 50/);
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

			// Use getAllByRole since "Next" may appear in multiple pagination controls
			const nextButtons = screen.getAllByRole("button", { name: "Next" });
			await user.click(nextButtons[0]);

			await waitFor(() => {
				// Prev button should be enabled after navigating
				const prevButtons = screen.getAllByRole("button", { name: "Prev" });
				expect(prevButtons.some((btn) => !btn.hasAttribute("disabled"))).toBe(
					true,
				);
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
			await waitFor(() => {
				const prevButtons = screen.getAllByRole("button", { name: "Prev" });
				expect(prevButtons.every((btn) => btn.hasAttribute("disabled"))).toBe(
					true,
				);
			});

			// Go to next page
			const nextButtons = screen.getAllByRole("button", { name: "Next" });
			await user.click(nextButtons[0]);

			// Now prev should be enabled
			await waitFor(
				() => {
					const prevButtons = screen.getAllByRole("button", { name: "Prev" });
					expect(prevButtons.some((btn) => !btn.hasAttribute("disabled"))).toBe(
						true,
					);
				},
				{ timeout: 10000 },
			);

			// Go back to previous page
			const prevButtons2 = screen.getAllByRole("button", { name: "Prev" });
			const enabledPrevBtn = prevButtons2.find(
				(btn) => !btn.hasAttribute("disabled"),
			);
			if (!enabledPrevBtn) throw new Error("No enabled Prev button found");
			await user.click(enabledPrevBtn);

			// Prev should be disabled again
			await waitFor(() => {
				const prevButtonsFinal = screen.getAllByRole("button", {
					name: "Prev",
				});
				expect(
					prevButtonsFinal.every((btn) => btn.hasAttribute("disabled")),
				).toBe(true);
			});
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
			const page2Button = screen.getAllByRole("button", { name: "2" })[0];
			await user.click(page2Button);

			await waitFor(() => {
				// Page 2 should show "21 to 40 of 50 models"
				// ModelTable renders two PaginationBar instances (top + bottom),
				// so the same text appears twice — use getAllByText
				expect(screen.getAllByText(/21 to 40 of 50/).length).toBeGreaterThan(0);
			});
		});
	});

	describe("Model Click", () => {
		it("calls onModelClick when clicking on a model row", async () => {
			const onModelClick = vi.fn();

			const { user } = renderWithProviders(
				<ModelTable {...defaultProps} onModelClick={onModelClick} />,
			);

			// Click the row itself: the model name is now a copy pill that stops
			// propagation, so clicking the name copies rather than opening.
			const modelRow = screen.getByText("Test Model").closest("tr");
			await user.click(modelRow as HTMLElement);

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

			// Click on the copy button (aria-label stays short for accessibility)
			const copyButton = screen.getByRole("button", {
				name: "Click to copy model ID",
			});
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

			renderWithProviders(
				<ModelTable
					models={models}
					providers={providers}
					providerFilter="provider-1"
				/>,
			);

			// Only Provider 1 model should be visible in the table
			// Provider 2 model should not be rendered
			expect(screen.queryByText("Provider 2")).not.toBeInTheDocument();
		});
	});

	describe("Manually Disabled Status", () => {
		it("renders Manually Disabled status badge", () => {
			const manuallyDisabledModel = {
				...mockModel,
				enabled: true,
				disabled_manually: true,
			};

			renderWithProviders(
				<ModelTable
					models={[manuallyDisabledModel]}
					providers={[mockProvider]}
				/>,
			);

			// Verify "Manually Disabled" text appears
			const statusBadge = screen.getByText("Manually Disabled");
			expect(statusBadge).toBeInTheDocument();

			// Verify the badge has warning styling
			// The text is in the inner <span class="badge-text">, check parent's class
			expect(statusBadge.parentElement?.className).toContain(
				"ui-badge-warning",
			);
		});
	});

	describe("Capability Filter Clear All", () => {
		it("clears all capability filters when clicking clear all button", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Vision Model",
					capabilities: '{"streaming":true,"vision":true,"audio_input":true}',
				},
				{
					...mockModel,
					id: "model-002",
					name: "Audio Model",
					capabilities: '{"streaming":true,"vision":false,"audio_input":true}',
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click Vision filter button
			const visionFilter = screen.getByRole("button", { name: "Vision" });
			await user.click(visionFilter);

			// Click Audio filter button
			const audioFilter = screen.getByRole("button", { name: "Audio" });
			await user.click(audioFilter);

			// Verify both filters are active: only model-001 matches both Vision AND Audio (AND-logic)
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(1);
			});

			// Click the ✕ clear button
			const clearButton = screen.getByText("✕");
			await user.click(clearButton);

			// Verify both filters are cleared (all models visible again)
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
			});

			// Verify ✕ button disappears
			expect(screen.queryByText("✕")).not.toBeInTheDocument();
		});
	});

	describe("Empty State with Null Models", () => {
		it("renders empty state when models is null", () => {
			renderWithProviders(
				<ModelTable
					models={null as unknown as []}
					providers={[mockProvider]}
				/>,
			);

			expect(
				screen.getByText(
					"No models discovered yet. Add a provider and discover models.",
				),
			).toBeInTheDocument();
		});
	});

	describe("Sort by Provider Name", () => {
		it("sorts by provider name", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Model A",
					provider_name: "Zeta Provider",
				},
				{
					...mockModel,
					id: "model-002",
					name: "Model Z",
					provider_name: "Alpha Provider",
				},
			];

			const { user } = renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Click Provider header - use aria-label to find the sort button specifically
			const providerHeader = screen.getByRole("button", {
				name: "Sort by Provider",
			});
			await user.click(providerHeader);

			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
				// Alpha Provider should appear first
				expect(rows[0].textContent).toContain("Alpha Provider");
				expect(rows[1].textContent).toContain("Zeta Provider");
			});
		});
	});

	describe("Capabilities Column Rendering", () => {
		it("renders capabilities column with badges", async () => {
			const models = [
				{
					...mockModel,
					id: "model-001",
					name: "Few Caps Model",
					capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
				},
				{
					...mockModel,
					id: "model-002",
					name: "Many Caps Model",
					capabilities: '{"streaming":true,"vision":true,"tools":true}',
				},
			];

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			// Capabilities column header is not sortable (plain <th>, no SortableHeader)
			await waitFor(() => {
				const rows = screen.getByRole("table").querySelectorAll("tbody tr");
				expect(rows.length).toBe(2);
				// Both models should be visible with their capability badges
				expect(rows[0].textContent).toContain("Few Caps Model");
				expect(rows[1].textContent).toContain("Many Caps Model");
				expect(rows[1].textContent).toContain("Vision");
			});
		});
	});

	describe("Fallback to proxyModelID", () => {
		it("falls back to proxyModelID when model name is missing", () => {
			const modelWithoutName = {
				...mockModel,
				name: "",
			};

			renderWithProviders(
				<ModelTable models={[modelWithoutName]} providers={[mockProvider]} />,
			);

			// Verify it displays the proxyModelID format in the name pill specifically
			// (font-medium; the id CopyablePill below uses font-mono / model-id-text)
			const table = screen.getByRole("table");
			const nameSpan = table.querySelector("tbody span.font-medium");
			expect(nameSpan?.textContent).toBe("Test-Provider/test-model-v1");
		});
	});

	describe("Row Click Without Callback", () => {
		it("does not throw when clicking row without onModelClick callback", async () => {
			const { user } = renderWithProviders(
				<ModelTable models={[mockModel]} providers={[mockProvider]} />,
			);

			// Click on model row - should not throw
			const modelRow = screen.getByText("Test Model");
			await expect(user.click(modelRow)).resolves.not.toThrow();
		});
	});

	describe("Search by Display Name", () => {
		it("searches by display_name field", async () => {
			const modelWithDisplayName = {
				...mockModel,
				name: "Plain Name",
				display_name: "Fancy Display Name",
			};

			const { user } = renderWithProviders(
				<ModelTable
					models={[modelWithDisplayName]}
					providers={[mockProvider]}
				/>,
			);

			// Type "Fancy" in search
			const searchInput = screen.getByPlaceholderText("Search models…");
			await user.type(searchInput, "Fancy");

			await waitFor(() => {
				// Model should still be visible
				expect(screen.getByText("Plain Name")).toBeInTheDocument();
			});
		});
	});

	describe("Search by Model ID", () => {
		it("searches by model_id field", async () => {
			const modelWithUniqueID = {
				...mockModel,
				name: "Generic",
				model_id: "unique-model-id-xyz",
			};

			const { user } = renderWithProviders(
				<ModelTable models={[modelWithUniqueID]} providers={[mockProvider]} />,
			);

			// Type "unique-model-id" in search
			const searchInput = screen.getByPlaceholderText("Search models…");
			await user.type(searchInput, "unique-model-id");

			await waitFor(() => {
				// Model should be visible
				expect(screen.getByText("Generic")).toBeInTheDocument();
			});
		});
	});

	describe("Pagination Reset on Provider Filter Change", () => {
		it("resets to page 1 when provider filter changes", async () => {
			// Create 25+ models for pagination
			const models = Array.from({ length: 25 }, (_, i) => ({
				...mockModel,
				id: `model-${i}`,
				model_id: `model-${i}`,
				name: `Model ${i}`,
				provider_id: i < 15 ? "provider-1" : "provider-2",
				provider_name: i < 15 ? "Provider 1" : "Provider 2",
			}));

			const providers = [
				{ ...mockProvider, id: "provider-1", name: "Provider 1" },
				{ ...mockProvider, id: "provider-2", name: "Provider 2" },
			];

			const { user, rerender } = renderWithProviders(
				<ModelTable models={models} providers={providers} />,
			);

			// Navigate to page 2
			const page2Button = screen.getAllByRole("button", { name: "2" })[0];
			await user.click(page2Button);

			// Verify we're on page 2 (pagination shows "21 to 25 of 25 models")
			await waitFor(() => {
				expect(screen.getAllByText(/21 to 25 of 25/).length).toBeGreaterThan(0);
			});

			// Page-owned provider filter changes
			rerender(
				<ModelTable
					models={models}
					providers={providers}
					providerFilter="provider-1"
				/>,
			);

			// Verify we're back on page 1 (pagination shows "1 to 15 of 15 models")
			// Anchor "1" to start to avoid matching "21 of 15" (broken state)
			// Two PaginationBar instances so use getAllByText
			await waitFor(() => {
				const paginationTexts = screen.getAllByText((content) => {
					return /^1\b/.test(content) && content.includes("of 15");
				});
				expect(paginationTexts.length).toBeGreaterThan(0);
			});
		});

		it("does not render delete disabled button when onDeleteDisabled is not provided", () => {
			const disabledModel = {
				...mockModel,
				id: "model-disabled-1",
				enabled: false,
			};
			const models = [mockModel, disabledModel];

			renderWithProviders(
				<ModelTable models={models} providers={[mockProvider]} />,
			);

			expect(
				screen.queryByRole("button", { name: /delete.*disabled/i }),
			).not.toBeInTheDocument();
		});

		it("does not render delete disabled button when all models are enabled", () => {
			const onDeleteDisabled = vi.fn();
			const models = [mockModel, { ...mockModel, id: "model-002" }];

			renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider]}
					onDeleteDisabled={onDeleteDisabled}
				/>,
			);

			expect(
				screen.queryByRole("button", { name: /delete.*disabled/i }),
			).not.toBeInTheDocument();
		});

		it("renders delete disabled button with correct count when there are disabled models", () => {
			const onDeleteDisabled = vi.fn();
			const disabledModel = {
				...mockModel,
				id: "model-disabled-1",
				enabled: false,
			};
			const models = [mockModel, disabledModel];

			renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider]}
					onDeleteDisabled={onDeleteDisabled}
				/>,
			);

			expect(screen.getByText("Delete 1 disabled")).toBeInTheDocument();
		});

		it("opens confirm dialog when delete disabled button is clicked", async () => {
			const onDeleteDisabled = vi.fn();
			const disabledModel = {
				...mockModel,
				id: "model-disabled-1",
				enabled: false,
			};
			const models = [mockModel, disabledModel];

			const { user } = renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider]}
					onDeleteDisabled={onDeleteDisabled}
				/>,
			);

			await user.click(screen.getByText("Delete 1 disabled"));

			expect(screen.getByText("Delete Disabled Models")).toBeInTheDocument();
		});

		it("calls onDeleteDisabled with disabled model IDs on confirm", async () => {
			const onDeleteDisabled = vi.fn();
			const disabledModel = {
				...mockModel,
				id: "model-disabled-1",
				enabled: false,
			};
			const models = [mockModel, disabledModel];

			const { user } = renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider]}
					onDeleteDisabled={onDeleteDisabled}
				/>,
			);

			await user.click(screen.getByText("Delete 1 disabled"));
			await user.click(screen.getByText("Delete"));

			await waitFor(() => {
				expect(onDeleteDisabled).toHaveBeenCalledWith(["model-disabled-1"]);
			});
		});

		it("closes confirm dialog on cancel", async () => {
			const onDeleteDisabled = vi.fn();
			const disabledModel = {
				...mockModel,
				id: "model-disabled-1",
				enabled: false,
			};
			const models = [mockModel, disabledModel];

			const { user } = renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider]}
					onDeleteDisabled={onDeleteDisabled}
				/>,
			);

			await user.click(screen.getByText("Delete 1 disabled"));
			expect(screen.getByText("Delete Disabled Models")).toBeInTheDocument();

			await user.click(screen.getByText("Cancel"));

			await waitFor(() => {
				expect(
					screen.queryByText("Delete Disabled Models"),
				).not.toBeInTheDocument();
			});
		});

		it("only includes disabled models from filtered view, not all models", async () => {
			const onDeleteDisabled = vi.fn();
			const otherProvider = {
				...mockProvider,
				id: "provider-002",
				name: "Other Provider",
			};
			const disabledInScope = {
				...mockModel,
				id: "model-disabled-scope",
				provider_id: "provider-001",
				enabled: false,
			};
			const disabledOutOfScope = {
				...mockModel,
				id: "model-disabled-other",
				provider_id: "provider-002",
				name: "Other Disabled",
				provider_name: "Other Provider",
				enabled: false,
			};
			const models = [mockModel, disabledInScope, disabledOutOfScope];

			const { user } = renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider, otherProvider]}
					onDeleteDisabled={onDeleteDisabled}
					providerFilter="provider-001"
				/>,
			);

			// Button should show 1 disabled (only the one in filtered view)
			expect(screen.getByText("Delete 1 disabled")).toBeInTheDocument();

			await user.click(screen.getByText("Delete 1 disabled"));
			await user.click(screen.getByText("Delete"));

			// Should only pass the in-scope disabled model ID
			await waitFor(() => {
				expect(onDeleteDisabled).toHaveBeenCalledWith(["model-disabled-scope"]);
			});
		});

		it("does not show delete button when disabled models are outside filter scope", async () => {
			const onDeleteDisabled = vi.fn();
			const otherProvider = {
				...mockProvider,
				id: "provider-002",
				name: "Other Provider",
			};
			const disabledOutOfScope = {
				...mockModel,
				id: "model-disabled-other",
				provider_id: "provider-002",
				name: "Other Disabled",
				provider_name: "Other Provider",
				enabled: false,
			};
			const models = [mockModel, disabledOutOfScope];

			renderWithProviders(
				<ModelTable
					models={models}
					providers={[mockProvider, otherProvider]}
					onDeleteDisabled={onDeleteDisabled}
					providerFilter="provider-001"
				/>,
			);

			// No disabled models in the filtered view, so button should not appear
			expect(
				screen.queryByRole("button", { name: /delete.*disabled/i }),
			).not.toBeInTheDocument();
		});
	});
});
