import "@testing-library/jest-dom";

import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { RestoreConfirmModal } from "../RestoreConfirmModal";

describe("RestoreConfirmModal", () => {
	it("renders when open", () => {
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		expect(
			screen.getByRole("heading", { name: "Restore Database Backup" }),
		).toBeInTheDocument();
		expect(
			screen.getByText("This will permanently overwrite all data"),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Restore Database" }),
		).toBeInTheDocument();
	});

	it("does not render when closed", () => {
		renderWithProviders(
			<RestoreConfirmModal
				open={false}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		expect(
			screen.queryByRole("heading", { name: "Restore Database Backup" }),
		).not.toBeInTheDocument();
	});

	it("disables confirm button when admin token is empty", () => {
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		const confirmButton = screen.getByRole("button", {
			name: "Restore Database",
		});
		expect(confirmButton).toBeDisabled();
	});

	it("enables confirm button when admin token is entered", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		const input = screen.getByLabelText("Confirm with admin token");
		await user.type(input, "test-token");

		const confirmButton = screen.getByRole("button", {
			name: "Restore Database",
		});
		expect(confirmButton).toBeEnabled();
	});

	it("calls onConfirm with admin token and signature when one is pasted", async () => {
		const user = userEvent.setup();
		const onConfirm = vi.fn();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={onConfirm}
				isPending={false}
			/>,
		);

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"  test-token  ",
		);
		const hex = "deadbeef".repeat(8);
		await user.type(
			screen.getByLabelText("Backup signature (optional)"),
			` ${hex}\n`,
		);

		await user.click(screen.getByRole("button", { name: "Restore Database" }));

		// A signed restore needs no second step, and the paste travels trimmed.
		expect(onConfirm).toHaveBeenCalledWith("test-token", hex);
		expect(
			screen.queryByRole("heading", { name: "Restore an unsigned backup?" }),
		).not.toBeInTheDocument();
	});

	it("blocks a malformed signature before anything is uploaded", async () => {
		const user = userEvent.setup();
		const onConfirm = vi.fn();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={onConfirm}
				isPending={false}
			/>,
		);

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		const sigField = screen.getByLabelText("Backup signature (optional)");
		await user.type(sigField, "not-a-signature");

		// A sidecar is 64 hex chars; a bad paste is flagged inline and the
		// restore button stays disabled, so no dump is uploaded to be refused.
		expect(screen.getByRole("alert")).toHaveTextContent(
			"A signature is 64 hexadecimal characters; check the paste.",
		);
		expect(sigField).toHaveAttribute("aria-invalid", "true");
		const restoreButton = screen.getByRole("button", {
			name: "Restore Database",
		});
		expect(restoreButton).toBeDisabled();
		await user.click(restoreButton);
		expect(onConfirm).not.toHaveBeenCalled();

		// Fixing the paste clears the error and enables the button.
		await user.clear(sigField);
		await user.type(sigField, "0123456789abcdef".repeat(4));
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
		expect(restoreButton).toBeEnabled();
	});

	it("wires the help text to the signature field", () => {
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);
		expect(
			screen.getByLabelText("Backup signature (optional)"),
		).toHaveAccessibleDescription(/Copy signature/);
	});

	it("does not restore an unsigned dump until the second confirm", async () => {
		const user = userEvent.setup();
		const onConfirm = vi.fn();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={onConfirm}
				isPending={false}
			/>,
		);

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		await user.click(screen.getByRole("button", { name: "Restore Database" }));

		// Empty signature: the click opens the speed bump instead of restoring.
		expect(onConfirm).not.toHaveBeenCalled();
		expect(
			screen.getByRole("heading", { name: "Restore an unsigned backup?" }),
		).toBeInTheDocument();
		expect(
			screen.getByText(
				"This dump is unsigned and its contents cannot be verified",
			),
		).toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: "Restore anyway" }));
		expect(onConfirm).toHaveBeenCalledWith("test-token", "");
	});

	it("goes inert on both stages when the card becomes managed", async () => {
		const user = userEvent.setup();
		const onConfirm = vi.fn();
		const dialog = (managed: boolean) => (
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={onConfirm}
				isPending={false}
				managed={managed}
			/>
		);
		const { rerender } = renderWithProviders(dialog(false));
		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		await user.click(screen.getByRole("button", { name: "Restore Database" }));
		expect(
			screen.getByRole("heading", { name: "Restore an unsigned backup?" }),
		).toBeInTheDocument();

		// The managed poll flips while the operator sits on the second stage:
		// the dialog stays, the restore button does not.
		rerender(dialog(true));
		const anyway = screen.getByRole("button", { name: "Restore anyway" });
		expect(anyway).toBeDisabled();
		await user.click(anyway);
		expect(onConfirm).not.toHaveBeenCalled();
	});

	it("cannot start a restore from the first stage while managed", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
				managed
			/>,
		);
		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		expect(
			screen.getByRole("button", { name: "Restore Database" }),
		).toBeDisabled();
	});

	it("remounts and refocuses the dialog when switching to the unsigned confirm", async () => {
		// The two stages are separate Modal instances (distinct keys), so the
		// switch moves focus into the new dialog instead of leaving it on the
		// button that just unmounted; a screen reader is told something changed.
		const user = userEvent.setup();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);
		const formDialog = screen.getByRole("dialog");

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		await user.click(screen.getByRole("button", { name: "Restore Database" }));

		const unsignedDialog = screen.getByRole("dialog");
		expect(unsignedDialog).not.toBe(formDialog);
		expect(unsignedDialog).toHaveAccessibleName("Restore an unsigned backup?");
		expect(document.activeElement).toBe(unsignedDialog);
	});

	it("goes back from the unsigned confirm without restoring", async () => {
		const user = userEvent.setup();
		const onConfirm = vi.fn();
		const onClose = vi.fn();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={onClose}
				onConfirm={onConfirm}
				isPending={false}
			/>,
		);

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		await user.click(screen.getByRole("button", { name: "Restore Database" }));
		await user.click(screen.getByRole("button", { name: "Back" }));

		// Back returns to the form with the token still filled in, so the user
		// can paste the signature they went to fetch.
		expect(onConfirm).not.toHaveBeenCalled();
		expect(onClose).not.toHaveBeenCalled();
		expect(screen.getByLabelText("Confirm with admin token")).toHaveValue(
			"test-token",
		);
		expect(
			screen.getByRole("button", { name: "Restore Database" }),
		).toBeEnabled();
	});

	it("shows Restoring… on the unsigned confirm while pending", async () => {
		const user = userEvent.setup();
		const { rerender } = renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		await user.type(
			screen.getByLabelText("Confirm with admin token"),
			"test-token",
		);
		await user.click(screen.getByRole("button", { name: "Restore Database" }));

		rerender(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={true}
			/>,
		);

		expect(screen.getByRole("button", { name: "Restoring…" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "Back" })).toBeDisabled();
	});

	it("calls onClose on cancel", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={onClose}
				onConfirm={vi.fn()}
				isPending={false}
			/>,
		);

		const cancelButton = screen.getByRole("button", { name: "Cancel" });
		await user.click(cancelButton);

		expect(onClose).toHaveBeenCalled();
	});

	it("shows Restoring… when pending", async () => {
		renderWithProviders(
			<RestoreConfirmModal
				open={true}
				onClose={vi.fn()}
				onConfirm={vi.fn()}
				isPending={true}
			/>,
		);

		const confirmButton = screen.getByRole("button", {
			name: "Restoring…",
		});
		expect(confirmButton).toBeDisabled();

		const input = screen.getByLabelText("Confirm with admin token");
		expect(input).toBeDisabled();

		const cancelButton = screen.getByRole("button", { name: "Cancel" });
		expect(cancelButton).toBeDisabled();
	});
});
