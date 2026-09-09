import { fireEvent, screen } from "@testing-library/react";
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
		<Modal title="Row" nav={nav} onClose={() => setSelected(null)}>
			<p>row {selected.id}</p>
		</Modal>
	);
}

describe("ModalNav", () => {
	it("shows the open row's position in the list", () => {
		renderWithProviders(<Harness />);
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
	});

	it("steps to the next and previous row", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness />);

		await user.click(screen.getByRole("button", { name: "Next" }));
		expect(screen.getByText("row c")).toBeInTheDocument();
		expect(screen.getByText("3 / 3")).toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: "Prev" }));
		await user.click(screen.getByRole("button", { name: "Prev" }));
		expect(screen.getByText("row a")).toBeInTheDocument();
	});

	it("disables each arrow at its end of the list", async () => {
		const user = userEvent.setup();
		renderWithProviders(<Harness startId="a" />);

		expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();

		await user.click(screen.getByRole("button", { name: "Next" }));
		await user.click(screen.getByRole("button", { name: "Next" }));
		expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
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

	it("hides the stepper when the open row left the list", () => {
		renderWithProviders(<Harness startId="gone" />);

		expect(
			screen.queryByRole("button", { name: "Next" }),
		).not.toBeInTheDocument();
		expect(screen.getByText("row gone")).toBeInTheDocument();
	});
});
