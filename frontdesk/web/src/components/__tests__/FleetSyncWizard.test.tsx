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

function primaryRow(id = "1", name = "hotel-1"): FleetMemberStatus {
	return {
		member_id: id,
		name,
		reachable: true,
		has_token: true,
		master_key_matches: true,
		schema_ok: true,
		added: 0,
		updated: 0,
		removed: 0,
		note: "primary (source of truth)",
	};
}

// Records every PUT /api/fleet/autosync body so tests can assert the token gate.
let autosyncPuts: Array<Record<string, unknown>>;

function renderWizard() {
	render(
		<ToastProvider>
			<FleetSyncWizard members={members} onChanged={() => {}} />
		</ToastProvider>,
	);
}

// The merged component reads the persisted designation and the last-run marker on
// mount. Default to "no primary set" (fresh wizard) + "never run"; individual
// tests override with their own server.use, which takes precedence.
beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	autosyncPuts = [];
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
			autosyncPuts.push(body);
			return HttpResponse.json({
				enabled: body.enabled ?? true,
				primary_id: body.primary_id ?? "",
			});
		}),
	);
});

async function pickPrimary(id = "1") {
	await userEvent.selectOptions(await screen.findByLabelText(/Primary/i), id);
}

describe("FleetSyncWizard", () => {
	it("shows the last-run banner in step 1 when the wizard has run before", async () => {
		server.use(
			http.get("/api/fleet/last-sync", () =>
				HttpResponse.json({
					last_run_at: "2026-06-20T08:00:00Z",
					primary_id: "1",
					primary_name: "hotel-1",
				}),
			),
		);
		renderWizard();
		expect(
			await screen.findByText(/You last synced this fleet/i),
		).toBeInTheDocument();
	});

	it("blocks the config step until MASTER_KEY matches on every member", async () => {
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
							// Schema skew: the member checks its schema before the MASTER_KEY
							// canary, so master_key_matches is unevaluated (null) and diff
							// counts are zero. Without the schema gate this member would slip
							// through to the config step, where config sync then fails for it.
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

		// The schema remedy is shown and the config step stays locked.
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
		expect(
			await screen.findByText("the primary is on fire"),
		).toBeInTheDocument();
	});

	it("walks every step, persists the primary, syncs, and lands on the resting screen", async () => {
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
							master_key_matches: true,
							schema_ok: true,
							added: configSynced ? 0 : 1,
							updated: 0,
							removed: 0,
						},
					],
				}),
			),
			http.post("/api/config/sync", () => {
				configSynced = true;
				return HttpResponse.json({
					results: [{ member_id: "2", name: "hotel-2", ok: true }],
				});
			}),
		);
		renderWizard();
		await pickPrimary();

		const next = screen.getByRole("button", { name: "Next" });
		await waitFor(() => expect(next).toBeEnabled());
		await userEvent.click(next); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3

		await userEvent.click(
			await screen.findByRole("button", { name: /Sync configuration now/i }),
		);
		const dialog = await screen.findByRole("dialog");
		// A first setup does not change the primary, so no admin token is asked for.
		expect(
			within(dialog).queryByLabelText(/Admin token/i),
		).not.toBeInTheDocument();
		await userEvent.click(within(dialog).getByRole("checkbox"));
		await userEvent.click(
			within(dialog).getByRole("button", {
				name: /Replace config on 1 member now/i,
			}),
		);

		// Designation was persisted with auto-sync on, then the fleet was synced.
		await waitFor(() =>
			expect(autosyncPuts).toEqual([{ enabled: true, primary_id: "1" }]),
		);
		expect(configSynced).toBe(true);

		// Resting screen: source of truth + usage URLs from the configured lb_port.
		expect(await screen.findByText("Auto-sync on")).toBeInTheDocument();
		expect(screen.getByText("http://localhost:9090/v1")).toBeInTheDocument();
		expect(screen.getByText("https://hotel-2.example.com")).toBeInTheDocument();
	});

	it("reaches the resting screen through the config step when there is nothing to sync", async () => {
		let syncCalled = false;
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
							master_key_matches: true,
							schema_ok: true,
							added: 0,
							updated: 0,
							removed: 0,
						},
					],
				}),
			),
			http.post("/api/config/sync", () => {
				syncCalled = true;
				return HttpResponse.json({ results: [] });
			}),
		);
		renderWizard();
		await pickPrimary();

		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(),
		);
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3

		expect(
			await screen.findByText(/already matches the primary/i),
		).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Continue" }));

		// Primary persisted, but no destructive push happened.
		await waitFor(() =>
			expect(autosyncPuts).toEqual([{ enabled: true, primary_id: "1" }]),
		);
		expect(syncCalled).toBe(false);
		expect(await screen.findByText("Auto-sync on")).toBeInTheDocument();
	});

	it("opens on the resting screen when a primary is already designated", async () => {
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					lb_port: "9090",
					members: [primaryRow()],
				}),
			),
		);
		renderWizard();
		expect(await screen.findByText("Auto-sync on")).toBeInTheDocument();
		expect(screen.getByText("hotel-1")).toBeInTheDocument();
		expect(screen.getByText("Auto-sync on")).toBeInTheDocument();
		// No wizard picker is shown while resting.
		expect(screen.queryByLabelText(/Select the primary/i)).toBeNull();
	});

	it("pauses and resumes auto-sync without a token", async () => {
		let enabled = true;
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled, primary_id: "1" }),
			),
			http.put("/api/fleet/autosync", async ({ request }) => {
				const body = (await request.json()) as Record<string, unknown>;
				autosyncPuts.push(body);
				enabled = Boolean(body.enabled);
				return HttpResponse.json({ enabled, primary_id: "1" });
			}),
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [primaryRow()],
				}),
			),
		);
		renderWizard();

		await userEvent.click(await screen.findByRole("button", { name: "Pause" }));
		expect(await screen.findByText("Auto-sync paused")).toBeInTheDocument();
		// Pausing flips only the enabled flag; the backend is never sent a token.
		expect(autosyncPuts).toEqual([{ enabled: false, primary_id: "1" }]);
	});

	it("rejects re-selecting the current primary as the new primary", async () => {
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "1" }),
			),
			http.get("/api/fleet/status", () =>
				HttpResponse.json({
					primary_id: "1",
					primary_reachable: true,
					members: [primaryRow()],
				}),
			),
		);
		renderWizard();
		expect(await screen.findByText("Auto-sync paused")).toBeInTheDocument();

		// Re-run the wizard and re-select the SAME primary: this is not a valid
		// change (the source of truth cannot be replaced with itself).
		await userEvent.click(
			screen.getByRole("button", { name: "Re-run wizard" }),
		);
		await pickPrimary("1");
		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(),
		);
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3

		// The proceed action is disabled and the same-host reason is shown; no PUT
		// is made.
		expect(
			await screen.findByText(/cannot replace the primary with the same host/i),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
		expect(autosyncPuts).toEqual([]);
	});

	it("surfaces the backend 409 when repointing onto the same host", async () => {
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
			http.get("/api/fleet/status", ({ request }) => {
				const id = new URL(request.url).searchParams.get("primary") ?? "1";
				return HttpResponse.json({
					primary_id: id,
					primary_reachable: true,
					members: [primaryRow(id, `hotel-${id}`)],
				});
			}),
			// The backend detects member 2 is the same physical host as the current
			// primary (self-reports is_primary) and rejects the repoint with 409.
			http.put("/api/fleet/autosync", () =>
				HttpResponse.json({ error: "same host" }, { status: 409 }),
			),
		);
		renderWizard();
		await userEvent.click(
			await screen.findByRole("button", { name: "Re-run wizard" }),
		);
		await pickPrimary("2");
		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(),
		);
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3
		await userEvent.click(
			screen.getByRole("button", { name: "Set as primary" }),
		);
		// Confirm the token-gated change.
		const dialog = await screen.findByRole("dialog");
		await userEvent.type(
			within(dialog).getByLabelText(/Admin token/i),
			"test-token",
		);
		await userEvent.click(
			within(dialog).getByRole("button", { name: /Change primary/i }),
		);

		expect(
			await screen.findByText(/cannot replace the primary with the same host/i),
		).toBeInTheDocument();
	});

	it("gates a re-run that changes the primary behind the admin token", async () => {
		server.use(
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
			// Whichever member is probed comes back fully converged, so the re-run
			// reaches the config step with nothing to push.
			http.get("/api/fleet/status", ({ request }) => {
				const id = new URL(request.url).searchParams.get("primary") ?? "1";
				return HttpResponse.json({
					primary_id: id,
					primary_reachable: true,
					members: [primaryRow(id, `hotel-${id}`)],
				});
			}),
		);
		renderWizard();

		await userEvent.click(
			await screen.findByRole("button", { name: "Re-run wizard" }),
		);
		await pickPrimary("2");
		await waitFor(() =>
			expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(),
		);
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 2
		await userEvent.click(screen.getByRole("button", { name: "Next" })); // -> 3

		// Changing an existing primary needs the token before it will commit.
		await userEvent.click(
			await screen.findByRole("button", { name: "Set as primary" }),
		);
		const dialog = await screen.findByRole("dialog");
		const confirm = within(dialog).getByRole("button", {
			name: "Change primary",
		});
		expect(confirm).toBeDisabled();
		await userEvent.type(
			within(dialog).getByLabelText(/Admin token/i),
			"secret-admin-token",
		);
		expect(confirm).toBeEnabled();
		await userEvent.click(confirm);

		await waitFor(() =>
			expect(autosyncPuts).toEqual([
				{ enabled: true, primary_id: "2", confirm_token: "secret-admin-token" },
			]),
		);
	});
});
