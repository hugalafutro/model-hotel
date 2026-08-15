import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "i18next";
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
// can prove the preview deleted nothing. The panel probes with a dry run on
// mount to decide whether to show itself at all, so every recorded sequence
// starts with that probe.
function prune(previewCount: number, realDeleted = previewCount, failed = 0) {
	const calls: boolean[] = [];
	server.use(
		http.post(PRUNE, ({ request }) => {
			const dry = new URL(request.url).searchParams.has("dryRun");
			calls.push(dry);
			return HttpResponse.json({
				deleted: dry ? previewCount : realDeleted,
				failed: dry ? 0 : failed,
				results: [],
			});
		}),
	);
	return calls;
}

// pruneButton waits for the mount-time probe to resolve and the panel to show.
const pruneButton = () => screen.findByTestId("prune-frontdesk-backups");

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

// expectHidden waits for the mount-time probe to be answered, then gives the
// panel a moment to appear if it were going to, and asserts it did not.
async function expectHidden(served: () => number) {
	await waitFor(() => expect(served()).toBeGreaterThan(0));
	await expect(
		screen.findByTestId("prune-frontdesk-backups", {}, { timeout: 100 }),
	).rejects.toThrow();
}

// The section exists to clean up dumps older Front Desk builds left behind, so a
// fleet that holds none (every fleet set up after that build) never sees it.
it("stays hidden while probing and when no Front Desk backups exist", async () => {
	let release: (() => void) | undefined;
	const gate = new Promise<void>((r) => {
		release = r;
	});
	let served = 0;
	server.use(
		http.post(PRUNE, async () => {
			await gate;
			served++;
			return HttpResponse.json({ deleted: 0, failed: 0, results: [] });
		}),
	);
	renderPanel();

	// Nothing shows while the probe is in flight: no flash of a section that
	// then vanishes.
	expect(screen.queryByTestId("prune-frontdesk-backups")).toBeNull();
	release?.();
	await expectHidden(() => served);
});

it("stays hidden when the probe itself fails", async () => {
	let served = 0;
	server.use(
		http.post(PRUNE, () => {
			served++;
			return new HttpResponse(null, { status: 500 });
		}),
	);
	renderPanel();

	await expectHidden(() => served);
});

// A member Front Desk could not read is not evidence of leftover dumps, so a
// member being down does not summon the section on its own.
it("stays hidden when the only finding is an unreadable member", async () => {
	let served = 0;
	server.use(
		http.post(PRUNE, () => {
			served++;
			return HttpResponse.json({
				deleted: 0,
				failed: 0,
				results: [
					{
						member_id: "b",
						name: "b",
						deleted: 0,
						failed: 0,
						error: "could not read this member's backup listing",
					},
				],
			});
		}),
	);
	renderPanel();

	await expectHidden(() => served);
});

it("previews before deleting and names the count in the confirmation", async () => {
	const calls = prune(7);
	renderPanel();

	await userEvent.click(await pruneButton());

	await waitFor(() =>
		expect(screen.getByTestId("prune-preview-count")).toHaveTextContent("7"),
	);
	// Every call so far was a dry run (the mount probe, then the preview):
	// nothing has been deleted.
	expect(calls).toEqual([true, true]);
});

it("deletes only after the confirmation is accepted", async () => {
	const calls = prune(2);
	renderPanel();

	await userEvent.click(await pruneButton());
	await userEvent.click(confirmButton(await screen.findByRole("dialog")));

	await waitFor(() => expect(calls).toEqual([true, true, false]));
	await waitFor(() =>
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
	);
	// The outcome is reported with the number actually deleted.
	await waitFor(() =>
		expect(screen.getByRole("status")).toHaveTextContent("2"),
	);
	// Everything it existed to clean up is gone, so the section retires itself.
	expect(screen.queryByTestId("prune-frontdesk-backups")).toBeNull();
});

// A refused delete means dumps remain, so the section stays for another go.
it("stays visible when some backups could not be deleted", async () => {
	prune(3, 2, 1);
	renderPanel();

	await userEvent.click(await pruneButton());
	await userEvent.click(confirmButton(await screen.findByRole("dialog")));

	await waitFor(() =>
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
	);
	expect(screen.getByTestId("prune-frontdesk-backups")).toBeInTheDocument();
});

it("cancelling the confirmation deletes nothing", async () => {
	const calls = prune(3);
	renderPanel();

	await userEvent.click(await pruneButton());
	await userEvent.click(cancelButton(await screen.findByRole("dialog")));

	await waitFor(() =>
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
	);
	expect(calls).toEqual([true, true]);
});

// The dumps can disappear between the mount probe and the click (another
// operator, or the member's own cleanup), so the confirmation must still cope
// with an empty preview.
it("blocks the confirm button when there is nothing left to delete", async () => {
	let probes = 0;
	server.use(
		http.post(PRUNE, () => {
			probes++;
			return HttpResponse.json({
				deleted: probes === 1 ? 4 : 0,
				failed: 0,
				results: [],
			});
		}),
	);
	renderPanel();

	await userEvent.click(await pruneButton());
	expect(confirmButton(await screen.findByRole("dialog"))).toBeDisabled();
});

it("reports a failed preview instead of opening the confirmation", async () => {
	let probes = 0;
	server.use(
		http.post(PRUNE, () => {
			probes++;
			return probes === 1
				? HttpResponse.json({ deleted: 4, failed: 0, results: [] })
				: new HttpResponse(null, { status: 500 });
		}),
	);
	renderPanel();

	const btn = await pruneButton();
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

	await userEvent.click(await pruneButton());
	const dialog = await screen.findByRole("dialog");

	await waitFor(() => expect(dialog.querySelectorAll("p")).toHaveLength(2));
});

// The server runs the prune detached from the request, so a lost response does
// not mean a lost run. The toast must point the operator at where the outcome
// actually lands rather than claiming a failure nobody observed, and the dialog
// stays open because nothing here proves the run is over.
it("points a lost prune response at the event log", async () => {
	server.use(
		http.post(PRUNE, ({ request }) => {
			const dry = new URL(request.url).searchParams.has("dryRun");
			if (dry) {
				return HttpResponse.json({ deleted: 3, failed: 0, results: [] });
			}
			return HttpResponse.error();
		}),
	);
	renderPanel();

	await userEvent.click(await pruneButton());
	const dialog = await screen.findByRole("dialog");
	await userEvent.click(confirmButton(dialog));

	// Compared against the resolved translation rather than a literal string, so
	// the test pins which message fires without depending on the active locale.
	const toasts = await screen.findByRole("status");
	await waitFor(() =>
		expect(toasts).toHaveTextContent(
			i18n.t("settings.maintenance.pruneUnknown"),
		),
	);
	expect(screen.queryByRole("dialog")).toBeInTheDocument();
});
