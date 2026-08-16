import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DestinationList } from "../DestinationList";

const targets = ["ntfys://ntfy.example.com/secret1", "tgram://123:abc/42"];

it("renders one readable row per target with host and secret in clear", () => {
	render(
		<DestinationList
			targets={targets}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
		/>,
	);
	const rows = screen.getAllByTestId("alert-destination-row");
	expect(rows).toHaveLength(2);
	expect(rows[0]).toHaveTextContent("ntfy.example.com");
	expect(rows[0]).toHaveTextContent("secret1");
	expect(rows[1]).toHaveTextContent("123:abc");
	expect(screen.queryByText("********")).toBeNull();
});

// An unknown scheme has no identifying segment to highlight, so the row falls
// back to the whole URL rather than rendering a blank cell.
it("renders an unknown scheme as a plain Apprise URL row", () => {
	render(
		<DestinationList
			targets={["slack://tokA/tokB/tokC"]}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
		/>,
	);
	expect(screen.getByTestId("alert-destination-row")).toHaveTextContent(
		"slack://tokA/tokB/tokC",
	);
});

it("shows the empty state", () => {
	render(
		<DestinationList
			targets={[]}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
		/>,
	);
	expect(screen.getByTestId("alert-destinations-empty")).toBeInTheDocument();
});

it("tests a row and removes a row behind a confirm", async () => {
	const onTest = vi.fn();
	const onRemove = vi.fn();
	render(
		<DestinationList
			targets={targets}
			onRemove={onRemove}
			onTest={onTest}
			busy={false}
		/>,
	);
	await userEvent.click(screen.getAllByTestId("alert-destination-test")[1]);
	expect(onTest).toHaveBeenCalledWith("tgram://123:abc/42");

	await userEvent.click(screen.getAllByTestId("alert-destination-remove")[0]);
	await userEvent.click(screen.getByTestId("alert-destination-remove-confirm"));
	expect(onRemove).toHaveBeenCalledWith("ntfys://ntfy.example.com/secret1");
});

it("keeps the destination when the confirm is cancelled", async () => {
	const onRemove = vi.fn();
	render(
		<DestinationList
			targets={targets}
			onRemove={onRemove}
			onTest={() => {}}
			busy={false}
		/>,
	);
	await userEvent.click(screen.getAllByTestId("alert-destination-remove")[0]);
	await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
	expect(onRemove).not.toHaveBeenCalled();
	expect(screen.getAllByTestId("alert-destination-row")).toHaveLength(2);
});

it("blocks and explains the row actions when a reason is given", () => {
	render(
		<DestinationList
			targets={targets}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
			disabledReason="unsaved change"
		/>,
	);
	expect(screen.getByTestId("alert-destinations-dirty")).toHaveTextContent(
		"unsaved change",
	);
	const remove = screen.getAllByTestId("alert-destination-remove")[0];
	expect(remove).toBeDisabled();
	expect(remove).toHaveAttribute("title", "unsaved change");
	expect(screen.getAllByTestId("alert-destination-test")[0]).toBeDisabled();
	// Copying is local, so it stays available.
	expect(screen.getAllByTestId("alert-destination-copy")[0]).toBeEnabled();
});

// Three identical button labels per row, so the accessible name says which
// destination each one acts on.
it("names every row action after its destination", () => {
	render(
		<DestinationList
			targets={targets}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
		/>,
	);
	expect(
		screen.getByRole("button", { name: "Remove: ntfy ntfy.example.com" }),
	).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: "Test: Telegram api.telegram.org" }),
	).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: "Copy: Telegram api.telegram.org" }),
	).toBeInTheDocument();
});

it("copies a target URL to the clipboard", async () => {
	const writeText = vi.fn().mockResolvedValue(undefined);
	Object.defineProperty(navigator, "clipboard", {
		value: { writeText },
		configurable: true,
	});
	render(
		<DestinationList
			targets={targets}
			onRemove={() => {}}
			onTest={() => {}}
			busy={false}
		/>,
	);
	await userEvent.click(screen.getAllByTestId("alert-destination-copy")[0]);
	expect(writeText).toHaveBeenCalledWith("ntfys://ntfy.example.com/secret1");
});

it("disables the row actions while the card is busy", () => {
	render(
		<DestinationList
			targets={targets}
			onRemove={() => {}}
			onTest={() => {}}
			busy={true}
		/>,
	);
	expect(screen.getAllByTestId("alert-destination-test")[0]).toBeDisabled();
	expect(screen.getAllByTestId("alert-destination-remove")[0]).toBeDisabled();
});
