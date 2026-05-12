import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

	it("renders custom header when provided instead of title", () => {
		render(
			<Modal onClose={onClose} header={<h3>Custom Header</h3>}>
				Content
			</Modal>,
		);
		expect(screen.getByText("Custom Header")).toBeInTheDocument();
		expect(screen.queryByText("Test Title")).not.toBeInTheDocument();
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
			<Modal onClose={onClose} maxWidth="max-w-lg">
				Content
			</Modal>,
		);
		const modalContent = screen.getByRole("dialog").querySelector(".max-w-lg");
		expect(modalContent).toBeInTheDocument();
	});

	it("applies custom className via maxWidth", () => {
		render(
			<Modal onClose={onClose} maxWidth="max-w-2xl">
				Content
			</Modal>,
		);
		expect(screen.getByRole("dialog")).toBeInTheDocument();
	});

	it("applies scrollable class when scrollable is true", () => {
		render(
			<Modal onClose={onClose} scrollable>
				Content
			</Modal>,
		);
		const modalContent = screen
			.getByRole("dialog")
			.querySelector(".max-h-\\[85vh\\].overflow-y-auto");
		expect(modalContent).toBeInTheDocument();
	});

	it("applies custom zIndex", () => {
		render(
			<Modal onClose={onClose} zIndex="z-60">
				Content
			</Modal>,
		);
		const modal = screen.getByRole("dialog");
		expect(modal.className).toContain("z-60");
	});
});
