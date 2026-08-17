import {
	act,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
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
		await waitFor(() => {
			expect(onClose).toHaveBeenCalledTimes(1);
		});
	});

	it("starts closing when Escape is pressed", async () => {
		render(<Modal onClose={onClose}>Content</Modal>);
		const dialog = screen.getByRole("dialog");
		act(() => {
			fireEvent.keyDown(dialog, { key: "Escape" });
		});
		// Escape begins the fade; onClose lands once the fallback timer elapses.
		await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
	});

	it("closes on Escape pressed from outside the dialog subtree", async () => {
		// The regression this guards: a control that unmounts while focused hands
		// focus back to <body>, so the key event no longer originates inside the
		// dialog. With the handler scoped to the dialog node the modal simply
		// stopped closing on Escape from that point on.
		render(<Modal onClose={onClose}>Content</Modal>);
		act(() => {
			fireEvent.keyDown(document.body, { key: "Escape" });
		});
		await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
	});

	it("gives Escape to the topmost dialog only", async () => {
		// A nested confirm must close before the dialog that opened it. With a
		// document-level listener every open modal hears the key, so the stack
		// order is what decides, and only the last-opened one may act.
		const onCloseOuter = vi.fn();
		const onCloseInner = vi.fn();
		render(
			<>
				<Modal onClose={onCloseOuter}>Outer</Modal>
				<Modal onClose={onCloseInner}>Inner</Modal>
			</>,
		);
		act(() => {
			fireEvent.keyDown(document.body, { key: "Escape" });
		});
		await waitFor(() => expect(onCloseInner).toHaveBeenCalledTimes(1));
		expect(onCloseOuter).not.toHaveBeenCalled();
	});

	it("hands Escape back to the opener once the nested dialog unmounts", async () => {
		// The stack has to shrink on unmount, or the outer dialog stays deaf to
		// Escape for the rest of its life.
		const onCloseOuter = vi.fn();
		function Nested({ showInner }: { showInner: boolean }) {
			return (
				<>
					<Modal onClose={onCloseOuter}>Outer</Modal>
					{showInner && <Modal onClose={vi.fn()}>Inner</Modal>}
				</>
			);
		}
		const { rerender } = render(<Nested showInner={true} />);
		rerender(<Nested showInner={false} />);

		act(() => {
			fireEvent.keyDown(document.body, { key: "Escape" });
		});
		await waitFor(() => expect(onCloseOuter).toHaveBeenCalledTimes(1));
	});

	// A dialog can be mid-write, where walking out would strand the work: it says
	// so with dismissible={false}, and both quiet exits are withdrawn for as long
	// as that lasts.
	it("ignores Escape and disables the close button while not dismissible", () => {
		vi.useFakeTimers();
		try {
			render(
				<Modal onClose={onClose} dismissible={false}>
					Content
				</Modal>,
			);
			act(() => {
				fireEvent.keyDown(document.body, { key: "Escape" });
			});
			// Not even the fade started, so the fallback timer has nothing to fire.
			act(() => {
				vi.advanceTimersByTime(1000);
			});
			expect(onClose).not.toHaveBeenCalled();
			expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
		} finally {
			vi.useRealTimers();
		}
	});

	it("closes on Escape again once it becomes dismissible", async () => {
		function Gated({ locked }: { locked: boolean }) {
			return (
				<Modal onClose={onClose} dismissible={!locked}>
					Content
				</Modal>
			);
		}
		const { rerender } = render(<Gated locked={true} />);
		act(() => {
			fireEvent.keyDown(document.body, { key: "Escape" });
		});
		expect(onClose).not.toHaveBeenCalled();

		rerender(<Gated locked={false} />);
		expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
		act(() => {
			fireEvent.keyDown(document.body, { key: "Escape" });
		});
		await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
	});

	it("closes on the opacity transition end and cancels the fallback timer", () => {
		vi.useFakeTimers();
		try {
			render(<Modal onClose={onClose}>Content</Modal>);
			const dialog = screen.getByRole("dialog");

			// Start closing via the close button (sets the internal closing flag and
			// arms the jsdom fallback timer).
			act(() => {
				screen.getByRole("button", { name: "Close" }).click();
			});

			// A non-opacity transition end on the dialog is ignored.
			act(() => {
				fireEvent.transitionEnd(dialog, { propertyName: "transform" });
			});
			expect(onClose).not.toHaveBeenCalled();

			// The opacity transition completing fires onClose exactly once.
			act(() => {
				fireEvent.transitionEnd(dialog, { propertyName: "opacity" });
			});
			expect(onClose).toHaveBeenCalledTimes(1);

			// The fallback timer was cancelled, so it does not fire a second onClose.
			act(() => {
				vi.advanceTimersByTime(1000);
			});
			expect(onClose).toHaveBeenCalledTimes(1);
		} finally {
			vi.useRealTimers();
		}
	});

	it("ignores a transition end that did not originate on the dialog itself", () => {
		render(<Modal onClose={onClose}>Content</Modal>);
		const dialog = screen.getByRole("dialog");
		act(() => {
			screen.getByRole("button", { name: "Close" }).click();
		});
		// A bubbled opacity transition from a child element (target != dialog) must
		// not be treated as the dialog's own fade completing.
		const content = screen.getByText("Content");
		act(() => {
			fireEvent.transitionEnd(content, { propertyName: "opacity" });
		});
		expect(onClose).not.toHaveBeenCalled();
		// The dialog's own transition still closes it.
		act(() => {
			fireEvent.transitionEnd(dialog, { propertyName: "opacity" });
		});
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
		await waitFor(() => {
			expect(onClose).toHaveBeenCalledTimes(1);
		});
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
		// The card caps its height and lays out as a flex column so the header
		// stays pinned; the body lives in a separate scrollable region.
		const card = dialog.querySelector(".max-h-\\[85vh\\].flex.flex-col");
		expect(card).toBeInTheDocument();
		const scrollArea = dialog.querySelector(".overflow-y-auto");
		expect(scrollArea).toBeInTheDocument();
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

	it("applies pr-10 padding to h2 when title prop is provided", () => {
		render(
			<Modal onClose={onClose} title="Padding Test">
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Padding Test");
		const titleElement = dialog.querySelector("h2");
		expect(titleElement).toHaveClass("pr-10");
	});

	it("applies pr-10 padding to div when header prop is provided", () => {
		render(
			<Modal onClose={onClose} header={<h3>Custom Header</h3>}>
				Content
			</Modal>,
		);
		const dialog = getByDialogName("Custom Header");
		const headerDiv = dialog.querySelector(
			`div[id="${dialog.getAttribute("aria-labelledby")}"]`,
		);
		expect(headerDiv).toHaveClass("pr-10");
	});

	it("does not apply pr-10 padding when neither title nor header is provided", () => {
		render(<Modal onClose={onClose}>Content</Modal>);
		const dialog = screen.getByRole("dialog");
		const pr10Element = dialog.querySelector(".pr-10");
		expect(pr10Element).not.toBeInTheDocument();
	});
});
