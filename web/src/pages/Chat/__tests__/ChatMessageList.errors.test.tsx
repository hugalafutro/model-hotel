import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import type { ChatMessage, Model } from "../../api/types";
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

		it("renders disable model button for model B error in conversation mode", () => {
			const errorMessages: ChatMessage[] = [
				{ role: "user", content: "Hello", timestamp: Date.now() },
				{
					role: "assistant",
					content: "",
					model: "Ollama Cloud/glm-5",
					timestamp: Date.now() + 1000,
					error: "500 Internal Server Error",
				},
			];
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatSubMode="conversation"
					messages={errorMessages}
				/>,
			);
			expect(screen.getByText("Disable model")).toBeInTheDocument();
		});
	});
});
