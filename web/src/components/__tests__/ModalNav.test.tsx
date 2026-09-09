import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { useModalNav } from "../../hooks/useModalNav";
import { renderWithProviders } from "../../test/utils";
import { Modal } from "../Modal";

interface Row {
	id: string;
}

const ROWS: Row[] = [{ id: "a" }, { id: "b" }, { id: "c" }];

/** A list-plus-detail-modal page, boiled down to what the stepper touches. */
function Harness({
	rows = ROWS,
	startId = "b",
}: {
	rows?: Row[];
	startId?: string;
}) {
	const [selected, setSelected] = useState<Row | null>({ id: startId });
	const nav = useModalNav(rows, selected, setSelected, (row) => row.id);
	if (!selected) return null;
	return (
		<Modal title="Row" nav={nav} scrollable onClose={() => setSelected(null)}>
			<p>row {selected.id}</p>
		</Modal>
	);
}

const prevButton = () => screen.getByRole("button", { name: "Previous row" });
const nextButton = () => screen.getByRole("button", { name: "Next row" });

describe("ModalNav", () => {
	it("shows the open row's position in the list", () => {
		renderWithProviders(<Harness />);
		expect(screen.getByText("2/3")).toBeInTheDocument();
	});

	it("steps to the next and previous row", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness />);

		await user.click(nextButton());
		expect(screen.getByText("row c")).toBeInTheDocument();
		expect(screen.getByText("3/3")).toBeInTheDocument();

		await user.click(prevButton());
		await user.click(prevButton());
		expect(screen.getByText("row a")).toBeInTheDocument();
	});

	it("marks each arrow inert at its end of the list, without unfocusing it", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness startId="a" />);

		expect(prevButton()).toHaveAttribute("aria-disabled", "true");
		expect(nextButton()).toHaveAttribute("aria-disabled", "false");

		await user.click(nextButton());
		await user.click(nextButton());
		expect(nextButton()).toHaveAttribute("aria-disabled", "true");
		// The button that ran out of rows keeps the focus that clicked it, so a
		// keyboard user is not dropped back to the page behind the dialog.
		expect(nextButton()).toHaveFocus();

		await user.click(nextButton());
		expect(screen.getByText("row c")).toBeInTheDocument();
	});

	it("offers no stepper for a single-row list", () => {
		renderWithProviders(<Harness rows={[{ id: "a" }]} startId="a" />);

		expect(
			screen.queryByRole("button", { name: "Next row" }),
		).not.toBeInTheDocument();
		expect(screen.getByText("row a")).toBeInTheDocument();
	});

	const announced = () =>
		document
			.querySelector("[role='dialog'] [aria-live='polite']")
			?.textContent?.trim();

	it("says where the open row sits once the reader steps there", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness />);
		// Nothing to say on open: the dialog title already announced itself.
		expect(announced()).toBe("");

		await user.click(nextButton());

		expect(announced()).toBe("Row 3 of 3");
	});

	it("stays quiet when a live update moves the row along", () => {
		const { rerender } = renderWithProviders(<Harness />);

		rerender(<Harness rows={[{ id: "new" }, ...ROWS]} />);

		// The position changed, but the reader did not ask for it and may be
		// midway through the row they opened.
		expect(announced()).toBe("");
	});

	it("steps with the left and right arrow keys", () => {
		renderWithProviders(<Harness />);

		fireEvent.keyDown(document, { key: "ArrowRight" });
		expect(screen.getByText("row c")).toBeInTheDocument();

		fireEvent.keyDown(document, { key: "ArrowLeft" });
		fireEvent.keyDown(document, { key: "ArrowLeft" });
		expect(screen.getByText("row a")).toBeInTheDocument();

		// Already at the first row: the key is a no-op, not a wrap-around.
		fireEvent.keyDown(document, { key: "ArrowLeft" });
		expect(screen.getByText("row a")).toBeInTheDocument();
	});

	it("leaves arrow keys to a field being typed in", () => {
		renderWithProviders(
			<>
				<input aria-label="filter" />
				<Harness />
			</>,
		);

		fireEvent.keyDown(screen.getByLabelText("filter"), { key: "ArrowRight" });
		expect(screen.getByText("row b")).toBeInTheDocument();
	});

	it("leaves arrow keys to a selection widget inside the dialog", () => {
		renderWithProviders(<Harness />);

		// A listbox in the dialog body moves its own selection with the arrows.
		const listbox = document.createElement("div");
		listbox.setAttribute("role", "listbox");
		screen.getByRole("dialog").appendChild(listbox);

		fireEvent.keyDown(listbox, { key: "ArrowRight" });
		expect(screen.getByText("row b")).toBeInTheDocument();
	});

	// Alt+Arrow is browser history; the rest carry their own shortcut meanings.
	it.each(["altKey", "ctrlKey", "metaKey", "shiftKey"])(
		"leaves %s + Arrow to the browser",
		(modifier) => {
			renderWithProviders(<Harness />);

			fireEvent.keyDown(document, { key: "ArrowRight", [modifier]: true });
			expect(screen.getByText("row b")).toBeInTheDocument();
		},
	);

	it("ignores a key another handler already took", () => {
		renderWithProviders(<Harness />);
		// Capture phase, so it runs before the dialog's own listener.
		const consume = (e: Event) => e.preventDefault();
		document.addEventListener("keydown", consume, true);

		fireEvent.keyDown(document, { key: "ArrowRight" });

		document.removeEventListener("keydown", consume, true);
		expect(screen.getByText("row b")).toBeInTheDocument();
	});

	it("leaves the arrows alone in a modal with no list behind it", () => {
		renderWithProviders(
			<Modal title="Plain" onClose={() => {}}>
				<p>no list</p>
			</Modal>,
		);

		// The bare dialog must not swallow the keys either: nothing to step,
		// so the press falls through to whatever else is listening.
		// fireEvent returns false only when a listener called preventDefault.
		expect(fireEvent.keyDown(document, { key: "ArrowRight" })).toBe(true);
	});

	it("still closes on Escape while the stepper is present", async () => {
		renderWithProviders(<Harness />);

		fireEvent.keyDown(document, { key: "Escape" });

		// Escape closes and nothing else: it must not also step the list on
		// its way out.
		expect(screen.getByText("row b")).toBeInTheDocument();
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
	});

	it.each(["a", "Enter", "ArrowDown", "Home", "Tab"])(
		"leaves the row alone for %s",
		(key) => {
			renderWithProviders(<Harness />);

			expect(fireEvent.keyDown(document, { key })).toBe(true);
			expect(screen.getByText("row b")).toBeInTheDocument();
		},
	);

	it("takes the keypress it acts on, and only that one", () => {
		renderWithProviders(<Harness />);

		// Consumed, so the same press cannot also scroll the dialog body.
		expect(fireEvent.keyDown(document, { key: "ArrowRight" })).toBe(false);
		expect(screen.getByText("row c")).toBeInTheDocument();

		// At the end of the list there is no step to take, so the press is
		// left to whatever else would have used it.
		expect(fireEvent.keyDown(document, { key: "ArrowRight" })).toBe(true);
	});

	it("steps only the topmost dialog", () => {
		renderWithProviders(
			<>
				<Harness />
				<Modal title="On top" onClose={() => {}}>
					<p>confirm</p>
				</Modal>
			</>,
		);

		fireEvent.keyDown(document, { key: "ArrowRight" });
		expect(screen.getByText("row b")).toBeInTheDocument();
	});

	it("follows the open row when a live update shifts the list", () => {
		const { rerender } = renderWithProviders(<Harness />);
		expect(screen.getByText("2/3")).toBeInTheDocument();

		// A newer row arrives at the top: same row still open, one place later.
		rerender(<Harness rows={[{ id: "new" }, ...ROWS]} />);
		expect(screen.getByText("row b")).toBeInTheDocument();
		expect(screen.getByText("3/4")).toBeInTheDocument();
	});

	it("starts each stepped-to row at the top of the dialog", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness />);
		const body = document.querySelector<HTMLElement>("[data-modal-scroll]");
		if (!body) throw new Error("scrollable body not rendered");
		body.scrollTop = 400;
		expect(body.scrollTop).toBe(400);

		await user.click(nextButton());

		expect(body.scrollTop).toBe(0);
	});

	it("leaves the scroll position alone when a live update moves the row along", () => {
		const { rerender } = renderWithProviders(<Harness />);
		const body = document.querySelector<HTMLElement>("[data-modal-scroll]");
		if (!body) throw new Error("scrollable body not rendered");
		body.scrollTop = 400;

		rerender(<Harness rows={[{ id: "new" }, ...ROWS]} />);

		// Same row, still open, still where the reader had scrolled to.
		expect(body.scrollTop).toBe(400);
	});

	it("hides the stepper when the open row left the list", () => {
		renderWithProviders(<Harness startId="gone" />);

		expect(
			screen.queryByRole("button", { name: "Next row" }),
		).not.toBeInTheDocument();
		expect(screen.getByText("row gone")).toBeInTheDocument();
	});
});
