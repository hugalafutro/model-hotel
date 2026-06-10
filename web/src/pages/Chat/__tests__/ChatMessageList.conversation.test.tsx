import { screen } from "@testing-library/react";
import type { ChatMessage, Model } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
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
});
