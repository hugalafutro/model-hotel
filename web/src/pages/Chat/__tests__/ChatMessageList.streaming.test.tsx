import { screen, waitFor } from "@testing-library/react";
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
});
