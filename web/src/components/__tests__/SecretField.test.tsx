import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { SecretField } from "../SecretField";

// Controlled harness: the real panels own the draft state, so mirror that here
// to exercise the reveal-gating which depends on the value being non-empty.
function Harness({
	configured = false,
	onClear = vi.fn(),
}: {
	configured?: boolean;
	onClear?: () => void;
}) {
	const [value, setValue] = useState("");
	return (
		<SecretField
			id="sf"
			testId="sf"
			value={value}
			configured={configured}
			placeholder="placeholder"
			onChange={setValue}
			onCommit={() => {}}
			onClear={onClear}
			toggleLabel="toggle"
			clearLabel="clear"
			clearConfirmTitle="Clear secret?"
			clearConfirmMessage="It will need to be pasted again."
		/>
	);
}

describe("SecretField", () => {
	it("hides the reveal eye until a secret is being entered", async () => {
		const user = userEvent.setup();
		render(<Harness />);

		// Nothing entered: nothing to reveal, so no eye.
		expect(screen.queryByTestId("sf-reveal")).not.toBeInTheDocument();

		const input = screen.getByTestId("sf-input") as HTMLInputElement;
		expect(input.type).toBe("password");

		await user.type(input, "abc");
		const reveal = screen.getByTestId("sf-reveal");
		await user.click(reveal);
		expect(input.type).toBe("text");
		await user.click(reveal);
		expect(input.type).toBe("password");
	});

	// Tab from the input to the eye must not commit the draft: the commit
	// would clear it and unmount the eye under the keyboard user's focus.
	it("keeps the draft while focus moves to the reveal control", async () => {
		const user = userEvent.setup();
		const onCommit = vi.fn();
		function Group() {
			const [value, setValue] = useState("");
			return (
				<>
					<SecretField
						id="sf"
						testId="sf"
						value={value}
						configured={false}
						placeholder="placeholder"
						onChange={setValue}
						onCommit={onCommit}
						onClear={() => {}}
						toggleLabel="toggle"
						clearLabel="clear"
						clearConfirmTitle="Clear secret?"
						clearConfirmMessage="It will need to be pasted again."
					/>
					<button type="button">elsewhere</button>
				</>
			);
		}
		render(<Group />);
		const input = screen.getByTestId("sf-input") as HTMLInputElement;
		await user.type(input, "abc");
		await user.tab();
		expect(onCommit).not.toHaveBeenCalled();
		expect(screen.getByTestId("sf-reveal")).toHaveFocus();
		await user.keyboard("{Enter}");
		expect(input.type).toBe("text");
		await user.tab();
		expect(screen.getByRole("button", { name: "elsewhere" })).toHaveFocus();
		expect(onCommit).toHaveBeenCalledTimes(1);
	});

	it("re-masks and hides the eye when the draft is cleared", async () => {
		const user = userEvent.setup();
		render(<Harness />);
		const input = screen.getByTestId("sf-input") as HTMLInputElement;

		await user.type(input, "abc");
		await user.click(screen.getByTestId("sf-reveal"));
		expect(input.type).toBe("text");

		await user.clear(input);
		expect(screen.queryByTestId("sf-reveal")).not.toBeInTheDocument();
		expect(input.type).toBe("password");
	});

	it("shows no clear control when no secret is configured", () => {
		render(<Harness configured={false} />);
		expect(screen.queryByTestId("sf-clear")).not.toBeInTheDocument();
	});

	it("gates clearing behind a confirm dialog", async () => {
		const user = userEvent.setup();
		const onClear = vi.fn();
		render(<Harness configured onClear={onClear} />);

		await user.click(screen.getByTestId("sf-clear"));
		// Confirm dialog appears; clearing has not happened yet.
		expect(onClear).not.toHaveBeenCalled();
		expect(screen.getByTestId("sf-confirm")).toBeInTheDocument();

		await user.click(screen.getByTestId("sf-confirm"));
		await waitFor(() => expect(onClear).toHaveBeenCalledTimes(1));
	});

	// Opening the clear confirmation moves focus into the dialog; that is not
	// leaving the field, so an edited draft stays pending and Cancel keeps it.
	it("does not commit the draft when the clear dialog opens", async () => {
		const user = userEvent.setup();
		const onCommit = vi.fn();
		function Group() {
			const [value, setValue] = useState("");
			return (
				<SecretField
					id="sf"
					testId="sf"
					value={value}
					configured
					placeholder="placeholder"
					onChange={setValue}
					onCommit={onCommit}
					onClear={() => {}}
					toggleLabel="toggle"
					clearLabel="clear"
					clearConfirmTitle="Clear secret?"
					clearConfirmMessage="It will need to be pasted again."
				/>
			);
		}
		render(<Group />);
		await user.type(screen.getByTestId("sf-input"), "pending-replacement");
		await user.click(screen.getByTestId("sf-clear"));
		expect(screen.getByTestId("sf-confirm")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Cancel" }));
		expect(onCommit).not.toHaveBeenCalled();
	});

	it("does not clear when the confirm dialog is cancelled", async () => {
		const user = userEvent.setup();
		const onClear = vi.fn();
		render(<Harness configured onClear={onClear} />);

		await user.click(screen.getByTestId("sf-clear"));
		await user.click(screen.getByRole("button", { name: "Cancel" }));
		expect(onClear).not.toHaveBeenCalled();
	});
});
