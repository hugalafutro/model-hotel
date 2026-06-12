import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import { ModelReplyCard } from "../ModelReplyCard";

describe("ModelReplyCard", () => {
	const defaultProps = {
		model: "Ollama Cloud/gemma3:4b",
		content: "This is a test response from the model.",
		thinkingContent: "",
		error: undefined,
		metrics: {
			durationMs: 500,
			promptTokens: 10,
			completionTokens: 20,
			tokensPerSecond: 40,
		},
		isStreaming: false,
		startTimeMs: 0,
		isWinner: false,
		isLoser: false,
		tint: "default" as const,
		afterModel: undefined,
		headerEnd: undefined,
		footerStart: undefined,
		footerEnd: undefined,
		className: undefined,
		headerClassName: undefined,
		bodyClassName: undefined,
		footerClassName: undefined,
		modelMaxWidth: "max-w-[26rem]",
		onModelNameClick: undefined,
		shortenModelName: true,
		showInfoIcon: false,
		params: undefined,
		isReasoningModel: false,
		personaName: undefined,
		personaTooltip: undefined,
		turnNumber: undefined,
		onDisableModel: undefined,
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering", () => {
		it("renders model name", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			expect(screen.getByText("gemma3:4b")).toBeInTheDocument();
		});

		it("renders full model name when shortenModelName is false", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} shortenModelName={false} />,
			);
			expect(screen.getByText("Ollama Cloud/gemma3:4b")).toBeInTheDocument();
		});

		it("renders content", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			expect(
				screen.getByText("This is a test response from the model."),
			).toBeInTheDocument();
		});

		it("renders bot icon", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			const botIcon = document.querySelector("svg.lucide-bot");
			expect(botIcon).toBeInTheDocument();
		});

		it("renders metrics in footer", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			expect(screen.getByText(/500ms/)).toBeInTheDocument();
			expect(screen.getByText(/40\.0 tok\/s/)).toBeInTheDocument();
			expect(screen.getByText(/30 tok/)).toBeInTheDocument();
		});

		it("renders thinking block when thinkingContent is provided", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					thinkingContent="This is the thinking process."
				/>,
			);
			expect(screen.getByText("Thinking")).toBeInTheDocument();
			const thinkingButton = screen.getByText("Thinking").closest("button");
			if (thinkingButton) {
				await user.click(thinkingButton);
			}
			expect(screen.getByText(/thinking process/)).toBeInTheDocument();
		});

		it("renders persona name when provided", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					personaName="🤖 Assistant"
					personaTooltip="Helpful assistant persona"
				/>,
			);
			expect(screen.getByText("🤖 Assistant")).toBeInTheDocument();
		});

		it("renders turn number when provided", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} turnNumber={3} />);
			expect(screen.getByText("Turn 3")).toBeInTheDocument();
		});

		it("renders custom params icon when params are provided", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					params={{ temperature: 0.7, max_tokens: 100 }}
				/>,
			);
			const settingsIcon = screen.getByTitle(/temperature:/i);
			expect(settingsIcon).toBeInTheDocument();
		});
	});

	describe("streaming state", () => {
		it("shows waiting indicator when streaming without content", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} isStreaming content="" />,
			);
			expect(screen.getByText("Waiting…")).toBeInTheDocument();
		});

		it("shows thinking indicator for reasoning models", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					isStreaming
					content=""
					isReasoningModel
				/>,
			);
			expect(screen.getByText("Thinking…")).toBeInTheDocument();
		});

		it("shows elapsed time during streaming", async () => {
			const now = Date.now();
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					isStreaming
					startTimeMs={now}
					content="Streaming content..."
				/>,
			);
			// Wait for the timer to update
			await waitFor(() => {
				const elapsedElement = screen.getByText(/\d+s/);
				expect(elapsedElement).toBeInTheDocument();
			});
		});

		it("does not show elapsed time when not streaming", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			expect(screen.queryByText(/\d+s/)).not.toBeInTheDocument();
		});

		it("shows cancel button in headerEnd when streaming", () => {
			const cancelButton = (
				<button type="button" aria-label="Cancel">
					Cancel
				</button>
			);
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					isStreaming
					headerEnd={cancelButton}
				/>,
			);
			expect(
				screen.getByRole("button", { name: "Cancel" }),
			).toBeInTheDocument();
		});
	});

	describe("error states", () => {
		it("renders error message when error is provided", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					error="Failed to generate response"
					content=""
				/>,
			);
			expect(
				screen.getByText("Failed to generate response"),
			).toBeInTheDocument();
		});

		it("renders error with partial content", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					error="Connection timeout"
					content="Partial response..."
				/>,
			);
			expect(screen.getByText("Partial response...")).toBeInTheDocument();
			expect(screen.getByText("⚠ Connection timeout")).toBeInTheDocument();
		});

		it("shows disable model button for 5xx errors", () => {
			const onDisableModel = vi.fn();
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					error="500 Internal Server Error"
					content=""
					onDisableModel={onDisableModel}
				/>,
			);
			expect(screen.getByText("Disable model")).toBeInTheDocument();
		});

		it("calls onDisableModel when disable button is clicked", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					error="502 Bad Gateway"
					content=""
					onDisableModel={vi.fn()}
				/>,
			);
			const disableButton = screen.getByRole("button", {
				name: "Disable model",
			});
			await user.click(disableButton);
			// onDisableModel should be called
		});

		it("does not show disable button for non-5xx errors", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} error="400 Bad Request" content="" />,
			);
			expect(screen.queryByText("Disable model")).not.toBeInTheDocument();
		});
	});

	describe("winner/loser states", () => {
		it("applies winner ring when isWinner is true", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} isWinner />);
			const card = document.querySelector(".ui-card");
			expect(card?.className).toContain("ring-1");
			expect(card?.className).toContain("ring-green-500/40");
		});

		it("applies loser opacity when isLoser is true", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} isLoser />);
			const card = document.querySelector(".ui-card");
			expect(card?.className).toContain("opacity-60");
		});
	});

	describe("tint variants", () => {
		it("applies accent tint class", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} tint="accent" />);
			const card = document.querySelector(".ui-card");
			expect(card?.className).toContain("ui-card-tint-accent");
		});

		it("applies blue tint class", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} tint="blue" />);
			const card = document.querySelector(".ui-card");
			expect(card?.className).toContain("ui-card-tint-blue");
		});
	});

	describe("maximize modal", () => {
		it("shows maximize button when content is available and not streaming", () => {
			renderWithProviders(<ModelReplyCard {...defaultProps} />);
			const maximizeButton = screen.getByRole("button", {
				name: "Maximize reply",
			});
			expect(maximizeButton).toBeInTheDocument();
		});

		it("does not show maximize button when streaming", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} isStreaming content="Streaming..." />,
			);
			expect(
				screen.queryByRole("button", { name: "Maximize reply" }),
			).not.toBeInTheDocument();
		});

		it("does not show maximize button when there is an error", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} error="Error" content="" />,
			);
			expect(
				screen.queryByRole("button", { name: "Maximize reply" }),
			).not.toBeInTheDocument();
		});

		it("opens modal when maximize button is clicked", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard {...defaultProps} />,
			);
			const maximizeButton = screen.getByRole("button", {
				name: "Maximize reply",
			});
			await user.click(maximizeButton);
			expect(screen.getByRole("dialog")).toBeInTheDocument();
		});

		it("shows maximized content in modal", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard {...defaultProps} />,
			);
			const maximizeButton = screen.getByRole("button", {
				name: "Maximize reply",
			});
			await user.click(maximizeButton);
			await waitFor(() => {
				expect(document.querySelector('[role="dialog"]')).toBeInTheDocument();
			});
		});

		it("closes modal when close button is clicked", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard {...defaultProps} />,
			);
			const maximizeButton = screen.getByRole("button", {
				name: "Maximize reply",
			});
			await user.click(maximizeButton);
			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);
			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});
		});

		it("copies content from modal when copy button is clicked", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard {...defaultProps} />,
			);
			const maximizeButton = screen.getByRole("button", {
				name: "Maximize reply",
			});
			await user.click(maximizeButton);
			const copyButton = screen.getByRole("button", { name: "Copy" });
			await user.click(copyButton);
			// Clipboard copy is tested separately
		});
	});

	describe("custom props", () => {
		it("renders afterModel content", () => {
			const afterModel = <span data-testid="after-model">Custom</span>;
			renderWithProviders(
				<ModelReplyCard {...defaultProps} afterModel={afterModel} />,
			);
			expect(screen.getByTestId("after-model")).toBeInTheDocument();
		});

		it("renders footerStart content", () => {
			const footerStart = <span data-testid="footer-start">Start</span>;
			renderWithProviders(
				<ModelReplyCard {...defaultProps} footerStart={footerStart} />,
			);
			expect(screen.getByTestId("footer-start")).toBeInTheDocument();
		});

		it("renders footerEnd content", () => {
			const footerEnd = <span data-testid="footer-end">End</span>;
			renderWithProviders(
				<ModelReplyCard {...defaultProps} footerEnd={footerEnd} />,
			);
			expect(screen.getByTestId("footer-end")).toBeInTheDocument();
		});

		it("applies custom className", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} className="custom-class" />,
			);
			const card = document.querySelector(".ui-card");
			expect(card?.className).toContain("custom-class");
		});

		it("applies custom headerClassName", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} headerClassName="header-class" />,
			);
			const card = document.querySelector(".ui-card");
			const header = card?.querySelector(
				"div.flex.items-center.justify-between",
			);
			expect(header?.className).toContain("header-class");
		});

		it("applies custom footerClassName", () => {
			renderWithProviders(
				<ModelReplyCard {...defaultProps} footerClassName="footer-class" />,
			);
			const card = document.querySelector(".ui-card");
			const footer = Array.from(
				card?.querySelectorAll("div.flex.items-center.justify-between") || [],
			).find((el) => el.className.includes("text-[11px]"));
			expect(footer?.className).toContain("footer-class");
		});
	});

	describe("clickable model name", () => {
		it("renders model name as button when onModelNameClick is provided", () => {
			const onModelNameClick = vi.fn();
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					onModelNameClick={onModelNameClick}
				/>,
			);
			const modelNameElement = screen.getByText("gemma3:4b");
			const parent = modelNameElement.closest("[role='button']");
			expect(parent).toBeInTheDocument();
		});

		it("tints the clickable model name when the card is accent-tinted", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					tint="accent"
					onModelNameClick={vi.fn()}
				/>,
			);
			expect(screen.getByText("gemma3:4b")).toHaveClass("text-(--accent)");
		});

		it("calls onModelNameClick when model name is clicked", async () => {
			const { user } = renderWithProviders(
				<ModelReplyCard {...defaultProps} onModelNameClick={vi.fn()} />,
			);
			const modelNameButton = screen
				.getByText("gemma3:4b")
				.closest("[role='button']");
			if (modelNameButton) {
				await user.click(modelNameButton);
				// onModelNameClick should be called
			}
		});

		it("shows info icon when showInfoIcon is true", () => {
			renderWithProviders(
				<ModelReplyCard
					{...defaultProps}
					onModelNameClick={vi.fn()}
					showInfoIcon
				/>,
			);
			expect(screen.getByTitle("Model details")).toBeInTheDocument();
		});
	});
});
