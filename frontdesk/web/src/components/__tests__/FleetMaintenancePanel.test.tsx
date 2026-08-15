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

const COUNT = "/api/fleet/backups/frontdesk-count";

// count answers the mount-time read the panel uses to decide whether to show
// itself. The server derives it from listings it already read; nothing here
// touches a member.
function count(n: number) {
	server.use(http.get(COUNT, () => HttpResponse.json({ count: n })));
}

// prune registers the prune endpoint and records each call's dryRun flag, so a
// test can prove the preview deleted nothing. It also answers the mount count
// with previewCount, so the panel shows exactly when there is something to do.
function prune(previewCount: number, realDeleted = previewCount, failed = 0) {
	const calls: boolean[] = [];
	count(previewCount);
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

// pruneButton waits for the mount-time count to resolve and the panel to show.
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

// expectHidden waits for the mount-time count to be answered, then gives the
// panel a moment to appear if it were going to, and asserts it did not.
async function expectHidden(served: () => number) {
	await waitFor(() => expect(served()).toBeGreaterThan(0));
	await expect(
		screen.findByTestId("prune-frontdesk-backups", {}, { timeout: 100 }),
	).rejects.toThrow();
}

// The section exists to clean up dumps older Front Desk builds left behind, so a
// fleet that holds none (every fleet set up after that build) never sees it.
it("stays hidden while the count is in flight and when it is zero", async () => {
	let release: (() => void) | undefined;
	const gate = new Promise<void>((r) => {
		release = r;
	});
	let served = 0;
	server.use(
		http.get(COUNT, async () => {
			await gate;
			served++;
			return HttpResponse.json({ count: 0 });
		}),
	);
	renderPanel();

	// Nothing shows while the count is in flight: no flash of a section that
	// then vanishes.
	expect(screen.queryByTestId("prune-frontdesk-backups")).toBeNull();
	release?.();
	await expectHidden(() => served);
});

it("stays hidden when the count cannot be read", async () => {
	let served = 0;
	server.use(
		http.get(COUNT, () => {
			served++;
			return new HttpResponse(null, { status: 500 });
		}),
	);
	renderPanel();

	await expectHidden(() => served);
});

// The mount read is the count endpoint, never a prune call: showing the section
// must not cost a fleet-wide listing read, and must never be one argument away
// from a real prune.
it("never calls the prune endpoint on mount", async () => {
	let pruneCalls = 0;
	server.use(
		http.post(PRUNE, () => {
			pruneCalls++;
			return HttpResponse.json({ deleted: 0, failed: 0, results: [] });
		}),
	);
	count(2);
	renderPanel();

	await pruneButton();
	expect(pruneCalls).toBe(0);
});

it("previews before deleting and names the count in the confirmation", async () => {
	const calls = prune(7);
	renderPanel();

	await userEvent.click(await pruneButton());

	await waitFor(() =>
		expect(screen.getByTestId("prune-preview-count")).toHaveTextContent("7"),
	);
	// The only prune call so far was the dry run: nothing has been deleted.
	expect(calls).toEqual([true]);
});

it("deletes only after the confirmation is accepted", async () => {
	const calls = prune(2);
	renderPanel();

	await userEvent.click(await pruneButton());
	await userEvent.click(confirmButton(await screen.findByRole("dialog")));

	await waitFor(() => expect(calls).toEqual([true, false]));
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

// A member Front Desk could not read during the run was never counted, so its
// dumps are unknown rather than gone: the section stays even though the run
// itself refused nothing. (Stricter than the mount rule on purpose: showing the
// section needs positive evidence, retiring it needs proof of nothing left.)
it("stays visible when a member could not be read during the run", async () => {
	count(2);
	server.use(
		http.post(PRUNE, () =>
			HttpResponse.json({
				deleted: 2,
				failed: 0,
				results: [
					{ member_id: "a", name: "a", deleted: 2, failed: 0 },
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
	expect(calls).toEqual([true]);
});

// The dumps can disappear between the mount count and the click (another
// operator, or the member's own cleanup), so the confirmation must still cope
// with an empty preview.
it("blocks the confirm button when there is nothing left to delete", async () => {
	count(4);
	server.use(
		http.post(PRUNE, () =>
			HttpResponse.json({ deleted: 0, failed: 0, results: [] }),
		),
	);
	renderPanel();

	await userEvent.click(await pruneButton());
	expect(confirmButton(await screen.findByRole("dialog"))).toBeDisabled();
});

it("reports a failed preview instead of opening the confirmation", async () => {
	count(4);
	server.use(http.post(PRUNE, () => new HttpResponse(null, { status: 500 })));
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
	count(1);
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
	count(3);
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
