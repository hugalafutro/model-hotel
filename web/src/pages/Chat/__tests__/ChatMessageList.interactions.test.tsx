import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import { ChatMessageList } from "../ChatMessageList";
import { createDefaultProps } from "./ChatMessageList.helpers";

describe("ChatMessageList", () => {
	const defaultProps = createDefaultProps({
		enabledModels: [createDefaultProps().enabledModels[0]],
	});

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
