import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DeleteConfirmModal } from "../DeleteConfirmModal";

// Stub Lucide icons
vi.mock("@/lib/icons", () => ({
	X: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="close-icon" />
	),
}));

describe("DeleteConfirmModal", () => {
	const onConfirm = vi.fn();
	const onCancel = vi.fn();
	const baseProps = {
		confirmText: "Test Provider",
		entityName: "Test Provider",
		isPending: false,
		onConfirm,
		onCancel,
	};
	const deleteButton = () => screen.getByRole("button", { name: "Delete" });
	const input = () => screen.getByTestId("delete-confirm-input");

	beforeEach(() => {
		onConfirm.mockClear();
		onCancel.mockClear();
	});

	it("renders the entity name in the sentence and as the text to type", () => {
		render(<DeleteConfirmModal {...baseProps} />);

		expect(
			screen.getByText(
				"Are you sure you want to delete Test Provider? This action cannot be undone.",
			),
		).toBeInTheDocument();
		expect(screen.getByText("To confirm, type:")).toBeInTheDocument();
		expect(screen.getByTestId("delete-confirm-expected")).toHaveTextContent(
			"Test Provider",
		);
	});

	it("renders with default title when entityType not provided", () => {
		render(<DeleteConfirmModal {...baseProps} />);
		expect(screen.getByText("Confirm Delete")).toBeInTheDocument();
	});

	it("renders with custom title when entityType provided", () => {
		render(<DeleteConfirmModal {...baseProps} entityType="provider" />);
		expect(screen.getByText("Delete provider")).toBeInTheDocument();
	});

	it("renders with custom title when title prop provided", () => {
		render(<DeleteConfirmModal {...baseProps} title="Remove Item" />);
		expect(screen.getByText("Remove Item")).toBeInTheDocument();
	});

	it("replaces the sentence with message and lists fields", () => {
		render(
			<DeleteConfirmModal
				{...baseProps}
				confirmText="confirm"
				message="This will permanently delete 2 disabled models."
				fields={["p/a", "p/b"]}
			/>,
		);

		expect(
			screen.getByText("This will permanently delete 2 disabled models."),
		).toBeInTheDocument();
		expect(screen.queryByText(/Are you sure/)).toBeNull();
		expect(screen.getByText("p/a")).toBeInTheDocument();
		expect(screen.getByText("p/b")).toBeInTheDocument();
		expect(screen.getByTestId("delete-confirm-expected")).toHaveTextContent(
			"confirm",
		);
	});

	it("focuses the text field on open so typing can start at once", () => {
		render(<DeleteConfirmModal {...baseProps} />);
		expect(document.activeElement).toBe(input());
	});

	it("keeps the delete button disabled until the exact text is typed", async () => {
		const user = userEvent.setup();
		render(<DeleteConfirmModal {...baseProps} />);

		expect(deleteButton()).toBeDisabled();
		await user.type(input(), "Test Provide");
		expect(deleteButton()).toBeDisabled();
		await user.type(input(), "r");
		expect(deleteButton()).toBeEnabled();
		// Nothing else: a trailing character disarms it again.
		await user.type(input(), " ");
		expect(deleteButton()).toBeDisabled();
	});

	it("is case-sensitive", async () => {
		const user = userEvent.setup();
		render(<DeleteConfirmModal {...baseProps} />);

		await user.type(input(), "test provider");
		expect(deleteButton()).toBeDisabled();
		await user.click(deleteButton());
		expect(onConfirm).not.toHaveBeenCalled();
	});

	it("calls onConfirm when the armed delete button is clicked", async () => {
		const user = userEvent.setup();
		render(<DeleteConfirmModal {...baseProps} />);

		await user.type(input(), "Test Provider");
		await user.click(deleteButton());
		expect(onConfirm).toHaveBeenCalledTimes(1);
	});

	it("submits on Enter only once armed", async () => {
		const user = userEvent.setup();
		render(<DeleteConfirmModal {...baseProps} />);

		await user.type(input(), "Test{Enter}");
		expect(onConfirm).not.toHaveBeenCalled();
		await user.type(input(), " Provider{Enter}");
		expect(onConfirm).toHaveBeenCalledTimes(1);
	});

	it("calls onCancel when cancel button is clicked", () => {
		render(<DeleteConfirmModal {...baseProps} />);
		fireEvent.click(screen.getByText("Cancel"));
		expect(onCancel).toHaveBeenCalledTimes(1);
	});

	it("calls onCancel when backdrop is clicked", async () => {
		render(<DeleteConfirmModal {...baseProps} />);
		fireEvent.click(screen.getByLabelText("Close dialog"));
		await waitFor(() => {
			expect(onCancel).toHaveBeenCalledTimes(1);
		});
	});

	it("calls onCancel when close button is clicked", async () => {
		render(<DeleteConfirmModal {...baseProps} />);
		fireEvent.click(screen.getByTestId("close-icon"));
		await waitFor(() => {
			expect(onCancel).toHaveBeenCalledTimes(1);
		});
	});

	it("shows deleting state and stays disabled while pending", async () => {
		const user = userEvent.setup();
		render(<DeleteConfirmModal {...baseProps} isPending={true} />);

		await user.type(input(), "Test Provider");
		const pending = screen.getByRole("button", { name: "Deleting…" });
		expect(pending).toBeDisabled();
		await user.type(input(), "{Enter}");
		expect(onConfirm).not.toHaveBeenCalled();
	});
});
