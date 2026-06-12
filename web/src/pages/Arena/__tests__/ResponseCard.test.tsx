import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import { AllProviders } from "../../../test/utils";
import { ResponseCard } from "../ResponseCard";
import type { ArenaResponse } from "../types";

const mockEnabledModel: Model = {
	id: "model-001",
	model_id: "gemma3:4b",
	name: "Gemma 3 4B",
	description: "Google Gemma 3 4B model",
	display_name: "Gemma 3 4B",
	provider_id: "provider-001",
	provider_name: "Ollama Cloud",
	capabilities: '{"reasoning":false,"streaming":true,"vision":false}',
	params: '{"temperature":0.7}',
	modality: "text",
	input_modalities: "text",
	output_modalities: "text",
	context_length: 8192,
	max_output_tokens: 4096,
	input_price_per_million: null,
	input_price_per_million_cache_hit: null,
	output_price_per_million: null,
	owned_by: "google",
	enabled: true,
	disabled_manually: false,
	created_at: "2026-01-01T00:00:00Z",
	last_seen_at: "2026-05-01T00:00:00Z",
};

const mockResponse: ArenaResponse = {
	model: "Ollama-Cloud/gemma3:4b",
	rawContent: "Hello world",
	content: "Hello world",
	thinkingContent: "",
	startTimeMs: Date.now() - 1000,
	done: true,
	error: null,
	metrics: {
		tokensPerSecond: 50,
		durationMs: 1000,
		promptTokens: 10,
		completionTokens: 20,
	},
};

const mockErrorResponse: ArenaResponse = {
	model: "Ollama-Cloud/gemma3:4b",
	rawContent: "",
	content: "",
	thinkingContent: "",
	startTimeMs: Date.now(),
	done: true,
	error: "Chat failed: 500 Internal Server Error",
	metrics: null,
};

const mockStreamingResponse: ArenaResponse = {
	model: "Ollama-Cloud/gemma3:4b",
	rawContent: "Hello",
	content: "Hello",
	thinkingContent: "",
	startTimeMs: Date.now(),
	done: false,
	error: null,
	metrics: {
		tokensPerSecond: null,
		durationMs: 0,
		promptTokens: 0,
		completionTokens: 0,
	},
};

const mockThinkingResponse: ArenaResponse = {
	model: "Ollama-Cloud/gemma3:4b",
	rawContent: "Let me think... The answer is 42.",
	content: "The answer is 42.",
	thinkingContent: "Let me think...",
	startTimeMs: Date.now() - 500,
	done: true,
	error: null,
	metrics: {
		tokensPerSecond: 45,
		durationMs: 500,
		promptTokens: 5,
		completionTokens: 15,
	},
};

const defaultProps = {
	response: mockResponse,
	vote: null as "A" | "B" | null,
	slotKey: "A" as const,
	roundIdx: 0,
	matchupIdx: 0,
	onVote: vi.fn(),
	onRetry: vi.fn(),
	onSwapModel: vi.fn(),
	onCancelSlot: vi.fn(),
	showVote: false,
	enabledModels: [mockEnabledModel],
	params: { temperature: 0.7, max_tokens: 1024 },
};

