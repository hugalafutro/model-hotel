import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FleetMemberStatus, MemberView } from "../../../api/types";
import { ToastProvider } from "../../../context/ToastContext";
import i18n from "../../../i18n";
import { server } from "../../../test/server";
import { FleetSyncWizard } from "../FleetSyncWizard";

// Error and cancel paths of the fleet sync wizard. Every UI string is looked up
// through its i18n key; backend error text is asserted verbatim because the
// wizard toasts an ApiError's own message.

function member(id: string, name: string): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: true,
		created_at: "",
		updated_at: "",
		status: {
			health: { known: true, healthy: true, latency_ms: 1, checked_at: "" },
		},
	};
}

const members = [member("1", "hotel-1"), member("2", "hotel-2")];

function row(id: string, over: Partial<FleetMemberStatus> = {}) {
	return {
		member_id: id,
		name: `hotel-${id}`,
		reachable: true,
		has_token: true,
		master_key_matches: true,
		schema_ok: true,
		added: 0,
		updated: 0,
		removed: 0,
		...over,
	};
}

const toasts = () => within(screen.getByRole("status"));
const btn = (key: string) =>
	screen.getByRole("button", { name: i18n.t(key) as string });

function renderWizard() {
	render(
		<ToastProvider>
			<FleetSyncWizard members={members} onChanged={() => {}} />
		</ToastProvider>,
	);
}

async function pick(id: string) {
	await userEvent.selectOptions(
		await screen.findByLabelText(i18n.t("settings.wizard.primaryLabel")),
		id,
	);
}

async function toStep3(id = "1") {
	await pick(id);
	await waitFor(() => expect(btn("settings.wizard.next")).toBeEnabled());
	await userEvent.click(btn("settings.wizard.next"));
	await userEvent.click(btn("settings.wizard.next"));
}

beforeEach(() => {
	server.use(
		http.get("/api/fleet/autosync", () =>
			HttpResponse.json({ enabled: false, primary_id: "" }),
		),
		http.get(
			"/api/fleet/last-sync",
			() => new HttpResponse(null, { status: 204 }),
		),
		http.put("/api/fleet/autosync", async ({ request }) => {
			const body = (await request.json()) as Record<string, unknown>;
			return HttpResponse.json({
				enabled: body.enabled ?? true,
				primary_id: body.primary_id ?? "",
			});
		}),
		http.post("/api/fleet/version-check", () =>
			HttpResponse.json({
				primary_id: "1",
				primary_version: "v1.0.0",
				skewed: [],
			}),
		),
	);
});

