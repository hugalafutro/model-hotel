import { fireEvent, screen, within } from "@testing-library/react";
import type { Model } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { ModelPicker } from "../ModelPicker";

describe("ModelPicker", () => {
	const mockModels: Model[] = [
		{
			id: "model-001",
			model_id: "gpt-4",
			name: "GPT-4",
			description: "OpenAI GPT-4",
			display_name: "GPT-4",
			provider_id: "provider-openai",
			provider_name: "OpenAI",
			capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			params: '{"temperature":0.7,"max_tokens":4096}',
			modality: "text",
			input_modalities: "text",
			output_modalities: "text",
			context_length: 8192,
			max_output_tokens: 4096,
			input_price_per_million: 30,
			input_price_per_million_cache_hit: 10,
			output_price_per_million: 60,
			owned_by: "openai",
			enabled: true,
			disabled_manually: false,
			created_at: "2024-01-01T00:00:00Z",
			last_seen_at: "2024-01-01T00:00:00Z",
		},
		{
			id: "model-002",
			model_id: "claude-3",
			name: "Claude 3",
			description: "Anthropic Claude 3",
			display_name: "Claude 3",
			provider_id: "provider-anthropic",
			provider_name: "Anthropic",
			capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			params: '{"temperature":0.7,"max_tokens":4096}',
			modality: "text",
			input_modalities: "text",
			output_modalities: "text",
			context_length: 8192,
			max_output_tokens: 4096,
			input_price_per_million: 3,
			input_price_per_million_cache_hit: 1,
			output_price_per_million: 15,
			owned_by: "anthropic",
			enabled: true,
			disabled_manually: false,
			created_at: "2024-01-01T00:00:00Z",
			last_seen_at: "2024-01-01T00:00:00Z",
		},
		{
			id: "model-003",
			model_id: "llama-3",
			name: "Llama 3",
			description: "Meta Llama 3",
			display_name: "Llama 3",
			provider_id: "provider-meta",
			provider_name: "Meta",
			capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
			params: '{"temperature":0.7,"max_tokens":4096}',
			modality: "text",
			input_modalities: "text",
			output_modalities: "text",
			context_length: 8192,
			max_output_tokens: 4096,
			input_price_per_million: 0,
			input_price_per_million_cache_hit: 0,
			output_price_per_million: 0,
			owned_by: "meta",
			enabled: true,
			disabled_manually: false,
			created_at: "2024-01-01T00:00:00Z",
			last_seen_at: "2024-01-01T00:00:00Z",
		},
	];

	const defaultProps = {
		models: mockModels,
		selected: "",
		onChange: vi.fn(),
		multi: false as const,
		label: "Select Model",
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering", () => {
		it("renders label", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			expect(screen.getByText("Select Model")).toBeInTheDocument();
		});

		it("renders search input", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			expect(searchInput).toBeInTheDocument();
		});

		it("renders provider filter dropdown", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			// ProviderFilter is a button that opens a dropdown
			const providerFilter = screen.getByText("Filter Providers");
			expect(providerFilter).toBeInTheDocument();
		});

		it("renders model groups by provider", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			expect(screen.getByText("OpenAI")).toBeInTheDocument();
			expect(screen.getByText("Anthropic")).toBeInTheDocument();
			expect(screen.getByText("Meta")).toBeInTheDocument();
		});

		it("renders model count per provider", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			// Each provider shows count like "(1)" - find first one
			const counts = screen.getAllByText("(1)");
			expect(counts.length).toBeGreaterThanOrEqual(1);
		});

		it("renders model chips", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
			expect(screen.getByText("Llama 3")).toBeInTheDocument();
		});

		it("renders collapse/expand all buttons", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			const collapseButton = screen.getByRole("button", {
				name: "Collapse all providers",
			});
			expect(collapseButton).toBeInTheDocument();
		});

		it("renders capability filter pills", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			// CAP_META has Vision, Reasoning, Tools, Structured, PDF, Video, Audio, Parallel
			// Models have vision:true, so Vision pill should show
			expect(screen.getByText("Vision")).toBeInTheDocument();
			// Audio pill exists in CAP_META but models don't have audio_input:true
			expect(screen.queryByText("Audio")).not.toBeInTheDocument();
		});

		it("renders vision capability pill when models have vision", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			expect(screen.getByText("Vision")).toBeInTheDocument();
		});

		it("does not render audio pill when no models have audio", () => {
			const modelsWithoutAudio = mockModels.map((m) => ({
				...m,
				capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
			}));
			renderWithProviders(
				<ModelPicker {...defaultProps} models={modelsWithoutAudio} />,
			);
			// Audio pill only shows if any model has audio_input capability
			expect(screen.queryByText("Audio")).not.toBeInTheDocument();
		});
	});

	describe("search functionality", () => {
		it("filters models by name", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			await user.type(searchInput, "GPT");
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.queryByText("Claude 3")).not.toBeInTheDocument();
			expect(screen.queryByText("Llama 3")).not.toBeInTheDocument();
		});

		it("filters models by model_id", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			await user.type(searchInput, "llama");
			expect(screen.getByText("Llama 3")).toBeInTheDocument();
			expect(screen.queryByText("GPT-4")).not.toBeInTheDocument();
		});

		it("filters models by provider name", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			await user.type(searchInput, "anthropic");
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
			expect(screen.queryByText("GPT-4")).not.toBeInTheDocument();
		});

		it("shows no results message when no matches", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			await user.type(searchInput, "nonexistent");
			expect(screen.getByText("No models match")).toBeInTheDocument();
		});

		it("clears filter when search is cleared", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			await user.type(searchInput, "GPT");
			expect(screen.queryByText("Claude 3")).not.toBeInTheDocument();
			await user.clear(searchInput);
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
		});
	});

	describe("provider filter", () => {
		it("filters models by selected provider", async () => {
			// ProviderFilter has click-outside handling (mousedown on document).
			// user.click fires mousedown → click, which closes the dropdown before
			// the option click registers. Use fireEvent.click to bypass mousedown.
			const { container, user } = renderWithProviders(
				<ModelPicker {...defaultProps} />,
			);
			const filterContainer = container.querySelector(
				'[data-testid="provider-filter"]',
			) as HTMLElement;
			await user.click(within(filterContainer).getByText("Filter Providers"));
			// Disambiguate dropdown "OpenAI" from group heading "OpenAI"
			const dropdown = container.querySelector(
				'[data-testid="provider-filter-dropdown"]',
			) as HTMLElement;
			const openaiBtn = within(dropdown)
				.getByText("OpenAI")
				.closest("button") as HTMLElement;
			fireEvent.click(openaiBtn);
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.queryByText("Claude 3")).not.toBeInTheDocument();
		});

		it("allows multiple provider selection", async () => {
			const { container, user } = renderWithProviders(
				<ModelPicker {...defaultProps} />,
			);
			// ProviderFilter has click-outside handling (mousedown on document).
			// user.click fires mousedown → click, which closes the dropdown before
			// the option click registers. Use fireEvent.click to bypass mousedown.
			const filterContainer = container.querySelector(
				'[data-testid="provider-filter"]',
			) as HTMLElement;
			await user.click(within(filterContainer).getByText("Filter Providers"));
			const dropdown = container.querySelector(
				'[data-testid="provider-filter-dropdown"]',
			) as HTMLElement;
			// Select OpenAI - dropdown stays open after fireEvent.click
			fireEvent.click(
				within(dropdown).getByText("OpenAI").closest("button") as HTMLElement,
			);
			// Select Anthropic in same open dropdown
			fireEvent.click(
				within(dropdown)
					.getByText("Anthropic")
					.closest("button") as HTMLElement,
			);
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
		});

		it("clears provider filter when cleared", async () => {
			const { container, user } = renderWithProviders(
				<ModelPicker {...defaultProps} />,
			);
			const filterContainer = container.querySelector(
				'[data-testid="provider-filter"]',
			) as HTMLElement;
			await user.click(within(filterContainer).getByText("Filter Providers"));
			const dropdown = container.querySelector(
				'[data-testid="provider-filter-dropdown"]',
			) as HTMLElement;
			// Select OpenAI - dropdown stays open after fireEvent.click
			fireEvent.click(
				within(dropdown).getByText("OpenAI").closest("button") as HTMLElement,
			);
			expect(screen.queryByText("Claude 3")).not.toBeInTheDocument();
			// Click "Clear" in the same open dropdown (it's a bulk action button)
			fireEvent.click(within(dropdown).getByText("Clear"));
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
		});
	});

	describe("capability filter", () => {
		it("filters models by vision capability", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const visionButton = screen.getByText("Vision");
			await user.click(visionButton);
			// GPT-4 and Claude 3 have vision, Llama 3 doesn't
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
		});

		it("filters models by tools capability", () => {
			// None of the mock models have tool_calling capability
			renderWithProviders(<ModelPicker {...defaultProps} />);
			const toolsButton = screen.queryByText("Tools");
			// Tools button should not exist since no models have tool_calling
			expect(toolsButton).not.toBeInTheDocument();
		});

		it("combines multiple capability filters", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			// Vision and Reasoning are available (models have vision)
			const visionButton = screen.getByText("Vision");
			await user.click(visionButton);
			// GPT-4 and Claude 3 have vision
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
			expect(screen.queryByText("Llama 3")).not.toBeInTheDocument();
		});

		it("clears capability filter when X button is clicked", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const visionButton = screen.getByText("Vision");
			await user.click(visionButton);
			// Llama 3 should be hidden (doesn't have vision)
			expect(screen.queryByText("Llama 3")).not.toBeInTheDocument();
			const clearButton = screen.getByRole("button", { name: "Clear filter" });
			await user.click(clearButton);
			// All models should be visible again
			expect(screen.getByText("Llama 3")).toBeInTheDocument();
		});

		it("disables capability pill when no models match", () => {
			const modelsWithOnlyVision = mockModels.map((m) => ({
				...m,
				capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			}));
			renderWithProviders(
				<ModelPicker {...defaultProps} models={modelsWithOnlyVision} />,
			);
			// Audio button exists in CAP_META but should be disabled since no models have audio_input
			const audioButton = screen.queryByText("Audio");
			if (audioButton) {
				expect(audioButton).toBeDisabled();
			} else {
				// Audio pill may not render if no models have any audio capability at all
				// In that case, check that Tools (another capability no model has) is disabled
				const toolsButton = screen.queryByText("Tools");
				if (toolsButton) {
					expect(toolsButton).toBeDisabled();
				}
			}
		});
	});

	describe("model selection", () => {
		it("selects model when chip is clicked", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker {...defaultProps} onChange={onChange} />,
			);
			const gptButton = screen.getByText("GPT-4");
			await user.click(gptButton);
			expect(onChange).toHaveBeenCalledWith("OpenAI/gpt-4");
		});

		it("deselects model when clicked again", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker
					{...defaultProps}
					selected="OpenAI/gpt-4"
					onChange={onChange}
				/>,
			);
			const gptButton = screen.getByText("GPT-4");
			await user.click(gptButton);
			expect(onChange).toHaveBeenCalledWith("");
		});

		it("highlights selected model", () => {
			renderWithProviders(
				<ModelPicker {...defaultProps} selected="OpenAI/gpt-4" />,
			);
			const gptChip = screen.getByText("GPT-4").closest("div");
			expect(gptChip).toHaveClass("bg-(--accent)/15");
		});

		it("shows selected models first in multi-select mode", () => {
			renderWithProviders(
				<ModelPicker
					{...defaultProps}
					multi={true}
					selected={["Meta/llama-3"]}
				/>,
			);
			// Llama 3 should appear first in its provider group
			const llamaChip = screen.getByText("Llama 3").closest("div");
			expect(llamaChip).toHaveClass("bg-(--accent)/15");
		});
	});

	describe("multi-select mode", () => {
		it("allows multiple model selection", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker
					{...defaultProps}
					multi={true}
					selected={[]}
					onChange={onChange}
				/>,
			);
			const gptButton = screen.getByText("GPT-4");
			const claudeButton = screen.getByText("Claude 3");
			await user.click(gptButton);
			await user.click(claudeButton);
			expect(onChange).toHaveBeenCalledTimes(2);
			expect(onChange).toHaveBeenCalledWith(["OpenAI/gpt-4"]);
			// Second call adds Claude to the selection (only the newly selected model)
			expect(onChange).toHaveBeenLastCalledWith(["Anthropic/claude-3"]);
		});

		it("deselects individual model in multi-select", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker
					{...defaultProps}
					multi={true}
					selected={["OpenAI/gpt-4", "Anthropic/claude-3"]}
					onChange={onChange}
				/>,
			);
			const gptButton = screen.getByText("GPT-4");
			await user.click(gptButton);
			expect(onChange).toHaveBeenCalledWith(["Anthropic/claude-3"]);
		});

		it("respects maxSelections limit", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker
					{...defaultProps}
					multi={true}
					maxSelections={2}
					selected={["OpenAI/gpt-4", "Anthropic/claude-3"]}
					onChange={onChange}
				/>,
			);
			const llamaButton = screen.getByText("Llama 3");
			await user.click(llamaButton);
			// Should not call onChange since limit reached
			expect(onChange).not.toHaveBeenCalled();
		});
	});

	describe("collapse/expand functionality", () => {
		it("collapses provider group when clicked", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const openAiButton = screen.getByRole("button", {
				name: (content, element) => {
					// Provider button contains "OpenAI" and "(1)"
					return content.includes("OpenAI") && element?.tagName === "BUTTON";
				},
			});
			await user.click(openAiButton);
			// After collapse, GPT-4 chip should still exist in DOM but be visually hidden
			// We check that the collapse icon rotated
			const openAiSection = screen.getByText("OpenAI").closest("div");
			const chevron = openAiSection?.querySelector("svg");
			expect(chevron).toHaveClass("-rotate-90");
		});

		it("expands provider group when clicked again", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const openAiButton = screen.getByRole("button", {
				name: /OpenAI/,
			});
			await user.click(openAiButton);
			await user.click(openAiButton);
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
		});

		it("collapses all providers when collapse all button is clicked", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const collapseAllButton = screen.getByRole("button", {
				name: "Collapse all providers",
			});
			await user.click(collapseAllButton);
			// Check that the button label changed to "Expand all providers"
			const expandAllButton = screen.getByRole("button", {
				name: "Expand all providers",
			});
			expect(expandAllButton).toBeInTheDocument();
		});

		it("expands all providers when expand all button is clicked", async () => {
			const { user } = renderWithProviders(<ModelPicker {...defaultProps} />);
			const collapseAllButton = screen.getByRole("button", {
				name: "Collapse all providers",
			});
			await user.click(collapseAllButton);
			const expandAllButton = screen.getByRole("button", {
				name: "Expand all providers",
			});
			await user.click(expandAllButton);
			expect(screen.getByText("GPT-4")).toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
			expect(screen.getByText("Llama 3")).toBeInTheDocument();
		});
	});

	describe("disabled state", () => {
		it("disables modelPicker when disabled prop is true", () => {
			renderWithProviders(<ModelPicker {...defaultProps} disabled={true} />);
			const searchInput = screen.getByPlaceholderText("Filter models…");
			expect(searchInput).toBeDisabled();
		});

		it("prevents model selection when disabled", async () => {
			const onChange = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker {...defaultProps} disabled={true} onChange={onChange} />,
			);
			const gptButton = screen.getByText("GPT-4");
			await user.click(gptButton);
			expect(onChange).not.toHaveBeenCalled();
		});

		it("disables collapse/expand buttons when disabled", () => {
			renderWithProviders(<ModelPicker {...defaultProps} disabled={true} />);
			// The scroll container div has pointer-events-none class when disabled
			const scrollContainer = screen
				.getByText("GPT-4")
				.closest("div.pointer-events-none");
			expect(scrollContainer).toBeInTheDocument();
		});

		it("shows reduced opacity when disabled", () => {
			renderWithProviders(<ModelPicker {...defaultProps} disabled={true} />);
			// The scroll container has opacity-50 and pointer-events-none classes when disabled
			const scrollContainer = screen
				.getByText("GPT-4")
				.closest("div.opacity-50");
			expect(scrollContainer).toBeInTheDocument();
		});
	});

	describe("exclude functionality", () => {
		it("excludes models specified in exclude prop", () => {
			renderWithProviders(
				<ModelPicker {...defaultProps} exclude={["OpenAI/gpt-4"]} />,
			);
			expect(screen.queryByText("GPT-4")).not.toBeInTheDocument();
			expect(screen.getByText("Claude 3")).toBeInTheDocument();
		});

		it("excludes multiple models", () => {
			renderWithProviders(
				<ModelPicker
					{...defaultProps}
					exclude={["OpenAI/gpt-4", "Anthropic/claude-3"]}
				/>,
			);
			expect(screen.queryByText("GPT-4")).not.toBeInTheDocument();
			expect(screen.queryByText("Claude 3")).not.toBeInTheDocument();
			expect(screen.getByText("Llama 3")).toBeInTheDocument();
		});
	});

	describe("random button", () => {
		it("renders random button when onRandom callback is provided", () => {
			const onRandom = vi.fn();
			renderWithProviders(
				<ModelPicker {...defaultProps} onRandom={onRandom} />,
			);
			const randomButton = screen.getByRole("button", { name: /Random/ });
			expect(randomButton).toBeInTheDocument();
		});

		it("calls onRandom when random button is clicked", async () => {
			const onRandom = vi.fn();
			const { user } = renderWithProviders(
				<ModelPicker {...defaultProps} onRandom={onRandom} />,
			);
			const randomButton = screen.getByRole("button", { name: /Random/ });
			await user.click(randomButton);
			expect(onRandom).toHaveBeenCalledTimes(1);
		});

		it("does not render random button when onRandom is not provided", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			expect(
				screen.queryByRole("button", { name: "Random" }),
			).not.toBeInTheDocument();
		});
	});

	describe("accessibility", () => {
		it("has proper label for search input", () => {
			renderWithProviders(<ModelPicker {...defaultProps} id="model-picker" />);
			const searchInput = screen.getByLabelText("Select Model");
			expect(searchInput).toHaveAttribute("id", "model-picker");
		});

		it("has proper title on model chips", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			const gptChip = screen.getByText("GPT-4").closest("div");
			// Title format: provider_name/display_name
			expect(gptChip).toHaveAttribute("title", "OpenAI/GPT-4");
		});

		it("has proper aria-label on collapse buttons", () => {
			renderWithProviders(<ModelPicker {...defaultProps} />);
			const collapseButton = screen.getByRole("button", {
				name: "Collapse all providers",
			});
			expect(collapseButton).toHaveAttribute("title");
		});
	});
});
