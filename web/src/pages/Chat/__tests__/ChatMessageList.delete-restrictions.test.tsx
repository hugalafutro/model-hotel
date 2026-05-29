import { screen } from "@testing-library/react";
import { renderWithProviders } from "../../../test/utils";
import { ChatMessageList } from "../ChatMessageList";
import { createDefaultProps } from "./ChatMessageList.helpers";

describe("ChatMessageList", () => {
	const defaultProps = createDefaultProps();

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("conversation mode delete restrictions", () => {
		it("only shows delete on last assistant message in conversation mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="conversation" />,
			);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			expect(deleteButtons.length).toBe(1);
		});

		it("shows delete on all assistant messages in chat mode", () => {
			renderWithProviders(
				<ChatMessageList {...defaultProps} chatSubMode="chat" />,
			);
			const deleteButtons = screen.getAllByRole("button", {
				name: "Delete message",
			});
			expect(deleteButtons.length).toBe(2);
		});
	});
});