describe("FleetSyncWizard error paths", () => {
	it("opens the wizard when the saved designation cannot be read", async () => {
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ error: "db down" }, { status: 500 }),
			),
		);
		renderWizard();
		expect(
			await screen.findByLabelText(i18n.t("settings.wizard.primaryLabel")),
		).toBeInTheDocument();
	});

	it("drops a failed probe that answers after the operator picked another primary", async () => {
		let releaseFirst: () => void = () => {};
		const firstGate = new Promise<void>((r) => {
			releaseFirst = r;
		});
		let secondDone = false;
		server.use(
			http.get("/api/fleet/status", async ({ request }) => {
				const id = new URL(request.url).searchParams.get("primary");
				if (id === "1") {
					await firstGate;
					return HttpResponse.text("stale probe failed", { status: 502 });
				}
				secondDone = true;
				return HttpResponse.json({
					primary_id: "2",
					primary_reachable: true,
					members: [row("2")],
				});
			}),
		);
		renderWizard();
		await pick("1");
		await pick("2");
		await waitFor(() => expect(secondDone).toBe(true));
		await waitFor(() => expect(btn("settings.wizard.next")).toBeEnabled());
		releaseFirst();
		// Give the stale rejection time to land; it must not toast.
		await new Promise((r) => setTimeout(r, 50));
		expect(screen.queryByText("stale probe failed")).toBeNull();
		expect(btn("settings.wizard.next")).toBeEnabled();
	});

	it("keeps the sync blocked and toasts when the version check fails", async () => {
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [row("1"), row("2")],
				}),
			),
			http.post("/api/fleet/version-check", () =>
				HttpResponse.text("version probe failed", { status: 502 }),
			),
		);
		renderWizard();
		await toStep3();
		await waitFor(() =>
			expect(toasts().getByText("version probe failed")).toBeInTheDocument(),
		);
		// No result: the proceed action stays disabled (fail closed).
		expect(btn("settings.wizard.continueNoChanges")).toBeDisabled();
	});

	it("stays on the config step and re-probes when the push fails; Back to a closed gate is refused", async () => {
		let probes = 0;
		server.use(
			http.get("/api/fleet/status", () => {
				probes += 1;
				// The re-probe after the failed push finds the primary unreachable,
				// which closes the step 2 gate behind the operator.
				return HttpResponse.json({
					primary_id: "1",
					primary_reachable: probes === 1,
					members: [row("1"), row("2", { added: 1 })],
				});
			}),
			http.post("/api/config/sync", () =>
				HttpResponse.text("push exploded", { status: 502 }),
			),
		);
		renderWizard();
		await toStep3();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.wizard.syncConfigBtn"),
			}),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(within(dialog).getByRole("checkbox"));
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: i18n.t("settings.configSyncDo", { count: 1 }),
			}),
		);
		await waitFor(() =>
			expect(toasts().getByText("push exploded")).toBeInTheDocument(),
		);
		await waitFor(() => expect(probes).toBe(2));
		// Still on the config step, not the resting screen.
		expect(
			screen.getByText(i18n.t("settings.wizard.step3Title")),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", {
				name: i18n.t("settings.wizard.rerunBtn"),
			}),
		).toBeNull();
		// Step 2 is no longer unlocked, so Back does nothing.
		await userEvent.click(btn("settings.wizard.back"));
		expect(
			screen.getByText(i18n.t("settings.wizard.step3Title")),
		).toBeInTheDocument();
	});
});

describe("FleetSyncWizard resting screen", () => {
	const designated = () =>
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
		);

	it("toasts the backend error when pausing auto-sync fails", async () => {
		designated();
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [row("1")],
				}),
			),
			http.put("/api/fleet/autosync", () =>
				HttpResponse.text("pause refused", { status: 500 }),
			),
		);
		renderWizard();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.wizard.pauseBtn"),
			}),
		);
		await waitFor(() =>
			expect(toasts().getByText("pause refused")).toBeInTheDocument(),
		);
		// The enabled state did not flip.
		expect(
			screen.getByText(i18n.t("settings.wizard.autoSyncOn")),
		).toBeInTheDocument();
	});

	it("cancelling a re-run restores the saved primary and re-probes it", async () => {
		designated();
		const probed: string[] = [];
		server.use(
			http.get("/api/fleet/status", ({ request }) => {
				const id = new URL(request.url).searchParams.get("primary") ?? "";
				probed.push(id);
				return HttpResponse.json({
					primary_id: id,
					primary_reachable: true,
					members: [row(id)],
				});
			}),
		);
		renderWizard();
		await userEvent.click(
			await screen.findByRole("button", {
				name: i18n.t("settings.wizard.rerunBtn"),
			}),
		);
		await pick("2");
		await waitFor(() => expect(probed).toEqual(["1", "2"]));
		await userEvent.click(btn("settings.wizard.cancelRerun"));
		// Back on the resting screen, probing the saved primary again.
		expect(
			await screen.findByRole("button", {
				name: i18n.t("settings.wizard.rerunBtn"),
			}),
		).toBeInTheDocument();
		await waitFor(() => expect(probed).toEqual(["1", "2", "1"]));
		expect(
			screen.queryByLabelText(i18n.t("settings.wizard.primaryLabel")),
		).toBeNull();
	});
});
