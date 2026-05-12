import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import { mockModel } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { SwapPicker } from "../SwapPicker";

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
});
