import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../../test/utils";
import { DestinationList } from "../DestinationList";

const targets = ["ntfys://ntfy.example.com/secret1", "tgram://123:abc/42"];

describe("DestinationList", () => {
	it("renders one readable row per target with host and secret in clear", () => {
		renderWithProviders(
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

	// The card renders under three UI styles, so a row says WHAT each part is
	// and index.css decides how it looks. A raw palette utility here would show
	// up as an unthemed pill or button in two of the three.
	it("styles the row from semantic classes only", () => {
		renderWithProviders(
			<DestinationList
				targets={targets}
				onRemove={() => {}}
				onTest={() => {}}
				busy={false}
			/>,
		);
		const row = screen.getAllByTestId("alert-destination-row")[0];
		expect(within(row).getByText("ntfy")).toHaveClass(
			"ui-badge",
			"ui-badge-neutral",
		);
		for (const id of [
			"alert-destination-copy",
			"alert-destination-test",
			"alert-destination-remove",
		]) {
			expect(within(row).getByTestId(id)).toHaveClass(
				"ui-btn",
				"ui-btn-secondary",
			);
		}
	});

	// An unknown scheme has no identifying segment to highlight, so the row falls
	// back to the whole URL rather than rendering a blank cell.
	it("renders an unknown scheme as a plain Apprise URL row", () => {
		renderWithProviders(
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
		renderWithProviders(
			<DestinationList
				targets={[]}
				onRemove={() => {}}
				onTest={() => {}}
				busy={false}
			/>,
		);
		expect(screen.getByTestId("alert-destinations-empty")).toBeInTheDocument();
	});

	it("prefers the caller's wording for the empty state", () => {
		renderWithProviders(
			<DestinationList
				targets={[]}
				onRemove={() => {}}
				onTest={() => {}}
				busy={false}
				emptyText="nothing added in this run"
			/>,
		);
		expect(screen.getByTestId("alert-destinations-empty")).toHaveTextContent(
			"nothing added in this run",
		);
	});

	it("tests a row and removes a row behind a confirm", async () => {
		const onTest = vi.fn();
		const onRemove = vi.fn();
		const { user } = renderWithProviders(
			<DestinationList
				targets={targets}
				onRemove={onRemove}
				onTest={onTest}
				busy={false}
			/>,
		);
		await user.click(screen.getAllByTestId("alert-destination-test")[1]);
		expect(onTest).toHaveBeenCalledWith("tgram://123:abc/42");

		await user.click(screen.getAllByTestId("alert-destination-remove")[0]);
		await user.click(screen.getByTestId("alert-destination-remove-confirm"));
		await waitFor(() =>
			expect(onRemove).toHaveBeenCalledWith("ntfys://ntfy.example.com/secret1"),
		);
	});

	it("keeps the destination when the confirm is cancelled", async () => {
		const onRemove = vi.fn();
		const { user } = renderWithProviders(
			<DestinationList
				targets={targets}
				onRemove={onRemove}
				onTest={() => {}}
				busy={false}
			/>,
		);
		await user.click(screen.getAllByTestId("alert-destination-remove")[0]);
		await user.click(screen.getByTestId("confirm-dialog-cancel"));
		await waitFor(() =>
			expect(
				screen.queryByTestId("alert-destination-remove-confirm"),
			).toBeNull(),
		);
		expect(onRemove).not.toHaveBeenCalled();
		expect(screen.getAllByTestId("alert-destination-row")).toHaveLength(2);
	});

	it("blocks and explains the row actions when a reason is given", () => {
		renderWithProviders(
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
		renderWithProviders(
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
		const { user } = renderWithProviders(
			<DestinationList
				targets={targets}
				onRemove={() => {}}
				onTest={() => {}}
				busy={false}
			/>,
		);
		// Spied after render: userEvent.setup() installs its own clipboard stub,
		// so the spy has to go on whatever object is in place by then.
		const writeText = vi
			.spyOn(navigator.clipboard, "writeText")
			.mockResolvedValue(undefined);
		await user.click(screen.getAllByTestId("alert-destination-copy")[0]);
		expect(writeText).toHaveBeenCalledWith("ntfys://ntfy.example.com/secret1");
	});

	it("disables the row actions while the card is busy", () => {
		renderWithProviders(
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
});
