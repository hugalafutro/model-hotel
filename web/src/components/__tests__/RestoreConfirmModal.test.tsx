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
			screen.getByText("This will permanently destroy all current data"),
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

	it("calls onConfirm with admin token on confirm", async () => {
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

		const input = screen.getByLabelText("Confirm with admin token");
		await user.type(input, "  test-token  ");

		const confirmButton = screen.getByRole("button", {
			name: "Restore Database",
		});
		await user.click(confirmButton);

		expect(onConfirm).toHaveBeenCalledWith("test-token");
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
