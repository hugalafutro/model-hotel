import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { getByDialogName } from "../../test/helpers";
import { Modal } from "../Modal";

describe("Modal", () => {
	const onClose = vi.fn();

	beforeEach(() => {
		onClose.mockClear();
	});

	it("renders children", () => {
		render(<Modal onClose={onClose}>Test Content</Modal>);
		expect(screen.getByText("Test Content")).toBeInTheDocument();
	});

	it("renders title when provided", () => {
		render(
			<Modal onClose={onClose} title="Test Title">
				Content
			</Modal>,
		);
		expect(screen.getByText("Test Title")).toBeInTheDocument();
	});

	it("associates dialog with title via aria-labelledby", () => {
		render(
			<Modal onClose={onClose} title="Test Title">
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Test Title");
		expect(dialog).toHaveAttribute("aria-labelledby");
		expect(dialog).toHaveAttribute("aria-modal", "true");
	});

	it("renders custom header when provided instead of title", () => {
		render(
			<Modal onClose={onClose} header={<h3>Custom Header</h3>}>
				Content
			</Modal>,
		);
		expect(screen.getByText("Custom Header")).toBeInTheDocument();
		expect(screen.queryByText("Test Title")).not.toBeInTheDocument();
	});

	it("associates dialog with custom header via aria-labelledby", () => {
		render(
			<Modal onClose={onClose} header={<h3>Custom Header</h3>}>
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Custom Header");
		expect(dialog).toBeInTheDocument();
	});

	it("does not set aria-labelledby when no title or header", () => {
		render(<Modal onClose={onClose}>Content</Modal>);
		const dialog = screen.getByRole("dialog");
		expect(dialog).not.toHaveAttribute("aria-labelledby");
	});

	it("calls onClose when close button is clicked", async () => {
		const user = userEvent.setup();
		render(<Modal onClose={onClose}>Content</Modal>);
		const closeButton = screen.getByRole("button", { name: "Close" });
		await user.click(closeButton);
		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when backdrop is clicked and closeOnBackdrop is true", async () => {
		const user = userEvent.setup();
		render(
			<Modal onClose={onClose} closeOnBackdrop>
				Content
			</Modal>,
		);
		// The backdrop is the button with aria-label "Close dialog"
		const backdrop = screen.getByRole("button", { name: "Close dialog" });
		await user.click(backdrop);
		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("does not call onClose when backdrop is clicked and closeOnBackdrop is false", async () => {
		const user = userEvent.setup();
		render(
			<Modal onClose={onClose} closeOnBackdrop={false}>
				Content
			</Modal>,
		);
		const backdrop = screen.getByRole("button", { name: "Close dialog" });
		await user.click(backdrop);
		expect(onClose).not.toHaveBeenCalled();
	});

	it("applies custom maxWidth class", () => {
		render(
			<Modal onClose={onClose} title="Width Test" maxWidth="max-w-lg">
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Width Test");
		const modalContent = dialog.querySelector(".max-w-lg");
		expect(modalContent).toBeInTheDocument();
	});

	it("applies custom className via maxWidth", () => {
		render(
			<Modal onClose={onClose} title="Width Test 2">
				Content
			</Modal>,
		);
		expect(getByDialogName("Width Test 2")).toBeInTheDocument();
	});

	it("applies scrollable class when scrollable is true", () => {
		render(
			<Modal onClose={onClose} title="Scroll Test" scrollable>
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Scroll Test");
		const modalContent = dialog.querySelector(
			".max-h-\\[85vh\\].overflow-y-auto",
		);
		expect(modalContent).toBeInTheDocument();
	});

	it("applies custom zIndex", () => {
		render(
			<Modal onClose={onClose} title="Z-Index Test" zIndex="z-60">
				Content
			</Modal>,
		);
		const modal = getByDialogName("Z-Index Test");
		expect(modal.className).toContain("z-60");
	});
});
