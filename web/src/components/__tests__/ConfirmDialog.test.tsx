import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConfirmDialog } from "../ConfirmDialog";

describe("ConfirmDialog", () => {
	const onConfirm = vi.fn();
	const onCancel = vi.fn();

	beforeEach(() => {
		onConfirm.mockClear();
		onCancel.mockClear();
	});

	it("renders title", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("Delete Item")).toBeInTheDocument();
	});

	it("renders message", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				message="Are you sure?"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("Are you sure?")).toBeInTheDocument();
	});

	it("renders default message when not provided", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("Discard changes to:")).toBeInTheDocument();
	});

	it("renders fields list", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1", "field2", "field3"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("field1")).toBeInTheDocument();
		expect(screen.getByText("field2")).toBeInTheDocument();
		expect(screen.getByText("field3")).toBeInTheDocument();
	});

	it("renders confirm and cancel buttons", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("Cancel")).toBeInTheDocument();
		expect(screen.getByText("Discard")).toBeInTheDocument();
	});

	it("calls onCancel when cancel button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		await user.click(screen.getByText("Cancel"));
		expect(onCancel).toHaveBeenCalledTimes(1);
	});

	it("calls onConfirm when confirm button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		await user.click(screen.getByText("Discard"));
		expect(onConfirm).toHaveBeenCalledTimes(1);
	});

	it("calls onCancel when close button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		const closeButton = screen.getByRole("button", { name: "Close" });
		await user.click(closeButton);
		expect(onCancel).toHaveBeenCalledTimes(1);
	});

	it("uses custom confirmLabel when provided", () => {
		render(
			<ConfirmDialog
				title="Delete Item"
				fields={["field1"]}
				confirmLabel="Delete Forever"
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(screen.getByText("Delete Forever")).toBeInTheDocument();
	});

	it("uses custom message when provided", () => {
		render(
			<ConfirmDialog
				title="Warning"
				message="This action cannot be undone"
				fields={["data"]}
				onConfirm={onConfirm}
				onCancel={onCancel}
			/>,
		);
		expect(
			screen.getByText("This action cannot be undone"),
		).toBeInTheDocument();
	});
});
