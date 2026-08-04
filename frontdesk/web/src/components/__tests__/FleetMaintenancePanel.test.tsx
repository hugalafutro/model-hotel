import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { FleetMaintenancePanel } from "../FleetMaintenancePanel";

const PRUNE = "/api/fleet/backups/prune-frontdesk";

function renderPanel() {
	render(
		<ToastProvider>
			<FleetMaintenancePanel />
		</ToastProvider>,
	);
}

// prune registers the endpoint and records each call's dryRun flag, so a test
// can prove the preview deleted nothing.
function prune(previewCount: number, realDeleted = previewCount) {
	const calls: boolean[] = [];
	server.use(
		http.post(PRUNE, ({ request }) => {
			const dry = new URL(request.url).searchParams.has("dryRun");
			calls.push(dry);
			return HttpResponse.json({
				deleted: dry ? previewCount : realDeleted,
				failed: 0,
				results: [],
			});
		}),
	);
	return calls;
}

// The confirm dialog renders exactly two actions, cancel first and the
// destructive confirm last. Selecting by position keeps these tests independent
// of the active locale.
const cancelButton = (dialog: HTMLElement) =>
	within(dialog).getAllByRole("button")[0];
const confirmButton = (dialog: HTMLElement) => {
	const buttons = within(dialog).getAllByRole("button");
	return buttons[buttons.length - 1];
};

beforeEach(() => {
	server.resetHandlers();
});

it("previews before deleting and names the count in the confirmation", async () => {
	const calls = prune(7);
	renderPanel();

	await userEvent.click(screen.getByTestId("prune-frontdesk-backups"));

	await waitFor(() =>
		expect(screen.getByTestId("prune-preview-count")).toHaveTextContent("7"),
	);
	// The only call so far was the dry run: nothing has been deleted.
	expect(calls).toEqual([true]);
});

it("deletes only after the confirmation is accepted", async () => {
	const calls = prune(2);
	renderPanel();

	await userEvent.click(screen.getByTestId("prune-frontdesk-backups"));
	await userEvent.click(confirmButton(await screen.findByRole("dialog")));

	await waitFor(() => expect(calls).toEqual([true, false]));
	await waitFor(() =>
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
	);
	// The outcome is reported with the number actually deleted.
	await waitFor(() =>
		expect(screen.getByRole("status")).toHaveTextContent("2"),
	);
});

it("cancelling the confirmation deletes nothing", async () => {
	const calls = prune(3);
	renderPanel();

	await userEvent.click(screen.getByTestId("prune-frontdesk-backups"));
	await userEvent.click(cancelButton(await screen.findByRole("dialog")));

	await waitFor(() =>
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
	);
	expect(calls).toEqual([true]);
});

it("blocks the confirm button when there is nothing to delete", async () => {
	prune(0);
	renderPanel();

	await userEvent.click(screen.getByTestId("prune-frontdesk-backups"));
	expect(confirmButton(await screen.findByRole("dialog"))).toBeDisabled();
});

it("reports a failed preview instead of opening the confirmation", async () => {
	server.use(http.post(PRUNE, () => new HttpResponse(null, { status: 500 })));
	renderPanel();

	const btn = screen.getByTestId("prune-frontdesk-backups");
	await userEvent.click(btn);

	// No dialog, and the button is usable again so the operator can retry.
	await waitFor(() => expect(btn).not.toBeDisabled());
	expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

// A member whose listing could not be read means the preview count is a floor,
// so the confirmation says so instead of hiding it.
it("notes members whose backups could not be counted", async () => {
	server.use(
		http.post(PRUNE, () =>
			HttpResponse.json({
				deleted: 1,
				failed: 0,
				results: [
					{ member_id: "a", name: "a", deleted: 1, failed: 0 },
					{
						member_id: "b",
						name: "b",
						deleted: 0,
						failed: 0,
						error: "could not read this member's backup listing",
					},
				],
			}),
		),
	);
	renderPanel();

	await userEvent.click(screen.getByTestId("prune-frontdesk-backups"));
	const dialog = await screen.findByRole("dialog");

	await waitFor(() => expect(dialog.querySelectorAll("p")).toHaveLength(2));
});
