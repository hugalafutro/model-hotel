import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BlurCommitInput } from "../BlurCommitInput";

describe("BlurCommitInput", () => {
	it("shows the value it is given", () => {
		render(<BlurCommitInput id="f" value="stored" onCommit={vi.fn()} />);
		expect(screen.getByRole("textbox")).toHaveValue("stored");
	});

	it("holds the edit until blur, then reports it once", async () => {
		const user = userEvent.setup();
		const onCommit = vi.fn();
		render(<BlurCommitInput id="f" value="" onCommit={onCommit} />);

		await user.type(screen.getByRole("textbox"), "abc");
		expect(onCommit).not.toHaveBeenCalled();

		await user.tab();
		expect(onCommit).toHaveBeenCalledExactlyOnceWith("abc");
	});

	it("commits on Enter", async () => {
		const user = userEvent.setup();
		const onCommit = vi.fn();
		render(<BlurCommitInput id="f" value="" onCommit={onCommit} />);

		await user.type(screen.getByRole("textbox"), "abc{Enter}");
		expect(onCommit).toHaveBeenCalledExactlyOnceWith("abc");
	});

	it("reports nothing when the text comes back unchanged", async () => {
		const user = userEvent.setup();
		const onCommit = vi.fn();
		render(<BlurCommitInput id="f" value="ab" onCommit={onCommit} />);

		const input = screen.getByRole("textbox");
		await user.type(input, "c{Backspace}");
		await user.tab();
		expect(onCommit).not.toHaveBeenCalled();
	});

	it("follows the value again once the edit is committed", async () => {
		const user = userEvent.setup();
		const { rerender } = render(
			<BlurCommitInput id="f" value="old" onCommit={vi.fn()} />,
		);

		await user.clear(screen.getByRole("textbox"));
		await user.type(screen.getByRole("textbox"), "new");
		await user.tab();
		// A save the server rejected leaves `value` where it was, and the field
		// snaps back to it rather than keeping the dead draft.
		rerender(<BlurCommitInput id="f" value="old" onCommit={vi.fn()} />);
		expect(screen.getByRole("textbox")).toHaveValue("old");
	});

	it("takes a monospace class and a test id", () => {
		render(
			<BlurCommitInput
				id="f"
				value=""
				onCommit={vi.fn()}
				mono
				testId="the-field"
			/>,
		);
		const input = screen.getByTestId("the-field");
		expect(input.className).toContain("font-mono");
	});
});