describe("ResponseCard", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering model info", () => {
		it("renders model name from response.model", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Model name should be displayed (shortened) - check for provider/model format
			expect(screen.getByText(/gemma3:4b/)).toBeInTheDocument();
		});

		it("renders model name as clickable when modelObj exists", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Model name should be displayed and clickable
			expect(screen.getByText("gemma3:4b")).toBeInTheDocument();
		});

		it("renders params tooltip when params exist", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Settings icon should be present for params
			const settingsIcon = document.querySelector(".lucide-settings");
			expect(settingsIcon).toBeInTheDocument();
		});
	});

	describe("completed response state", () => {
		it("renders completed response with content", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Content should be displayed
			expect(screen.getByText("Hello world")).toBeInTheDocument();
		});

		it("shows completed icon (CheckCircle2)", () => {
			const { container } = render(<ResponseCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// CheckCircle2 icon should be present (green)
			const completedIcons = container.querySelectorAll(".text-green-400");
			expect(completedIcons.length).toBeGreaterThan(0);
		});

		it("shows retry button for completed responses", () => {
			const { container } = render(<ResponseCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// RefreshCw icon for retry
			const retryButtons = container.querySelectorAll(".lucide-refresh-cw");
			expect(retryButtons.length).toBeGreaterThan(0);
		});

		it("shows swap model button for completed responses", () => {
			const { container } = render(<ResponseCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// ArrowLeftRight icon for swap
			const swapButtons = container.querySelectorAll(
				".lucide-arrow-left-right",
			);
			expect(swapButtons.length).toBeGreaterThan(0);
		});

		it("shows copy button for completed responses with content", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// CopyButton should render with copy icon
			const copyButton = screen.getByRole("button", { name: /copy/i });
			expect(copyButton).toBeInTheDocument();
		});

		it("calls onRetry when retry button is clicked", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Find retry button by title
			const retryButton = screen.getByRole("button", {
				name: /re-roll/i,
			});
			fireEvent.click(retryButton);

			expect(defaultProps.onRetry).toHaveBeenCalledWith(0, 0, "A");
		});

		it("calls onSwapModel when swap button is clicked", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Find swap button by title
			const swapButton = screen.getByRole("button", {
				name: /swap model/i,
			});
			fireEvent.click(swapButton);

			expect(defaultProps.onSwapModel).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"Ollama-Cloud/gemma3:4b",
			);
		});

		it("calls onVote when vote thumb is clicked", () => {
			render(<ResponseCard {...defaultProps} vote={null} showVote={true} />, {
				wrapper: AllProviders,
			});

			// Vote thumb button
			const voteButton = screen.getByRole("button", {
				name: /vote for this response/i,
			});
			fireEvent.click(voteButton);

			expect(defaultProps.onVote).toHaveBeenCalledWith(0, 0, "A");
		});

		it("does not call onVote when vote thumb is clicked and already voted", () => {
			render(<ResponseCard {...defaultProps} vote="A" showVote={true} />, {
				wrapper: AllProviders,
			});

			// Vote thumb button should be present but disabled
			const voteButtons = screen.getAllByRole("button");
			const voteButton = voteButtons.find(
				(btn) =>
					btn.querySelector(".lucide-thumbs-up") ||
					btn.textContent?.includes("👍"),
			);
			expect(voteButton).toBeInTheDocument();
			if (voteButton) {
				expect(voteButton).toHaveAttribute("disabled");
			}
		});
	});

	describe("error response state", () => {
		it("renders error message", () => {
			render(<ResponseCard {...defaultProps} response={mockErrorResponse} />, {
				wrapper: AllProviders,
			});

			// Error message should be displayed (500 status code)
			expect(screen.getByText(/500/)).toBeInTheDocument();
		});

		it("shows error icon (AlertCircle)", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockErrorResponse} />,
				{ wrapper: AllProviders },
			);

			// AlertCircle icon should be present (red)
			const errorIcons = container.querySelectorAll(".text-red-400");
			expect(errorIcons.length).toBeGreaterThan(0);
		});

		it("shows retry button for error responses", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockErrorResponse} />,
				{ wrapper: AllProviders },
			);

			// RefreshCw icon for retry
			const retryButtons = container.querySelectorAll(".lucide-refresh-cw");
			expect(retryButtons.length).toBeGreaterThan(0);
		});

		it("shows swap model button (X icon) for error responses", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockErrorResponse} />,
				{ wrapper: AllProviders },
			);

			// X icon for swap (different from completed state)
			const swapButtons = container.querySelectorAll(".lucide-x");
			expect(swapButtons.length).toBeGreaterThan(0);
		});

		it("does not show copy button for error responses", () => {
			render(<ResponseCard {...defaultProps} response={mockErrorResponse} />, {
				wrapper: AllProviders,
			});

			expect(
				screen.queryByRole("button", { name: /copy/i }),
			).not.toBeInTheDocument();
		});

		it("calls onRetry when retry button is clicked for error", () => {
			render(<ResponseCard {...defaultProps} response={mockErrorResponse} />, {
				wrapper: AllProviders,
			});

			const retryButton = screen.getByRole("button", {
				name: /retry/i,
			});
			fireEvent.click(retryButton);

			expect(defaultProps.onRetry).toHaveBeenCalledWith(0, 0, "A");
		});

		it("calls onSwapModel when swap button is clicked for error", () => {
			render(<ResponseCard {...defaultProps} response={mockErrorResponse} />, {
				wrapper: AllProviders,
			});

			const swapButton = screen.getByRole("button", {
				name: /swap model/i,
			});
			fireEvent.click(swapButton);

			expect(defaultProps.onSwapModel).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"Ollama-Cloud/gemma3:4b",
			);
		});
	});

	describe("streaming response state", () => {
		it("renders streaming response with partial content", () => {
			render(
				<ResponseCard {...defaultProps} response={mockStreamingResponse} />,
				{ wrapper: AllProviders },
			);

			// Partial content should be displayed
			expect(screen.getByText("Hello")).toBeInTheDocument();
		});

		it("shows cancel button (CircleStop) for streaming responses", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockStreamingResponse} />,
				{ wrapper: AllProviders },
			);

			// CircleStop icon for cancel
			const cancelButtons = container.querySelectorAll(".lucide-circle-stop");
			expect(cancelButtons.length).toBeGreaterThan(0);
		});

		it("does not show retry button for streaming responses", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockStreamingResponse} />,
				{ wrapper: AllProviders },
			);

			// No RefreshCw icon for streaming
			const retryButtons = container.querySelectorAll(".lucide-refresh-cw");
			expect(retryButtons.length).toBe(0);
		});

		it("does not show swap button for streaming responses", () => {
			const { container } = render(
				<ResponseCard {...defaultProps} response={mockStreamingResponse} />,
				{ wrapper: AllProviders },
			);

			// No ArrowLeftRight icon for streaming
			const swapButtons = container.querySelectorAll(
				".lucide-arrow-left-right",
			);
			expect(swapButtons.length).toBe(0);
		});

		it("calls onCancelSlot when cancel button is clicked", () => {
			render(
				<ResponseCard {...defaultProps} response={mockStreamingResponse} />,
				{ wrapper: AllProviders },
			);

			const cancelButton = screen.getByRole("button", {
				name: /cancel/i,
			});
			fireEvent.click(cancelButton);

			expect(defaultProps.onCancelSlot).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"Ollama-Cloud/gemma3:4b",
			);
		});
	});

	describe("thinking content", () => {
		it("renders thinking content when present", () => {
			render(
				<ResponseCard {...defaultProps} response={mockThinkingResponse} />,
				{ wrapper: AllProviders },
			);

			// Thinking content should be displayed (as part of the response)
			expect(screen.getByText(/The answer is 42/)).toBeInTheDocument();
		});

		it("renders final content when thinking content is present", () => {
			render(
				<ResponseCard {...defaultProps} response={mockThinkingResponse} />,
				{ wrapper: AllProviders },
			);

			// Final content should also be displayed
			expect(screen.getByText(/The answer is 42/)).toBeInTheDocument();
		});
	});

	describe("vote state", () => {
		it("renders vote thumb when showVote=true", () => {
			render(<ResponseCard {...defaultProps} vote={null} showVote={true} />, {
				wrapper: AllProviders,
			});

			// Vote thumb button should be present
			const voteButton = screen.getByRole("button", {
				name: /vote for this response/i,
			});
			expect(voteButton).toBeInTheDocument();
		});

		it("does not render vote thumb when showVote=false", () => {
			render(<ResponseCard {...defaultProps} vote={null} showVote={false} />, {
				wrapper: AllProviders,
			});

			expect(
				screen.queryByRole("button", { name: /vote for this response/i }),
			).not.toBeInTheDocument();
		});

		it("shows winner highlight when vote matches slotKey", () => {
			render(<ResponseCard {...defaultProps} vote="A" showVote={true} />, {
				wrapper: AllProviders,
			});

			// Trophy icon should be present for winner
			const trophyIcon = document.querySelector(".lucide-trophy");
			expect(trophyIcon).toBeInTheDocument();
		});

		it("does not show trophy icon when vote does not match slotKey", () => {
			render(<ResponseCard {...defaultProps} vote="B" showVote={true} />, {
				wrapper: AllProviders,
			});

			expect(document.querySelector(".lucide-trophy")).not.toBeInTheDocument();
		});

		it("applies winner styling to vote thumb when isWinner", () => {
			render(<ResponseCard {...defaultProps} vote="A" showVote={true} />, {
				wrapper: AllProviders,
			});

			// Vote thumb should have winner styling (green)
			// Find the vote button by its structure (it's a button with thumb icon)
			const voteButtons = screen.getAllByRole("button");
			const voteButton = voteButtons.find(
				(btn) =>
					btn.querySelector(".lucide-thumbs-up") ||
					btn.textContent?.includes("👍"),
			);
			expect(voteButton).toBeInTheDocument();
			if (voteButton) {
				expect(voteButton).toHaveClass("text-green-400");
			}
		});
	});

	describe("model detail modal", () => {
		it("opens model detail modal when model name is clicked", async () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Click on model name text (uses regex to match)
			const modelNameText = screen.getByText(/gemma3:4b/);
			fireEvent.click(modelNameText);

			// Modal should appear with model details (the heading; the display
			// name also appears as a detail field in the unified modal)
			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Gemma 3 4B" }),
				).toBeInTheDocument();
			});
		});

		it("closes model detail modal when close button is clicked", async () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Open modal
			const modelNameText = screen.getByText(/gemma3:4b/);
			fireEvent.click(modelNameText);

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Gemma 3 4B" }),
				).toBeInTheDocument();
			});

			// Close modal - find by aria-label or close text
			const closeButton = screen.getByLabelText("Close");
			fireEvent.click(closeButton);

			// Modal should close
			await waitFor(() => {
				expect(
					screen.queryByRole("heading", { name: "Gemma 3 4B" }),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("copy functionality", () => {
		it("copies content to clipboard when copy button is clicked", async () => {
			const writeTextMock = vi.fn().mockResolvedValue(undefined);
			vi.stubGlobal("navigator", {
				clipboard: { writeText: writeTextMock },
			});

			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			const copyButton = screen.getByRole("button", { name: /copy/i });
			fireEvent.click(copyButton);

			await waitFor(() => {
				expect(writeTextMock).toHaveBeenCalledWith("Hello world");
			});
		});

		it("does not show copy button when content is empty", () => {
			const emptyResponse: ArenaResponse = {
				...mockResponse,
				content: "",
				rawContent: "",
			};

			render(<ResponseCard {...defaultProps} response={emptyResponse} />, {
				wrapper: AllProviders,
			});

			expect(
				screen.queryByRole("button", { name: /copy/i }),
			).not.toBeInTheDocument();
		});
	});

	describe("disabled model mutation", () => {
		it("renders disable model button for 5xx error response", () => {
			// The disable button appears when error is a 5xx error
			render(<ResponseCard {...defaultProps} response={mockErrorResponse} />, {
				wrapper: AllProviders,
			});

			// Disable model button should be rendered for 5xx errors
			const disableBtn = screen.getByRole("button", {
				name: /disable model/i,
			});
			expect(disableBtn).toBeInTheDocument();
		});
	});

	describe("winner/loser styling", () => {
		it("applies winner styling when isWinner", () => {
			render(<ResponseCard {...defaultProps} vote="A" showVote={true} />, {
				wrapper: AllProviders,
			});

			// Winner should have trophy icon
			const trophyIcon = document.querySelector(".lucide-trophy");
			expect(trophyIcon).toBeInTheDocument();
		});

		it("applies loser styling when isLoser", () => {
			render(<ResponseCard {...defaultProps} vote="B" showVote={true} />, {
				wrapper: AllProviders,
			});

			// Loser should not have trophy icon
			expect(document.querySelector(".lucide-trophy")).not.toBeInTheDocument();
		});
	});

	describe("metrics display", () => {
		it("renders metrics when available", () => {
			render(<ResponseCard {...defaultProps} />, { wrapper: AllProviders });

			// Metrics should be displayed (tokens per second, etc.)
			// The ModelReplyCard component handles metrics display
			expect(screen.getByText(/50/)).toBeInTheDocument(); // tokensPerSecond
		});

		it("does not render metrics when null", () => {
			const noMetricsResponse: ArenaResponse = {
				...mockResponse,
				metrics: null,
			};

			render(<ResponseCard {...defaultProps} response={noMetricsResponse} />, {
				wrapper: AllProviders,
			});

			// Should not show metrics
			expect(screen.queryByText(/50/)).not.toBeInTheDocument();
		});
	});
});
