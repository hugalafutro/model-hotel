import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
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

	it("does not close on Escape or backdrop click when not dismissible", async () => {
		const onClose = vi.fn();
		render(
			<Modal title="Busy dialog" onClose={onClose} dismissible={false}>
				<input aria-label="field" />
			</Modal>,
		);
		await userEvent.keyboard("{Escape}");
		// The backdrop is the dialog's parent; mousedown on it would normally close.
		const backdrop = screen.getByRole("dialog").parentElement as HTMLElement;
		await userEvent.click(backdrop);
		expect(onClose).not.toHaveBeenCalled();
	});

	it("does not steal focus when the parent re-renders (new onClose identity)", async () => {
		// Mirrors the real panels: an inline-arrow onClose plus an in-modal control
		// that re-renders the parent. Focus must stay where the user put it.
		function Harness() {
			const [n, setN] = useState(0);
			return (
				<Modal
					title="t"
					onClose={() => {}}
					actions={
						<button type="button" onClick={() => setN(n + 1)}>
							Bump {n}
						</button>
					}
				>
					<input aria-label="field" />
				</Modal>
			);
		}
		render(<Harness />);
		const bump = screen.getByRole("button", { name: /Bump/ });
		bump.focus();
		expect(bump).toHaveFocus();
		await userEvent.click(bump); // re-renders with a fresh onClose identity
		expect(screen.getByRole("button", { name: /Bump/ })).toHaveFocus();
	});

	it("restores focus to the trigger on close", async () => {
		function Harness() {
			const [open, setOpen] = useState(false);
			return (
				<>
					<button type="button" onClick={() => setOpen(true)}>
						Open
					</button>
					{open && (
						<Modal
							title="t"
							onClose={() => setOpen(false)}
							actions={
								<button type="button" onClick={() => setOpen(false)}>
									OK
								</button>
							}
						>
							<input aria-label="field" />
						</Modal>
					)}
				</>
			);
		}
		render(<Harness />);
		const opener = screen.getByRole("button", { name: "Open" });
		await userEvent.click(opener); // opener was focused at mount → captured
		expect(screen.getByLabelText("field")).toHaveFocus();
		await userEvent.keyboard("{Escape}"); // close → focus returns to opener
		await waitFor(() => expect(opener).toHaveFocus());
	});
});
