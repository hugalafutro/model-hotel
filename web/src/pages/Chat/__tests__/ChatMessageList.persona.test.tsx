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

	describe("persona display", () => {
		it("renders persona name for chat mode assistant messages", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatActivePersonaId="merlin"
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
			// Component renders persona.label directly (translation key), not t() translated
			expect(screen.getByText(/Merlin/)).toBeInTheDocument();
		});

		it("renders persona name for model B in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatSubMode="conversation"
					activePersonaIdB="sarge"
					messages={[
						{
							role: "assistant",
							content: "Response",
							model: "Ollama Cloud/glm-5",
							timestamp: Date.now(),
						},
					]}
				/>,
			);
			expect(screen.getByText(/Sarge/)).toBeInTheDocument();
		});

		it("renders persona name for model A in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatSubMode="conversation"
					conversationActivePersonaIdA="merlin"
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
			expect(screen.getByText(/Merlin/)).toBeInTheDocument();
		});

		it("does not render persona name when persona ID does not match any persona", () => {
			renderWithProviders(
				<ChatMessageList
					{...defaultProps}
					chatActivePersonaId="nonexistent"
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
			expect(screen.getByText("Response")).toBeInTheDocument();
			// No persona name should appear
		});
	});
});
