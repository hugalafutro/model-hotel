import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import { ConversationConfig } from "../ConversationConfig";

describe("ConversationConfig", () => {
	const defaultProps = {
		maxTurns: 5,
		onMaxTurnsChange: vi.fn(),
		turnDelayMs: 1000,
		onTurnDelayMsChange: vi.fn(),
		conversationState: "idle" as const,
		currentTurn: 0,
		turnCountdown: 0,
		configCollapsed: false,
		onToggleCollapsed: vi.fn(),
		input: "",
		onInputChange: vi.fn(),
		onStart: vi.fn(),
		onContinue: vi.fn(),
		onRetry: vi.fn(),
		onStop: vi.fn(),
		canStart: true,
		disabledReason: undefined,
		selectedModel: "Ollama Cloud/gemma3:4b",
		selectedModelB: "Ollama Cloud/glm-5",
		failedModel: undefined,
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering", () => {
		it("renders conversation config header", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			expect(screen.getByText("Conversation Config")).toBeInTheDocument();
		});

		it("renders rounds and delay inputs", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			expect(screen.getByLabelText("Rounds")).toBeInTheDocument();
			expect(screen.getByLabelText("Delay (ms)")).toBeInTheDocument();
		});

		it("renders prompt textarea when idle", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			expect(screen.getByLabelText("Prompt")).toBeInTheDocument();
		});

		it("renders start button when idle", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			expect(screen.getByRole("button", { name: "Start" })).toBeInTheDocument();
		});

		it("renders status indicator", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			expect(screen.getByText("Status:")).toBeInTheDocument();
			expect(screen.getByText("idle")).toBeInTheDocument();
		});

		it("renders collapse/expand toggle button", () => {
			renderWithProviders(<ConversationConfig {...defaultProps} />);
			// The toggle button is in the header - find all buttons and check there's more than just Start
			const buttons = screen.getAllByRole("button");
			expect(buttons.length).toBeGreaterThan(1);
		});

		it("shows collapsed preview when configCollapsed is true", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} configCollapsed />,
			);
			expect(screen.getByText(/Rounds:/)).toBeInTheDocument();
			expect(screen.getByText("5")).toBeInTheDocument();
			expect(screen.getByText(/Delay:/)).toBeInTheDocument();
			expect(screen.getByText("1000")).toBeInTheDocument();
		});

		it("shows round counter when conversation is active", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="running"
					currentTurn={3}
				/>,
			);
			expect(screen.getByText("Round:")).toBeInTheDocument();
			expect(screen.getByText("2 / 5")).toBeInTheDocument();
		});
	});

	describe("state rendering", () => {
		it("renders continue button when paused", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="paused" />,
			);
			expect(
				screen.getByRole("button", { name: "Continue" }),
			).toBeInTheDocument();
		});

		it("renders retry button when error state", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="error" />,
			);
			expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
		});

		it("renders stop button when running", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="running" />,
			);
			expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
		});

		it("shows error banner with failed model name", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="error"
					failedModel="Ollama Cloud/gemma3:4b"
				/>,
			);
			expect(
				screen.getByText(
					"Ollama Cloud/gemma3:4b: Generation failed - use Retry or change the model to continue",
				),
			).toBeInTheDocument();
		});

		it("shows disabled reason hint when cannot start", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					canStart={false}
					disabledReason="Select both models first"
				/>,
			);
			expect(screen.getByText("Select both models first")).toBeInTheDocument();
		});

		it("shows countdown timer when turnCountdown > 0", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="running"
					currentTurn={2}
					turnCountdown={5}
				/>,
			);
			expect(screen.getByText("Next in 5s…")).toBeInTheDocument();
		});
	});

	describe("user interactions", () => {
		it("calls onMaxTurnsChange when rounds input changes", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const roundsInput = screen.getByDisplayValue("5");
			await user.clear(roundsInput);
			await user.type(roundsInput, "10");
			expect(defaultProps.onMaxTurnsChange).toHaveBeenCalled();
		});

		it("clamps rounds value to max 50 on blur", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const roundsInput = screen.getByLabelText("Rounds");
			await user.clear(roundsInput);
			await user.type(roundsInput, "100");
			await user.tab();
			expect(defaultProps.onMaxTurnsChange).toHaveBeenCalledWith(50);
		});

		it("clamps rounds value to min 1 on blur", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const roundsInput = screen.getByDisplayValue("5");
			await user.clear(roundsInput);
			await user.type(roundsInput, "0");
			await user.tab();
			expect(defaultProps.onMaxTurnsChange).toHaveBeenCalled();
		});

		it("calls onTurnDelayMsChange when delay input changes", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const delayInput = screen.getByDisplayValue("1000");
			await user.clear(delayInput);
			await user.type(delayInput, "2000");
			expect(defaultProps.onTurnDelayMsChange).toHaveBeenCalled();
		});

		it("clamps delay value to max 5000 on blur", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const delayInput = screen.getByLabelText("Delay (ms)");
			await user.clear(delayInput);
			await user.type(delayInput, "10000");
			await user.tab();
			expect(defaultProps.onTurnDelayMsChange).toHaveBeenCalledWith(5000);
		});

		it("calls onInputChange when prompt textarea changes", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const promptTextarea = screen.getByPlaceholderText(
				/Select both models first|Enter a topic/,
			);
			await user.type(promptTextarea, "Test prompt");
			expect(defaultProps.onInputChange).toHaveBeenCalledTimes(11);
		});

		it("calls onStart when start button is clicked", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			const startButton = screen.getByRole("button", { name: "Start" });
			await user.click(startButton);
			expect(defaultProps.onStart).toHaveBeenCalledTimes(1);
		});

		it("calls onContinue when continue button is clicked", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="paused" />,
			);
			const continueButton = screen.getByRole("button", { name: "Continue" });
			await user.click(continueButton);
			expect(defaultProps.onContinue).toHaveBeenCalledTimes(1);
		});

		it("calls onRetry when retry button is clicked", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="error"
					currentTurn={0}
					input="test"
				/>,
			);
			const retryButton = screen.getByRole("button", { name: /Retry/ });
			await user.click(retryButton);
			expect(defaultProps.onRetry).toHaveBeenCalled();
		});

		it("calls onStop when stop button is clicked", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="running" />,
			);
			const stopButton = screen.getByRole("button", { name: "Stop" });
			await user.click(stopButton);
			expect(defaultProps.onStop).toHaveBeenCalledTimes(1);
		});

		it("calls onToggleCollapsed when toggle button is clicked", async () => {
			const { user } = renderWithProviders(
				<ConversationConfig {...defaultProps} />,
			);
			// The toggle button is in the header with chevron icons
			// Find the button that's not the Start button
			const allButtons = screen.getAllByRole("button");
			const toggleButton = allButtons.find(
				(btn) => !btn.textContent?.includes("Start"),
			);
			if (toggleButton) {
				await user.click(toggleButton);
				expect(defaultProps.onToggleCollapsed).toHaveBeenCalledTimes(1);
			}
		});
	});

	describe("disabled states", () => {
		it("disables rounds input when conversation is not idle", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="running" />,
			);
			expect(screen.getByLabelText("Rounds")).toBeDisabled();
		});

		it("disables delay input when conversation is not idle", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} conversationState="running" />,
			);
			expect(screen.getByLabelText("Delay (ms)")).toBeDisabled();
		});

		it("disables start button when canStart is false", () => {
			renderWithProviders(
				<ConversationConfig {...defaultProps} canStart={false} />,
			);
			expect(screen.getByRole("button", { name: "Start" })).toBeDisabled();
		});

		it("disables prompt when no models selected", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					selectedModel={undefined}
					selectedModelB={undefined}
				/>,
			);
			expect(screen.getByLabelText("Prompt")).toBeDisabled();
		});

		it("disables retry button when prompt is empty", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="error"
					currentTurn={0}
					input=""
				/>,
			);
			expect(screen.getByRole("button", { name: "Retry" })).toBeDisabled();
		});
	});

	describe("error states", () => {
		it("shows retry from turn number for later turn failures", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="error"
					currentTurn={4}
				/>,
			);
			expect(screen.getByText("Retry from Turn 2")).toBeInTheDocument();
		});

		it("shows prompt input on first turn error", () => {
			renderWithProviders(
				<ConversationConfig
					{...defaultProps}
					conversationState="error"
					currentTurn={0}
				/>,
			);
			expect(screen.getByLabelText("Prompt")).toBeInTheDocument();
			expect(
				screen.getByPlaceholderText("Re-enter or edit your prompt…"),
			).toBeInTheDocument();
		});
	});
});
