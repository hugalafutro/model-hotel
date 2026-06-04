import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Model } from "../../api/types";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { renderWithProviders } from "../../test/utils";
import { ProviderModelsModal } from "../ProviderModelsModal";

describe("ProviderModelsModal", () => {
	const defaultProps = {
		provider: mockProvider,
		models: [mockModel],
		onClose: vi.fn(),
	};

	it("renders modal with provider name", () => {
		renderWithProviders(<ProviderModelsModal {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("shows model count badge with plural form", () => {
		const models: Model[] = [
			{ ...mockModel, id: "model-001" },
			{ ...mockModel, id: "model-002" },
		];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// Badge renders count and plural separately
		// t("components.providerModelsModal.modelCount", { count: 2 }) resolves to "models" (the _other form)
		const badge = document.querySelector("span.rounded-full");
		expect(badge).toBeInTheDocument();
		expect(badge?.textContent).toMatch(/2\s+models/);
	});

	it("shows 1 model when single model", () => {
		renderWithProviders(<ProviderModelsModal {...defaultProps} />);

		// Use getAllByText since "1 model" appears in multiple places (badge + pagination text)
		const modelTexts = screen.getAllByText("1 model");
		expect(modelTexts.length).toBeGreaterThan(0);
	});

	it("shows N models when multiple models", () => {
		const models: Model[] = [
			{ ...mockModel, id: "model-001" },
			{ ...mockModel, id: "model-002" },
			{ ...mockModel, id: "model-003" },
			{ ...mockModel, id: "model-004" },
			{ ...mockModel, id: "model-005" },
		];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// Badge renders count and plural: "5 models"
		const badge = document.querySelector("span.rounded-full");
		expect(badge).toBeInTheDocument();
		expect(badge?.textContent).toMatch(/5\s+models/);
	});

	it("filters models by provider_id", () => {
		const otherProviderModel: Model = {
			...mockModel,
			id: "model-other",
			provider_id: "provider-other",
			provider_name: "Other Provider",
		};

		const models: Model[] = [mockModel, otherProviderModel];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// Only the model belonging to the provider should be in the table
		// Check that Test Model is present in the table body
		const table = screen.getByRole("table");
		expect(within(table).getByText("Test Model")).toBeInTheDocument();
	});

	it("does not show models from other providers", () => {
		const otherProviderModel: Model = {
			...mockModel,
			id: "model-other",
			provider_id: "provider-other",
			provider_name: "Other Provider",
			name: "Other Model",
		};

		const models: Model[] = [mockModel, otherProviderModel];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// Other Model should not be in the table
		const table = screen.getByRole("table");
		expect(within(table).queryByText("Other Model")).not.toBeInTheDocument();
	});

	it("calls onClose when modal close button clicked", async () => {
		const onClose = vi.fn();
		const { user } = renderWithProviders(
			<ProviderModelsModal {...defaultProps} onClose={onClose} />,
		);

		// Find and click the close button (X button in modal header with aria-label "Close")
		const closeButton = screen.getByRole("button", { name: "Close" });
		await user.click(closeButton);

		await waitFor(() => {
			expect(onClose).toHaveBeenCalled();
		});
	});

	it("renders ModelTable with filtered models", () => {
		const models: Model[] = [
			{ ...mockModel, id: "model-001" },
			{ ...mockModel, id: "model-002", provider_id: "provider-002" },
		];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// ModelTable should render with the filtered models
		const table = screen.getByRole("table");
		expect(table).toBeInTheDocument();

		// Should show the model belonging to the provider
		expect(screen.getByText("Test Model")).toBeInTheDocument();
	});

	it("handles zero provider models", () => {
		const otherProviderModel: Model = {
			...mockModel,
			id: "model-other",
			provider_id: "provider-other",
			provider_name: "Other Provider",
		};

		const models: Model[] = [otherProviderModel];

		renderWithProviders(
			<ProviderModelsModal {...defaultProps} models={models} />,
		);

		// Badge renders count and plural: "0 models" (t key with count=0 uses _other form)
		const badge = document.querySelector("span.rounded-full");
		expect(badge).toBeInTheDocument();
		expect(badge?.textContent).toMatch(/0\s+models/);

		// Table should show empty state
		expect(
			screen.getByText(
				"No models discovered yet. Add a provider and discover models.",
			),
		).toBeInTheDocument();
	});

	it("modal has scrollable and maxWidth classes", () => {
		renderWithProviders(<ProviderModelsModal {...defaultProps} />);

		// Find the inner modal content div (not the outer dialog overlay)
		const modalContent = screen.getByRole("dialog").querySelector(".max-w-6xl");
		expect(modalContent).toBeInTheDocument();

		// Check for scrollable - the modal content should be scrollable
		// The scrollable prop adds overflow-y-auto to the content container
		const contentContainer = screen
			.getByRole("dialog")
			.querySelector(".overflow-y-auto");
		expect(contentContainer).toBeInTheDocument();
	});
});
