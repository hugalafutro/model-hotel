import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockModel } from "../../test/mocks/data";
import { renderWithProviders } from "../../test/utils";
import { ModelDetailModal, ModelDetailPanel } from "../ModelDetailPanel";

describe("ModelDetailPanel", () => {
	const defaultProps = {
		model: mockModel,
	};

	it("displays model name as title", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test Model v1")).toBeInTheDocument();
	});

	it("displays model description", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.getByText("A test model for development"),
		).toBeInTheDocument();
	});

	it("displays provider name", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("displays model ID", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("test-model-v1")).toBeInTheDocument();
	});

	it("displays context length", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("8,192")).toBeInTheDocument();
	});

	it("displays max output tokens", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("4,096")).toBeInTheDocument();
	});

	it("displays input price", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("$0.5")).toBeInTheDocument();
	});

	it("displays output price", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("$1.5")).toBeInTheDocument();
	});

	it("displays capabilities section", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		// Capabilities section exists with badges
		expect(screen.getByText("Provider")).toBeInTheDocument();
	});

	it("displays proxy ID pill", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(screen.getByText("Test-Provider/test-model-v1")).toBeInTheDocument();
	});

	it("shows collapse/expand toggle by default", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		const collapseButton = screen.getByRole("button", {
			name: /Collapse model details/i,
		});
		expect(collapseButton).toBeInTheDocument();
	});

	it("does not show collapse toggle when collapsible is false", () => {
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} collapsible={false} />,
		);

		expect(
			screen.queryByRole("button", { name: /Collapse model details/i }),
		).not.toBeInTheDocument();
	});

	it("collapses when collapse button is clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		await user.click(
			screen.getByRole("button", { name: /Collapse model details/i }),
		);

		expect(
			screen.queryByText("A test model for development"),
		).not.toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: /Expand model details/i }),
		).toBeInTheDocument();
	});

	it("expands when expand button is clicked after collapsing", async () => {
		const user = userEvent.setup();
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		await user.click(
			screen.getByRole("button", { name: /Collapse model details/i }),
		);
		await user.click(
			screen.getByRole("button", { name: /Expand model details/i }),
		);

		expect(
			screen.getByText("A test model for development"),
		).toBeInTheDocument();
	});

	it("shows settings cog when params and onParamsChange are provided", () => {
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /Generation parameters/i }),
		).toBeInTheDocument();
	});

	it("does not show settings cog when params is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.queryByRole("button", { name: /Generation parameters/i }),
		).not.toBeInTheDocument();
	});

	it("does not show settings cog when onParamsChange is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} params={{}} />);

		expect(
			screen.queryByRole("button", { name: /Generation parameters/i }),
		).not.toBeInTheDocument();
	});

	it("shows parameter sliders when settings cog is clicked", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// Sliders use span labels, not aria-labels
		expect(screen.getByText("Temperature")).toBeInTheDocument();
		expect(screen.getByText("Max Tokens")).toBeInTheDocument();
		expect(screen.getByText("Top P")).toBeInTheDocument();
		expect(screen.getByText("Min P")).toBeInTheDocument();
		expect(screen.getByText("Top K")).toBeInTheDocument();
		expect(screen.getByText("Freq Penalty")).toBeInTheDocument();
		expect(screen.getByText("Pres Penalty")).toBeInTheDocument();
	});

	it("shows reset button when custom params are set", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ temperature: 0.7 }}
				onParamsChange={onParamsChange}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /Reset parameters/i }),
		).toBeInTheDocument();
	});

	it("does not show reset button when no custom params", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		expect(
			screen.queryByRole("button", { name: /Reset parameters/i }),
		).not.toBeInTheDocument();
	});

	it("calls onParamsChange when reset button is clicked", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ temperature: 0.7 }}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(screen.getByRole("button", { name: /Reset parameters/i }));

		expect(onParamsChange).toHaveBeenCalledWith({});
	});

	it("calls onParamsChange when temperature slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		// Find the number input for temperature and change it
		const numberInputs = screen.getAllByRole("spinbutton");
		expect(numberInputs.length).toBeGreaterThan(0);
		await user.clear(numberInputs[0]);
		await user.type(numberInputs[0], "0.7");

		expect(onParamsChange).toHaveBeenCalled();
	});

	it("shows ApplyRecommendedButton when settings are opened", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		// Open settings panel first
		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// Apply Recommended button exists (shows "Loading..." initially)
		const applyButton = screen.getByRole("button", { name: /Loading/i });
		expect(applyButton).toBeInTheDocument();
		expect(applyButton).toBeDisabled();
	});

	it("applies accent tint class when tint is accent", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="accent" />,
		);

		expect(container.firstChild).toHaveClass("ui-card-tint-accent");
	});

	it("applies blue tint class when tint is blue", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="blue" />,
		);

		expect(container.firstChild).toHaveClass("ui-card-tint-blue");
	});

	it("does not apply tint class when tint is default", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} tint="default" />,
		);

		expect(container.firstChild).not.toHaveClass("ui-card-tint-accent");
		expect(container.firstChild).not.toHaveClass("ui-card-tint-blue");
	});

	it("applies pulse border class when pulseBorder is true", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} pulseBorder />,
		);

		expect(container.firstChild).toHaveClass(
			"animate-[pulse-border_2s_ease-in-out_infinite]",
		);
	});

	it("does not apply pulse border class when pulseBorder is false", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} pulseBorder={false} />,
		);

		expect(container.firstChild).not.toHaveClass(
			"animate-[pulse-border_2s_ease-in-out_infinite]",
		);
	});

	it("shows close button when onClose is provided", () => {
		const onClose = vi.fn();
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} onClose={onClose} />,
		);

		expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
	});

	it("does not show close button when onClose is not provided", () => {
		renderWithProviders(<ModelDetailPanel {...defaultProps} />);

		expect(
			screen.queryByRole("button", { name: "Close" }),
		).not.toBeInTheDocument();
	});

	it("calls onClose when close button is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		renderWithProviders(
			<ModelDetailPanel {...defaultProps} onClose={onClose} />,
		);

		await user.click(screen.getByRole("button", { name: "Close" }));

		expect(onClose).toHaveBeenCalled();
	});

	it("does not apply card wrapper when embedded is true", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} embedded />,
		);

		expect(container.firstChild).not.toHaveClass("ui-card");
	});

	it("applies card wrapper when embedded is false", () => {
		const { container } = renderWithProviders(
			<ModelDetailPanel {...defaultProps} embedded={false} />,
		);

		expect(container.firstChild).toHaveClass("ui-card");
	});

	it("uses display_name as title when available", () => {
		const modelWithDisplayName = {
			...mockModel,
			display_name: "Custom Display Name",
		};

		renderWithProviders(<ModelDetailPanel model={modelWithDisplayName} />);

		expect(screen.getByText("Custom Display Name")).toBeInTheDocument();
	});

	it("uses model_id as title when display_name is not available", () => {
		const modelWithoutDisplayName = {
			...mockModel,
			display_name: "",
		};

		renderWithProviders(<ModelDetailPanel model={modelWithoutDisplayName} />);

		// Title appears in h3 element
		const title = screen.getByRole("heading", { level: 3 });
		expect(title).toHaveTextContent("test-model-v1");
	});

	it("shows ReasoningEffortSelect when model has reasoning capability and provider supports it", async () => {
		const user = userEvent.setup();
		const reasoningModel = {
			...mockModel,
			capabilities: '{"reasoning":true}',
			provider_name: "OpenAI",
		};

		renderWithProviders(
			<ModelDetailPanel
				model={reasoningModel}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		// Open settings panel
		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// Reasoning Effort section should be visible
		expect(screen.getByText(/Reasoning Effort/i)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Low/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Medium/i })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /High/i })).toBeInTheDocument();
	});

	it("does NOT show ReasoningEffortSelect when model has no reasoning capability", async () => {
		const user = userEvent.setup();
		const nonReasoningModel = {
			...mockModel,
			capabilities: '{"reasoning":false}',
			provider_name: "OpenAI",
		};

		renderWithProviders(
			<ModelDetailPanel
				model={nonReasoningModel}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		expect(screen.queryByText(/Reasoning Effort/i)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /Low/i }),
		).not.toBeInTheDocument();
	});

	it("does NOT show ReasoningEffortSelect when model capabilities don't include reasoning", async () => {
		const user = userEvent.setup();
		const modelWithoutReasoning = {
			...mockModel,
			capabilities: '{"streaming":true,"vision":false}',
			provider_name: "OpenAI",
		};

		renderWithProviders(
			<ModelDetailPanel
				model={modelWithoutReasoning}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		expect(screen.queryByText(/Reasoning Effort/i)).not.toBeInTheDocument();
	});

	it("does NOT show ReasoningEffortSelect when provider doesn't support reasoning_effort (Anthropic)", async () => {
		const user = userEvent.setup();
		const anthropicReasoningModel = {
			...mockModel,
			capabilities: '{"reasoning":true}',
			provider_name: "Anthropic",
		};

		renderWithProviders(
			<ModelDetailPanel
				model={anthropicReasoningModel}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		expect(screen.queryByText(/Reasoning Effort/i)).not.toBeInTheDocument();
	});

	it("hides incompatible sliders for Anthropic provider (top_p, min_p, freq penalty, pres penalty)", async () => {
		const user = userEvent.setup();
		const anthropicModel = {
			...mockModel,
			provider_name: "Anthropic",
		};

		renderWithProviders(
			<ModelDetailPanel
				model={anthropicModel}
				params={{}}
				onParamsChange={vi.fn()}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);

		// These should NOT be in the DOM for Anthropic
		expect(screen.queryByText("Top P")).not.toBeInTheDocument();
		expect(screen.queryByText("Min P")).not.toBeInTheDocument();
		expect(screen.queryByText("Freq Penalty")).not.toBeInTheDocument();
		expect(screen.queryByText("Pres Penalty")).not.toBeInTheDocument();

		// These should still be visible
		expect(screen.getByText("Temperature")).toBeInTheDocument();
		expect(screen.getByText("Max Tokens")).toBeInTheDocument();
		expect(screen.getByText("Top K")).toBeInTheDocument();
	});

	it("calls onParamsChange with max_tokens when Max Tokens slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[1]);
		await user.type(numberInputs[1], "1024");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("max_tokens");
	});

	it("calls onParamsChange with top_p when Top P slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[2]);
		await user.type(numberInputs[2], "0.9");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("top_p");
	});

	it("calls onParamsChange with min_p when Min P slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[3]);
		await user.type(numberInputs[3], "0.1");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("min_p");
	});

	it("calls onParamsChange with top_k when Top K slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[4]);
		await user.type(numberInputs[4], "50");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("top_k");
	});

	it("calls onParamsChange with frequency_penalty when Freq Penalty slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[5]);
		await user.type(numberInputs[5], "0.5");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("frequency_penalty");
	});

	it("calls onParamsChange with presence_penalty when Pres Penalty slider is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		const numberInputs = screen.getAllByRole("spinbutton");
		await user.clear(numberInputs[6]);
		await user.type(numberInputs[6], "0.3");

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("presence_penalty");
	});

	it("calls onParamsChange with reasoning_effort when ReasoningEffortSelect is changed", async () => {
		const user = userEvent.setup();
		const onParamsChange = vi.fn();
		const reasoningModel = {
			...mockModel,
			capabilities: '{"reasoning":true}',
			provider_name: "OpenAI",
		};
		renderWithProviders(
			<ModelDetailPanel
				model={reasoningModel}
				params={{}}
				onParamsChange={onParamsChange}
			/>,
		);

		await user.click(
			screen.getByRole("button", { name: /Generation parameters/i }),
		);
		await user.click(screen.getByRole("button", { name: /Medium/i }));

		expect(onParamsChange).toHaveBeenCalled();
		const lastCall = onParamsChange.mock.calls.at(-1)[0];
		expect(lastCall).toHaveProperty("reasoning_effort");
	});

	it("shows reset button when max_tokens is set", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ max_tokens: 100 }}
				onParamsChange={onParamsChange}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /Reset parameters/i }),
		).toBeInTheDocument();
	});

	it("shows accent glow on settings cog when custom params are set but panel is closed", () => {
		const onParamsChange = vi.fn();
		renderWithProviders(
			<ModelDetailPanel
				{...defaultProps}
				params={{ top_p: 0.9 }}
				onParamsChange={onParamsChange}
			/>,
		);

		const cogButton = screen.getByRole("button", {
			name: /Generation parameters/i,
		});
		expect(cogButton).toHaveClass("text-(--accent)");
	});

	it("displays dash when context_length is null", () => {
		const modelWithNullContext = {
			...mockModel,
			context_length: null,
		};
		renderWithProviders(<ModelDetailPanel model={modelWithNullContext} />);

		// Find the "Context" label, then the value in the same div
		const contextLabel = screen.getByText("Context");
		const contextValue = contextLabel.nextElementSibling;
		expect(contextValue).toHaveTextContent("-");
	});

	it("displays dash when max_output_tokens is null", () => {
		const modelWithNullMaxOutput = {
			...mockModel,
			max_output_tokens: null,
		};
		renderWithProviders(<ModelDetailPanel model={modelWithNullMaxOutput} />);

		const maxOutLabel = screen.getByText("Max Out");
		const maxOutValue = maxOutLabel.nextElementSibling;
		expect(maxOutValue).toHaveTextContent("-");
	});
});

describe("ModelDetailModal", () => {
	const onClose = vi.fn();

	it("renders Modal wrapper with ModelDetailPanel inside", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		expect(screen.getByText("Test Model v1")).toBeInTheDocument();
		expect(screen.getByText("Test Provider")).toBeInTheDocument();
	});

	it("passes collapsible prop to ModelDetailPanel", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} collapsible />,
		);

		expect(
			screen.getByRole("button", { name: /Collapse model details/i }),
		).toBeInTheDocument();
	});

	it("does not show collapsible toggle by default", () => {
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		expect(
			screen.queryByRole("button", { name: /Collapse model details/i }),
		).not.toBeInTheDocument();
	});

	it("passes embedded prop to ModelDetailPanel", () => {
		const { container } = renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		// The panel should not have the outer card wrapper when embedded
		const panel = container.querySelector(".text-xs.relative.overflow-y-auto");
		expect(panel).toBeInTheDocument();
	});

	it("calls onClose when modal is closed", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ModelDetailModal model={mockModel} onClose={onClose} />,
		);

		const closeButton = screen.getByLabelText("Close");
		await user.click(closeButton);

		expect(onClose).toHaveBeenCalled();
	});
});
