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
});
