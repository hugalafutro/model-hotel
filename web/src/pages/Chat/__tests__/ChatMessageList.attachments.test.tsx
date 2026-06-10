import { screen } from "@testing-library/react";
import type { ChatMessage, Model } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { ChatMessageList } from "../ChatMessageList";

describe("ChatMessageList", () => {
	const defaultProps = {
		messages: [] as ChatMessage[],
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
					audioAttachment: { format: "wav", data: "base64-audio-data" },
					timestamp: Date.now(),
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messagesWithAudio} />,
			);
			expect(screen.getByText("WAV audio")).toBeInTheDocument();
		});

		it("renders user message with only image and no text", () => {
			const messagesWithOnlyImage: ChatMessage[] = [
				{
					role: "user",
					content: "",
					imageUrl: "data:image/png;base64,test",
					timestamp: Date.now(),
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messagesWithOnlyImage} />,
			);
			const img = screen.getByAltText("User attachment");
			expect(img).toBeInTheDocument();
			// Should still render the message bubble with the image
		});
	});
});
