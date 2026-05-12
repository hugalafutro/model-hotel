import { fireEvent, render, screen } from "@testing-library/react";
import { ConfirmDeleteButton } from "../ConfirmDeleteButton";

describe("ConfirmDeleteButton", () => {
	it("shows delete button initially", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} />);

		expect(screen.getByText("Delete Key")).toBeInTheDocument();
		expect(screen.queryByText("Are you sure?")).not.toBeInTheDocument();
	});

	it("enters confirm state when delete button is clicked", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} />);

		fireEvent.click(screen.getByText("Delete Key"));

		expect(screen.getByText("Are you sure?")).toBeInTheDocument();
		expect(screen.getByText("Yes, delete")).toBeInTheDocument();
		expect(screen.getByText("Cancel")).toBeInTheDocument();
		expect(screen.queryByText("Delete Key")).not.toBeInTheDocument();
	});

	it("calls onConfirm when confirm button is clicked", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} />);

		fireEvent.click(screen.getByText("Delete Key"));
		fireEvent.click(screen.getByText("Yes, delete"));

		expect(onConfirm).toHaveBeenCalledTimes(1);
	});

	it("returns to initial state when cancel is clicked", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} />);

		// Enter confirm state
		fireEvent.click(screen.getByText("Delete Key"));
		expect(screen.getByText("Are you sure?")).toBeInTheDocument();

		// Cancel
		fireEvent.click(screen.getByText("Cancel"));

		expect(screen.queryByText("Are you sure?")).not.toBeInTheDocument();
		expect(screen.getByText("Delete Key")).toBeInTheDocument();
	});

	it("shows loading state when loading prop is true", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} loading={true} />);

		fireEvent.click(screen.getByText("Delete Key"));

		expect(screen.getByText("Deleting…")).toBeInTheDocument();
		const confirmButton = screen.getByRole("button", { name: "Deleting…" });
		expect(confirmButton).toBeDisabled();
	});

	it("uses custom label when provided", () => {
		const onConfirm = vi.fn();
		render(<ConfirmDeleteButton onConfirm={onConfirm} label="Remove Item" />);

		expect(screen.getByText("Remove Item")).toBeInTheDocument();
	});

	it("uses custom confirm label when provided", () => {
		const onConfirm = vi.fn();
		render(
			<ConfirmDeleteButton onConfirm={onConfirm} confirmLabel="Yes, remove" />,
		);

		fireEvent.click(screen.getByText("Delete Key"));
		expect(screen.getByText("Yes, remove")).toBeInTheDocument();
	});

	it("applies custom className to container", () => {
		const onConfirm = vi.fn();
		render(
			<ConfirmDeleteButton onConfirm={onConfirm} className="custom-class" />,
		);

		expect(screen.getByText("Delete Key")).toHaveClass("custom-class");
	});
});
