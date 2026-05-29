import { screen } from "@testing-library/react";
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

	describe("conversation mode delete restrictions", () => {
		it("only shows delete on last assistant message in conversation mode", () => {
			// With multiple assistant messages, only the last one should have delete
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			// Only the last assistant message (index 2, model B) should have delete
			expect(deleteButtons.length).toBe(1);
		});

		it("shows delete on all assistant messages in chat mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="chat" />,
			);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			// Both assistant messages should have delete in chat mode
			expect(deleteButtons.length).toBe(2);
		});
	});
});
