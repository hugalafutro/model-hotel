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
		enabledModels: [] as Model[],
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

	describe("system message filtering", () => {
		it("does not render system messages", () => {
			const messages: ChatMessage[] = [
				{
					role: "system",
					content: "You are a helpful assistant",
					timestamp: Date.now(),
				},
				{
					role: "user",
					content: "Hello",
					timestamp: Date.now() + 1000,
				},
			];
			renderWithProviders(
				<ChatMessageList {...defaultProps} messages={messages} />,
			);
			expect(screen.getByText("Hello")).toBeInTheDocument();
			expect(
				screen.queryByText("You are a helpful assistant"),
			).not.toBeInTheDocument();
		});
	});
});
