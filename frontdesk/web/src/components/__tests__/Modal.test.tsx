import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Modal } from "../Modal";

function renderModal(onClose = vi.fn()) {
	render(
		<Modal
			title="Test dialog"
			onClose={onClose}
			actions={
				<>
					<button type="button">First</button>
					<button type="button">Last</button>
				</>
			}
		>
			<input aria-label="field" />
		</Modal>,
	);
	return onClose;
}

describe("Modal", () => {
	it("focuses the first focusable control on open", () => {
		renderModal();
		expect(screen.getByLabelText("field")).toHaveFocus();
	});

	it("traps Tab focus within the dialog (Shift+Tab from first wraps to last)", async () => {
		renderModal();
		expect(screen.getByLabelText("field")).toHaveFocus();
		await userEvent.tab({ shift: true });
		expect(screen.getByRole("button", { name: "Last" })).toHaveFocus();
		await userEvent.tab();
		expect(screen.getByLabelText("field")).toHaveFocus();
	});

	it("closes on Escape", async () => {
		const onClose = renderModal();
		await userEvent.keyboard("{Escape}");
		expect(onClose).toHaveBeenCalled();
	});
});
