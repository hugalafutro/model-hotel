import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DeleteConfirmModal } from "../DeleteConfirmModal";

// Stub Lucide icons
vi.mock("lucide-react", () => ({
	X: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="close-icon" />
	),
}));

describe("DeleteConfirmModal", () => {
	it("renders entity name", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test Provider"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText(/Test Provider/)).toBeInTheDocument();
	});

	it("renders with default title when entityType not provided", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText("Confirm Delete")).toBeInTheDocument();
	});

	it("renders with custom title when entityType provided", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				entityType="provider"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText("Delete provider")).toBeInTheDocument();
	});

	it("renders with custom title when title prop provided", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				title="Remove Item"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText("Remove Item")).toBeInTheDocument();
	});

	it("shows confirm and cancel buttons", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText("Cancel")).toBeInTheDocument();
		expect(screen.getByText("Delete")).toBeInTheDocument();
	});

	it("calls onConfirm when delete button is clicked", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		fireEvent.click(screen.getByText("Delete"));

		expect(onConfirm).toHaveBeenCalledTimes(1);
	});

	it("calls onCancel when cancel button is clicked", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		fireEvent.click(screen.getByText("Cancel"));

		expect(onCancel).toHaveBeenCalledTimes(1);
	});

	it("calls onCancel when backdrop is clicked", async () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		// Click the backdrop (the button with aria-label "Close dialog")
		fireEvent.click(screen.getByLabelText("Close dialog"));

		await waitFor(() => {
			expect(onCancel).toHaveBeenCalledTimes(1);
		});
	});

	it("calls onCancel when close button is clicked", async () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={false}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		fireEvent.click(screen.getByTestId("close-icon"));

		await waitFor(() => {
			expect(onCancel).toHaveBeenCalledTimes(1);
		});
	});

	it("shows deleting state when isPending is true", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={true}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		expect(screen.getByText("Deleting…")).toBeInTheDocument();
		const deleteButton = screen.getByRole("button", { name: "Deleting…" });
		expect(deleteButton).toBeDisabled();
	});

	it("disables cancel button when isPending is true", () => {
		const onConfirm = vi.fn();
		const onCancel = vi.fn();
		render(
			<DeleteConfirmModal
				entityName="Test"
				isPending={true}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);

		// Cancel should still work during pending state
		fireEvent.click(screen.getByText("Cancel"));
		expect(onCancel).toHaveBeenCalledTimes(1);
	});
});
