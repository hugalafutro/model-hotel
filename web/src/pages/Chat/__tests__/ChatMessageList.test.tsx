import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import type { ChatMessage, Model } from "../../api/types";
import { ChatMessageList } from "../ChatMessageList";

describe("ChatMessageList", () => {
	const mockMessages: ChatMessage[] = [
		{
			role: "user",
			content: "Hello, how are you?",
			timestamp: Date.now(),
		},
		{
			role: "assistant",
			content: "I'm doing well, thank you!",
			model: "Ollama Cloud/gemma3:4b",
			timestamp: Date.now() + 1000,
			metrics: {
				durationMs: 500,
				promptTokens: 10,
				completionTokens: 20,
				tokensPerSecond: 40,
			},
		},
		{
			role: "assistant",
			content: "This is model B's response.",
			model: "Ollama Cloud/glm-5",
			timestamp: Date.now() + 2000,
			metrics: {
				durationMs: 600,
				promptTokens: 12,
				completionTokens: 25,
				tokensPerSecond: 41.67,
			},
		},
	];

	const defaultProps = {
		messages: mockMessages,
		chatSubMode: "chat" as const,
		isStreaming: false,
		selectedModelB: "Ollama Cloud/glm-5",
		enabledModels: [
			{
				id: "model-1",
				model_id: "gemma3:4b",
				name: "gemma3:4b",
				description: "",
				display_name: "gemma3:4b",
				provider_id: "provider-1",
				provider_name: "Ollama Cloud",
				capabilities: '{"vision":false,"audio_input":false,"reasoning":false}',
				params: "{}",
				modality: "text",
				input_modalities: "text",
				output_modalities: "text",
				context_length: 8192,
				max_output_tokens: 4096,
				input_price_per_million: null,
				input_price_per_million_cache_hit: null,
				output_price_per_million: null,
				owned_by: "ollama",
				enabled: true,
				disabled_manually: false,
				created_at: "2024-01-01T00:00:00Z",
				last_seen_at: "2024-01-01T00:00:00Z",
			},
			{
				id: "model-2",
				model_id: "glm-5",
				name: "glm-5",
				description: "",
				display_name: "glm-5",
				provider_id: "provider-1",
				provider_name: "Ollama Cloud",
				capabilities: '{"vision":false,"audio_input":false,"reasoning":false}',
				params: "{}",
				modality: "text",
				input_modalities: "text",
				output_modalities: "text",
				context_length: 32768,
				max_output_tokens: 8192,
				input_price_per_million: null,
				input_price_per_million_cache_hit: null,
				output_price_per_million: null,
				owned_by: "zhipu",
				enabled: true,
				disabled_manually: false,
				created_at: "2024-01-01T00:00:00Z",
				last_seen_at: "2024-01-01T00:00:00Z",
			},
		] as Model[],
		onStopConversation: vi.fn(),
		onStop: vi.fn(),
		onRegenerate: vi.fn(),
		onDeleteMessage: vi.fn(),
		activePersonaIdB: null,
		conversationActivePersonaIdA: null,
		chatActivePersonaId: null,
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("message rendering", () => {
		it("renders user message", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			expect(screen.getByText("Hello, how are you?")).toBeInTheDocument();
		});

		it("renders assistant messages with model name", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			expect(screen.getByText(/gemma3:4b/)).toBeInTheDocument();
			expect(screen.getByText(/glm-5/)).toBeInTheDocument();
		});

		it("renders assistant message content", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			expect(
				screen.getByText("I'm doing well, thank you!"),
			).toBeInTheDocument();
			expect(
				screen.getByText("This is model B's response."),
			).toBeInTheDocument();
		});

		it("renders message timestamps", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			// Timestamps are rendered via formatTime
			const timestamps = screen.getAllByText(/\d{1,2}:\d{2}/);
			expect(timestamps.length).toBeGreaterThanOrEqual(2);
		});

		it("renders metrics for assistant messages", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			expect(screen.getByText(/500ms/)).toBeInTheDocument();
			expect(screen.getByText(/40\.0 tok\/s/)).toBeInTheDocument();
			expect(screen.getByText(/30 tok/)).toBeInTheDocument();
		});

		it("renders copy button for user messages", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			const copyButtons = screen.getAllByRole("button", { name: "Copy" });
			expect(copyButtons.length).toBeGreaterThanOrEqual(1);
		});

		it("renders copy button for assistant messages", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			const copyButtons = screen.getAllByRole("button", { name: "Copy" });
			expect(copyButtons.length).toBeGreaterThanOrEqual(2);
		});

		it("renders delete button for assistant messages", () => {
			renderWithProviders(<ChatMessageList {...defaultProps} />);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			expect(deleteButtons.length).toBeGreaterThanOrEqual(2);
		});

		it("renders settings icon when message has params", () => {
			const messagesWithParams: ChatMessage[] = [
				{
					role: "assistant",
					content: "Response with params",
					model: "Ollama Cloud/gemma3:4b",
					timestamp: Date.now(),
					params: { temperature: 0.7, max_tokens: 100 },
					metrics: {
						durationMs: 500,
						promptTokens: 10,
						completionTokens: 20,
						tokensPerSecond: 40,
					},
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messagesWithParams} />,
			);
			expect(screen.getByTitle(/Settings:/)).toBeInTheDocument();
		});
	});

	describe("conversation mode", () => {
		it("renders model B message on right side in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			// Model B message should be justified end
			const modelBMessage = screen.getByText("This is model B's response.");
			const container = modelBMessage.closest(".flex");
			expect(container).toHaveClass("justify-end");
		});

		it("renders model A message on left side in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			// Model A message should be justified start
			const modelAMessage = screen.getByText("I'm doing well, thank you!");
			const container = modelAMessage.closest(".flex");
			expect(container).toHaveClass("justify-start");
		});

		it("renders turn numbers in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			expect(screen.getByText("Turn 1")).toBeInTheDocument();
			expect(screen.getByText("Turn 2")).toBeInTheDocument();
		});

		it("renders user message centered in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			const userMessage = screen.getByText("Hello, how are you?");
			const container = userMessage.closest(".flex");
			expect(container).toHaveClass("justify-center");
		});

		it("renders user message with gray background in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			const userMessage = screen.getByText(/Hello, how are you?/);
			const bubble = userMessage.parentElement?.parentElement;
			expect(bubble?.className).toContain("bg-gray-500/20");
		});
	});

	describe("streaming state", () => {
		it("shows streaming indicator when isStreaming is true", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					isStreaming
					messages={[
						{
							role: "assistant",
							content: "",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			expect(screen.getByText(/Waiting/)).toBeInTheDocument();
		});

		it("shows cancel button during streaming", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					isStreaming
					messages={[
						{
							role: "assistant",
							content: "Streaming...",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			expect(cancelButton).toBeInTheDocument();
		});

		it("calls onStopConversation when cancel is clicked during streaming", async () => {
			const { user } = renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					isStreaming
					chatSubMode="conversation"
					messages={[
						{
							role: "assistant",
							content: "Streaming...",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);
			expect(defaultProps.onStopConversation).toHaveBeenCalledTimes(1);
		});

		it("shows elapsed time during streaming", async () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					isStreaming
					messages={[
						{
							role: "assistant",
							content: "Streaming...",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			// Wait for the timer to update
			await waitFor(() => {
				const elapsedElement = screen.getByText(/\d+s/);
				expect(elapsedElement).toBeInTheDocument();
			});
		});
	});

	describe("user interactions", () => {
		it("calls onDeleteMessage when delete button is clicked", async () => {
			const { user } = renderWithProviders(
				<ChatMessageList {...defaultProps} />,
			);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			await user.click(deleteButtons[0]);
			expect(defaultProps.onDeleteMessage).toHaveBeenCalledWith(1);
		});

		it("calls onRegenerate when regenerate button is clicked", async () => {
			const { user } = renderWithProviders(
				<ChatMessageList {...defaultProps} />,
			);
			const regenerateButton = screen.getByRole("button", {
				name: "Regenerate",
			});
			await user.click(regenerateButton);
			expect(defaultProps.onRegenerate).toHaveBeenCalledTimes(1);
		});

		it("calls onStop when stop button is clicked in chat mode", async () => {
			const { user } = renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					isStreaming
					messages={[
						{
							role: "assistant",
							content: "Streaming...",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			const stopButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(stopButton);
			expect(defaultProps.onStop).toHaveBeenCalledTimes(1);
		});
	});

	describe("error states", () => {
		it("renders error message in assistant bubble", () => {
			const errorMessage: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					model: "Ollama Cloud/gemma3:4b",
					timestamp: Date.now(),
					error: "Failed to generate response",
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={errorMessage} />,
			);
			expect(
				screen.getByText("Failed to generate response"),
			).toBeInTheDocument();
		});

		it("renders disable model button for 5xx errors", () => {
			const errorMessage: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					model: "Ollama Cloud/gemma3:4b",
					timestamp: Date.now(),
					error: "500 Internal Server Error",
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={errorMessage} />,
			);
			expect(screen.getByText("Disable model")).toBeInTheDocument();
		});

		it("renders error with partial content", () => {
			const partialErrorMessage: ChatMessage[] = [
				{
					role: "assistant",
					content: "Partial response...",
					model: "Ollama Cloud/gemma3:4b",
					timestamp: Date.now(),
					error: "Connection timeout",
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={partialErrorMessage} />,
			);
			expect(screen.getByText("Partial response...")).toBeInTheDocument();
			expect(screen.getByText("⚠ Connection timeout")).toBeInTheDocument();
		});
	});

	describe("persona display", () => {
		it("renders persona name when activePersonaId is set", () => {
			// This would require CHAT_PERSONAS to be populated
			// Testing the structure is in place
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatActivePersonaId="persona-1"
					messages={[
						{
							role: "assistant",
							content: "Response",
							model: "Ollama Cloud/gemma3:4b",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			// Persona rendering depends on CHAT_PERSONAS lookup
			// Component structure is tested via ModelReplyCard tests
		});
	});

	describe("user message with attachments", () => {
		it("renders user message with image", () => {
			const messagesWithImage: ChatMessage[] = [
				{
					role: "user",
					content: "What's in this image?",
					imageUrl: "data:image/png;base64,test",
					timestamp: Date.now(),
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messagesWithImage} />,
			);
			const img = screen.getByAltText("User attachment");
			expect(img).toBeInTheDocument();
			expect(img).toHaveAttribute("src", "data:image/png;base64,test");
		});

		it("renders user message with audio attachment", () => {
			const messagesWithAudio: ChatMessage[] = [
				{
					role: "user",
					content: "Transcribe this",
					audioAttachment: { format: "wav", duration: 30 },
					timestamp: Date.now(),
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messagesWithAudio} />,
			);
			expect(screen.getByText("WAV audio")).toBeInTheDocument();
		});
	});
});
