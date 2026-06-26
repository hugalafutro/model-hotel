import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FleetMemberStatus, MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { FleetSyncWizard } from "../FleetSyncWizard";

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

function primaryRow(): FleetMemberStatus {
	return {
		member_id: "1",
		name: "hotel-1",
		reachable: true,
		has_token: true,
		admin_token_matches: true,
		master_key_matches: true,
		schema_ok: true,
		added: 0,
		updated: 0,
		removed: 0,
		note: "primary (source of truth)",
	};
}

function renderWizard() {
	render(
		<ToastProvider>
			<FleetSyncWizard members={members} onChanged={() => {}} />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
});

async function pickPrimary() {
	await userEvent.selectOptions(screen.getByLabelText(/Primary/i), "1");
}

describe("FleetSyncWizard", () => {
	it("blocks the admin-token step until MASTER_KEY matches on every member", async () => {
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [
						primaryRow(),
						{
							member_id: "2",
							name: "hotel-2",
							reachable: true,
							has_token: true,
							admin_token_matches: false,
							master_key_matches: false,
							schema_ok: true,
							added: 0,
							updated: 0,
							removed: 0,
							note: "MASTER_KEY does not match the primary",
						},
					],
				}),
			),
		);
		renderWizard();
		await pickPrimary();

		const next = screen.getByRole("button", { name: "Next" });
		await waitFor(() => expect(next).toBeEnabled());
		await userEvent.click(next); // -> step 2 (MASTER_KEY)

		expect(await screen.findByText(/Set the same MASTER_KEY/i)).toBeVisible();
		// The mismatch must keep the admin-token step locked.
		expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
	});

	it("blocks past MASTER_KEY when a reachable member's schema is too old", async () => {
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [
						primaryRow(),
						{
							member_id: "2",
							name: "hotel-2",
							reachable: true,
							has_token: true,
							admin_token_matches: true,
							// Schema skew: the member checks its schema before the MASTER_KEY
							// canary, so master_key_matches is unevaluated (null) and diff
							// counts are zero. Without the schema gate this member would slip
							// through every step to Done, where config sync then fails for it.
							master_key_matches: null,
							schema_ok: false,
							added: 0,
							updated: 0,
							removed: 0,
							note: "this member's app version is too old to sync with the primary",
						},
					],
				}),
			),
		);
		renderWizard();
		await pickPrimary();
		const next = screen.getByRole("button", { name: "Next" });
		await waitFor(() => expect(next).toBeEnabled());
		await userEvent.click(next); // -> step 2 (MASTER_KEY)

		// The schema remedy is shown and the admin-token step stays locked.
		expect(await screen.findByText(/older app version/i)).toBeVisible();
		expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
	});

	it("shows the reason inline when the chosen primary is unusable (null member list)", async () => {
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: false,
					primary_note: "could not reach this member",
					members: null,
				}),
			),
		);
		renderWizard();
		await pickPrimary();
		// Must not crash on the null member list; shows the reason and stays on step 1.
		expect(
			await screen.findByText(/cannot be the primary/i),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
	});

	it("surfaces the backend error instead of a generic toast", async () => {
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.text("the primary is on fire", { status: 502 }),
			),
		);
		renderWizard();
		await pickPrimary();
		// The real backend message, not "Something went wrong".
		expect(
			await screen.findByText("the primary is on fire"),
		).toBeInTheDocument();
	});

	it("re-locks the Done step when the primary changes after a config sync", async () => {
		// Both members are token- and key-converged, but each has one pending config
		// change, so step 5 is only reachable once config has been synced for the
		// CURRENT primary. The handler echoes whichever primary is asked about.
		server.use(
			http.get("/api/fleet/status", ({ request }) => {
				const id = new URL(request.url).searchParams.get("primary") ?? "1";
				const other = id === "1" ? "2" : "1";
				return HttpResponse.json({
					primary_id: id,
					primary_reachable: true,
					members: [
						{ ...primaryRow(), member_id: id, name: `hotel-${id}` },
						{
							member_id: other,
							name: `hotel-${other}`,
							reachable: true,
							has_token: true,
							admin_token_matches: true,
							master_key_matches: true,
							schema_ok: true,
							added: 1,
							updated: 0,
							removed: 0,
						},
					],
				});
			}),
			http.post("/api/config/sync", () =>
				HttpResponse.json({ results: [{ member_id: "2", ok: true }] }),
			),
		);
		renderWizard();
		await pickPrimary(); // primary "1"

		// Walk to the config step and sync it, which lands on the Done step.
		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(),
		);
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 4
		await userEvent.click(
			await screen.findByRole("button", { name: /Sync configuration now/i }),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(within(dialog).getByRole("checkbox"));
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: /Replace config on 1 member now/i,
			}),
		);
		// configDone is set for primary "1": we land on the Done step.
		expect(await screen.findByText("Step 5: Done")).toBeInTheDocument();

		// Walk back to step 1 and switch to a primary whose config was never synced.
		for (let i = 0; i < 4; i++) {
			await userEvent.click(screen.getByRole("button", { name: "Back" }));
		}
		await userEvent.selectOptions(screen.getByLabelText(/Primary/i), "2");

		// configDone must have been reset, so the Done step is gated again: the
		// step-4 Next button (which advances to step 5) stays disabled until the new
		// primary's config is synced.
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 4
		expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
	});

	it("walks every step, syncs admin token then config, and reports the summary", async () => {
		let adminSynced = false;
		let configSynced = false;
		server.use(
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					lb_port: "9090",
					members: [
						primaryRow(),
						{
							member_id: "2",
							name: "hotel-2",
							reachable: true,
							has_token: true,
							admin_token_matches: adminSynced,
							master_key_matches: true,
							schema_ok: true,
							added: configSynced ? 0 : 1,
							updated: 0,
							removed: 0,
						},
					],
				}),
			),
			http.post("/api/admin-token/sync", () => {
				adminSynced = true;
				return HttpResponse.json({
					results: [{ member_id: "2", name: "hotel-2", ok: true }],
				});
			}),
			http.post("/api/config/sync", () => {
				configSynced = true;
				return HttpResponse.json({
					results: [{ member_id: "2", name: "hotel-2", ok: true }],
				});
			}),
		);
		renderWizard();
		await pickPrimary();

		// Step 1 -> 2 (MASTER_KEY ok).
		const toStep2 = screen.getByRole("button", { name: "Next" });
		await waitFor(() => expect(toStep2).toBeEnabled());
		await userEvent.click(toStep2);
		expect(
			await screen.findByText(/shares the primary's MASTER_KEY/i),
		).toBeVisible();

		// Step 2 -> 3 (admin token).
		await userEvent.click(screen.getByRole("button", { name: "Next" }));
		await userEvent.click(
			await screen.findByRole("button", { name: /Sync admin token now/i }),
		);
		const adminDialog = await screen.findByRole("dialog");
		await userEvent.click(within(adminDialog).getByRole("checkbox"));
		await userEvent.click(
			within(adminDialog).getByRole("button", {
				name: /Overwrite 1 member now/i,
			}),
		);
		// After syncing, the step reports success and unlocks the next.
		expect(
			await screen.findByText(/already uses the primary's admin token/i),
		).toBeVisible();

		// Step 3 -> 4 (config).
		await userEvent.click(screen.getByRole("button", { name: "Next" }));
		await userEvent.click(
			await screen.findByRole("button", { name: /Sync configuration now/i }),
		);
		const configDialog = await screen.findByRole("dialog");
		await userEvent.click(within(configDialog).getByRole("checkbox"));
		await userEvent.click(
			within(configDialog).getByRole("button", {
				name: /Replace config on 1 member now/i,
			}),
		);

		// Step 5 summary names the primary and the synced count.
		expect(
			await screen.findByText(/1 instance is now synced to hotel-1/i),
		).toBeInTheDocument();
		expect(adminSynced).toBe(true);
		expect(configSynced).toBe(true);

		// The Done step tells the operator where to send traffic: the configured
		// lb_port (9090) paired with the current host, both the direct /v1 URL and
		// the reverse-proxy forward target, plus the http/https guidance.
		expect(screen.getByText("http://localhost:9090/v1")).toBeInTheDocument();
		expect(screen.getByText("http://localhost:9090")).toBeInTheDocument();
		expect(
			screen.getByText(/data plane works over plain http/i),
		).toBeInTheDocument();
		// The load-balancer pool lists the active member backends.
		expect(screen.getByText("https://hotel-2.example.com")).toBeInTheDocument();
	});
});
