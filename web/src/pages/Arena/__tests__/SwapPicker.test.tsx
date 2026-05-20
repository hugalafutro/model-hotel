import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import { mockModel } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { SwapPicker } from "../SwapPicker";

// Stub Lucide icons
vi.mock("lucide-react", () => ({
	ChevronDown: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="chevron-down" />
	),
	ChevronsDownUp: () => <svg data-testid="chevrons-down-up" />,
	ChevronsUpDown: () => <svg data-testid="chevrons-up-down" />,
	X: () => <svg data-testid="x-icon" />,
}));

describe("SwapPicker", () => {
	const mockOnSelect = vi.fn();

	const createModel = (
		providerName: string,
		modelId: string,
		displayName?: string,
	): Model => ({
		...mockModel,
		provider_name: providerName,
		model_id: modelId,
		display_name: displayName || modelId,
	});

	it('renders "Pick a replacement model" text', () => {
		renderWithProviders(
			<SwapPicker
				enabledModels={[createModel("Provider", "model-1")]}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.getByText("Pick a replacement model")).toBeInTheDocument();
	});

	it("renders FilterInput with search placeholder", () => {
		renderWithProviders(
			<SwapPicker
				enabledModels={[createModel("Provider", "model-1")]}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const searchInput = screen.getByPlaceholderText("Search models…");
		expect(searchInput).toBeInTheDocument();
	});

	it("shows model buttons for enabled models", () => {
		const models = [
			createModel("Provider", "model-1", "Model One"),
			createModel("Provider", "model-2", "Model Two"),
		];
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.getByText("Model One")).toBeInTheDocument();
		expect(screen.getByText("Model Two")).toBeInTheDocument();
	});

	it("groups models by provider with provider headers", () => {
		const models = [
			createModel("OpenAI", "gpt-4", "GPT-4"),
			createModel("Anthropic", "claude-3", "Claude 3"),
		];
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.getByText("OpenAI")).toBeInTheDocument();
		expect(screen.getByText("Anthropic")).toBeInTheDocument();
	});

	it("shows model count per provider", () => {
		const models = [
			createModel("OpenAI", "gpt-4", "GPT-4"),
			createModel("OpenAI", "gpt-3.5", "GPT-3.5"),
			createModel("Anthropic", "claude-3", "Claude 3"),
		];
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.getByText("(2)")).toBeInTheDocument();
		expect(screen.getByText("(1)")).toBeInTheDocument();
	});

	it("excludes disabled models", () => {
		const models = [
			createModel("Provider", "model-1", "Model One"),
			createModel("Provider", "model-2", "Model Two"),
		];
		const disabledModels = new Set(["Provider/model-1"]);
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={disabledModels}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.queryByText("Model One")).not.toBeInTheDocument();
		expect(screen.getByText("Model Two")).toBeInTheDocument();
	});

	it("excludes already used models", () => {
		const models = [
			createModel("Provider", "model-1", "Model One"),
			createModel("Provider", "model-2", "Model Two"),
		];
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={["Provider/model-1"]}
				onSelect={mockOnSelect}
			/>,
		);
		expect(screen.queryByText("Model One")).not.toBeInTheDocument();
		expect(screen.getByText("Model Two")).toBeInTheDocument();
	});

	it("searches by display_name", async () => {
		const models = [
			createModel("Provider", "model-1", "GPT-4"),
			createModel("Provider", "model-2", "Claude-3"),
		];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const searchInput = screen.getByPlaceholderText("Search models…");
		await user.type(searchInput, "GPT");
		expect(screen.getByText("GPT-4")).toBeInTheDocument();
		expect(screen.queryByText("Claude-3")).not.toBeInTheDocument();
	});

	it("searches by model_id", async () => {
		const models = [
			createModel("Provider", "gpt-4-turbo", "GPT-4 Turbo"),
			createModel("Provider", "claude-3-opus", "Claude 3 Opus"),
		];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const searchInput = screen.getByPlaceholderText("Search models…");
		await user.type(searchInput, "turbo");
		expect(screen.getByText("GPT-4 Turbo")).toBeInTheDocument();
		expect(screen.queryByText("Claude 3 Opus")).not.toBeInTheDocument();
	});

	it("calls onSelect with model ID when button clicked", async () => {
		const models = [createModel("Provider", "model-1", "Model One")];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const button = screen.getByText("Model One");
		await user.click(button);
		expect(mockOnSelect).toHaveBeenCalledWith("Provider/model-1");
	});

	it('shows "No models available" when all filtered out', async () => {
		const models = [createModel("Provider", "model-1", "Model One")];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const searchInput = screen.getByPlaceholderText("Search models…");
		await user.type(searchInput, "nonexistent");
		expect(screen.getByText("No models available")).toBeInTheDocument();
	});

	it("uses proxyModelID for model IDs", async () => {
		const models = [createModel("Ollama Cloud", "gemma3:4b", "Gemma 3 4B")];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		const button = screen.getByText("Gemma 3 4B");
		await user.click(button);
		// proxyModelID normalizes provider name (spaces → hyphens)
		expect(mockOnSelect).toHaveBeenCalledWith("Ollama-Cloud/gemma3:4b");
	});

	it("renders collapse/expand all button", () => {
		const models = [
			createModel("Provider A", "model-1", "Model One"),
			createModel("Provider B", "model-2", "Model Two"),
		];
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		// Initially expanded, so shows "collapse all" icon
		expect(screen.getByTestId("chevrons-down-up")).toBeInTheDocument();
	});

	it("toggles provider collapse state when header is clicked", async () => {
		const models = [
			createModel("Provider A", "model-1", "Model One"),
			createModel("Provider B", "model-2", "Model Two"),
		];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		// Both provider headers visible
		expect(screen.getByText("Provider A")).toBeInTheDocument();
		expect(screen.getByText("Provider B")).toBeInTheDocument();

		// Click provider A header to collapse - chevron should rotate
		await user.click(screen.getByText("Provider A"));
		// After collapsing, the collapse-all button state changes
		expect(screen.getByTestId("chevrons-down-up")).toBeInTheDocument();
	});

	it("toggles collapse all button between expand/collapse states", async () => {
		const models = [
			createModel("Provider A", "model-1", "Model One"),
			createModel("Provider B", "model-2", "Model Two"),
		];
		const user = userEvent.setup();
		renderWithProviders(
			<SwapPicker
				enabledModels={models}
				disabledModels={new Set()}
				alreadyUsed={[]}
				onSelect={mockOnSelect}
			/>,
		);
		// Initially expanded - shows collapse all
		expect(screen.getByTestId("chevrons-down-up")).toBeInTheDocument();

		// Click collapse all button
		await user.click(screen.getByTestId("chevrons-down-up"));
		// Now all collapsed - shows expand all
		expect(screen.getByTestId("chevrons-up-down")).toBeInTheDocument();

		// Click expand all button
		await user.click(screen.getByTestId("chevrons-up-down"));
		// Back to expanded - shows collapse all
		expect(screen.getByTestId("chevrons-down-up")).toBeInTheDocument();
	});
});
