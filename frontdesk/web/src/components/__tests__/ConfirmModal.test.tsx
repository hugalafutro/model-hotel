import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConfirmModal } from "../ConfirmModal";

describe("ConfirmModal busy state", () => {
	it("shows the busy label, disables both buttons, and renders a spinner while busy", () => {
		const { container } = render(
			<ConfirmModal
				title="Replace config?"
				confirmLabel="Replace now"
				busy
				busyLabel="Replacing config..."
				onConfirm={vi.fn()}
				onClose={vi.fn()}
			>
				<p>body</p>
			</ConfirmModal>,
		);

		const confirm = screen.getByRole("button", { name: /Replacing config/ });
		expect(confirm).toBeDisabled();
		expect(confirm).toHaveAttribute("aria-busy", "true");
		// Cancel is disabled too: the action is in flight and the modal closes itself.
		expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
		expect(container.querySelector(".fd-spinner")).not.toBeNull();
		// The idle label is not shown while busy.
		expect(screen.queryByRole("button", { name: "Replace now" })).toBeNull();
	});

	it("shows the confirm label and an enabled cancel when not busy", () => {
		render(
			<ConfirmModal
				title="Replace config?"
				confirmLabel="Replace now"
				busyLabel="Replacing config..."
				onConfirm={vi.fn()}
				onClose={vi.fn()}
			>
				<p>body</p>
			</ConfirmModal>,
		);

		expect(screen.getByRole("button", { name: "Replace now" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
	});
});
