import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseEmitting, sseHandler } from "../../test/sse";
import { MembersPage } from "../MembersPage";

function member(
	overrides: Partial<MemberView> & { id: string; name: string },
): MemberView {
	return {
		url: `https://${overrides.name}.example.com`,
		state: "active",
		has_token: false,
		created_at: new Date().toISOString(),
		updated_at: new Date().toISOString(),
		status: {
			health: {
				known: true,
				healthy: true,
				latency_ms: 12,
				checked_at: new Date().toISOString(),
			},
		},
		...overrides,
	};
}

function renderPage() {
	return render(
		<ToastProvider>
			<MembersPage />
		</ToastProvider>,
	);
}

beforeEach(() => {
	server.use(sseHandler());
	// Default: no primary designated. The page derives the primary from
	// /api/fleet/autosync (primary_id), the single source of truth;
	// tests that care override this with a non-empty primary_id. The last-sync
	// endpoint is left mocked for any other consumer but no longer drives the badge.
	server.use(
		http.get("/api/fleet/autosync", () =>
			HttpResponse.json({ enabled: false, primary_id: "" }),
		),
		http.get(
			"/api/fleet/last-sync",
			() => new HttpResponse(null, { status: 204 }),
		),
	);
});

describe("MembersPage", () => {
	it("lists members with health badges and a version-mismatch flag", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						status: {
							health: {
								known: true,
								healthy: true,
								latency_ms: 9,
								checked_at: "",
							},
							traefik_status: "UP",
							version: "0.9.80",
						},
					}),
					member({
						id: "2",
						name: "hotel-2",
						status: {
							health: {
								known: true,
								healthy: false,
								latency_ms: 0,
								checked_at: "",
							},
							traefik_status: "DOWN",
							version: "0.9.79",
						},
					}),
					member({
						id: "3",
						name: "hotel-3",
						status: {
							health: {
								known: true,
								healthy: true,
								latency_ms: 11,
								checked_at: "",
							},
							version: "0.9.80",
						},
					}),
				]),
			),
		);
		renderPage();
		expect(await screen.findByText("hotel-1")).toBeInTheDocument();
		// hotel-2 is the minority version (0.9.79 vs two 0.9.80) → mismatch flag.
		const row2 = screen.getByText("hotel-2").closest("tr") as HTMLElement;
		expect(
			within(row2).getByTitle(/differs from the group/i),
		).toBeInTheDocument();
		// hotel-2 is down in both the Front Desk and Traefik views.
		expect(within(row2).getAllByText(/Down/i)).toHaveLength(2);
	});

	it("shows the auto-sync verified-in-sync heartbeat per member", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					// Verified recently: the heartbeat time renders.
					member({
						id: "1",
						name: "hotel-1",
						status: {
							health: {
								known: true,
								healthy: true,
								latency_ms: 9,
								checked_at: "",
							},
							auto_sync_verified_at: new Date().toISOString(),
						},
					}),
					// Never verified (or unreachable): the cell falls back to "Not yet".
					member({ id: "2", name: "hotel-2" }),
				]),
			),
		);
		renderPage();
		const row1 = (await screen.findByText("hotel-1")).closest(
			"tr",
		) as HTMLElement;
		// A relative-time string, not the "not yet" fallback.
		const verified1 = within(row1).getByTestId("member-verified");
		expect(verified1).toHaveTextContent(/ago|now/i);
		expect(verified1).not.toHaveTextContent(/not yet/i);

		const row2 = screen.getByText("hotel-2").closest("tr") as HTMLElement;
		expect(within(row2).getByTestId("member-verified")).toHaveTextContent(
			/not yet/i,
		);
	});

	it("explains the Verified and Last Config Sync columns in header tooltips", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		// The two columns are routinely misread as the same claim; each header
		// carries a tooltip stating what it actually measures (live hash check vs
		// last write). Distinct, non-empty titles are the contract.
		const verified = screen.getByTestId("col-verified");
		const lastSync = screen.getByTestId("col-last-sync");
		expect(verified.getAttribute("title")).toBeTruthy();
		expect(lastSync.getAttribute("title")).toBeTruthy();
		// i18next renders the raw key for a missing translation, which is truthy
		// and distinct; requiring resolved copy makes a deleted locale key fail
		// here instead of shipping a tooltip that reads "members.colVerifiedTip".
		expect(verified.getAttribute("title")).not.toMatch(/^members\./);
		expect(lastSync.getAttribute("title")).not.toMatch(/^members\./);
		expect(verified.getAttribute("title")).not.toBe(
			lastSync.getAttribute("title"),
		);
	});

	it("shows a last-updated footer below the members card", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		expect(screen.getByTestId("members-last-updated")).toHaveTextContent(
			/last updated/i,
		);
	});

	it("shows the empty state and first-member primary notice", async () => {
		server.use(http.get("/api/members", () => HttpResponse.json([])));
		renderPage();
		expect(await screen.findByText(/No members yet/i)).toBeInTheDocument();
		expect(screen.getByText(/default sync primary/i)).toBeInTheDocument();
	});

	it("adds a member and refetches", async () => {
		let created = false;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json(
					created ? [member({ id: "1", name: "hotel-1" })] : [],
				),
			),
			http.post("/api/members", async ({ request }) => {
				const body = (await request.json()) as { name: string; url: string };
				expect(body.name).toBe("hotel-1");
				created = true;
				return HttpResponse.json(member({ id: "1", name: body.name }), {
					status: 201,
				});
			}),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "hotel-1");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://hotel-1.example.com",
		);
		// A token is required to add: the host is verified before it is saved.
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByText("hotel-1")).toBeInTheDocument();
	});

	it("disables Add until an admin token is entered", async () => {
		server.use(http.get("/api/members", () => HttpResponse.json([])));
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "hotel-1");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://hotel-1.example.com",
		);
		// No token yet: Add stays disabled (the host cannot be verified without it).
		expect(screen.getByRole("button", { name: /^Add$/i })).toBeDisabled();
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		expect(screen.getByRole("button", { name: /^Add$/i })).toBeEnabled();
	});

	it("surfaces the https-required validation error", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post(
				"/api/members",
				() =>
					new HttpResponse(
						"frontdesk: validation failed: url must use https; set FRONTDESK_ALLOW_HTTP_MEMBERS=true",
						{ status: 400 },
					),
			),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "h1");
		await userEvent.type(screen.getByLabelText(/Base URL/i), "http://h1.local");
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/must use https/i,
		);
	});

	it("surfaces the already-a-member rejection when adding a duplicate host", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post(
				"/api/members",
				() =>
					new HttpResponse(
						"This host is already a member (added under a different address). Remove the existing entry first if you want to re-add it.",
						{ status: 409 },
					),
			),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "hotel-1-lan");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://hotel-1.lan.example.com",
		);
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/already a member/i,
		);
	});

	it("surfaces the already-primary rejection when adding the primary's host", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post(
				"/api/members",
				() =>
					new HttpResponse(
						"This host is already the fleet primary (the config source of truth), reached under a different address. It cannot also be added as a member.",
						{ status: 409 },
					),
			),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "hotel-1-lan");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://hotel-1.lan.example.com",
		);
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/already the fleet primary/i,
		);
	});

	it("shows the backend message when the member refuses the token", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post(
				"/api/members",
				() =>
					new HttpResponse(
						"This member rejected the admin token (HTTP 401). Double-check the token and try again.",
						{ status: 400 },
					),
			),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "h1");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://h1.example.com",
		);
		await userEvent.type(screen.getByLabelText(/Admin token/i), "wrong");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/rejected the admin token/i,
		);
	});

	it("drains a member after clicking Drain", async () => {
		let state = "active";
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						state: state as "active" | "drained",
					}),
					// A second active member so hotel-1's Drain control is enabled
					// (the last active member cannot be drained).
					member({ id: "2", name: "hotel-2", state: "active" }),
				]),
			),
			http.post("/api/members/1/state", async ({ request }) => {
				const body = (await request.json()) as { state: string };
				state = body.state;
				return HttpResponse.json(
					member({
						id: "1",
						name: "hotel-1",
						state: state as "active" | "drained",
					}),
				);
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");
		// Two active members means two Drain buttons; scope to hotel-1's row.
		const row1 = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row1).getByRole("button", { name: /^Drain$/i }),
		);
		// The action toggles to "Activate" and the state badge flips to Drained.
		await waitFor(() => {
			const r = screen.getByText("hotel-1").closest("tr") as HTMLElement;
			expect(
				within(r).getByRole("button", { name: /^Activate$/i }),
			).toBeInTheDocument();
		});
	});

	it("removes a member after confirming", async () => {
		// Three members so this is a plain removal; the two-member and lone-row
		// variants disband instead (covered below).
		let deleted = false;
		const roster = () => [
			...(deleted ? [] : [member({ id: "1", name: "hotel-1" })]),
			member({ id: "2", name: "hotel-2" }),
			member({ id: "3", name: "hotel-3" }),
		];
		server.use(
			http.get("/api/members", () => HttpResponse.json(roster())),
			http.delete("/api/members/1", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");
		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		// Confirm modal → click the destructive Remove inside it.
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);
		await waitFor(() =>
			expect(screen.queryByText("hotel-1")).not.toBeInTheDocument(),
		);
	});

	it("live-refreshes the list when a membership event arrives", async () => {
		let calls = 0;
		server.use(
			http.get("/api/members", () => {
				calls += 1;
				return HttpResponse.json(
					calls === 1
						? [member({ id: "1", name: "hotel-1" })]
						: [
								member({ id: "1", name: "hotel-1" }),
								member({ id: "2", name: "hotel-2" }),
							],
				);
			}),
			// SSE pushes a membership event on connect, which should trigger a refetch.
			sseEmitting([
				{
					id: "e1",
					type: "member.added",
					severity: "info",
					source: "frontdesk",
					message: "added",
					created_at: "",
				},
			]),
		);
		renderPage();
		await screen.findByText("hotel-1");
		expect(await screen.findByText("hotel-2")).toBeInTheDocument();
	});

	it("live-refreshes the primary badge when a settings event arrives", async () => {
		// Repointing the primary (or toggling auto-sync) emits only
		// settings.changed, so the event filter must refresh the auto-sync
		// status on it or the badge stays stale until the next unrelated event.
		let autosyncCalls = 0;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/fleet/autosync", () => {
				autosyncCalls += 1;
				return HttpResponse.json(
					autosyncCalls === 1
						? { enabled: false, primary_id: "" }
						: { enabled: true, primary_id: "1" },
				);
			}),
			sseEmitting([
				{
					id: "e1",
					type: "settings.changed",
					severity: "info",
					source: "frontdesk",
					message: "auto-sync settings updated",
					created_at: "",
				},
			]),
		);
		renderPage();
		await screen.findByText("hotel-1");
		expect(await screen.findByTestId("primary-badge")).toBeInTheDocument();
	});

	it("shows the error state when the list cannot be loaded", async () => {
		server.use(
			http.get("/api/members", () => new HttpResponse("boom", { status: 500 })),
		);
		renderPage();
		expect(
			await screen.findByText(/Could not reach Front Desk/i),
		).toBeInTheDocument();
	});

	it("activates a drained member", async () => {
		let state: "active" | "drained" = "drained";
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1", state })]),
			),
			http.post("/api/members/1/state", async ({ request }) => {
				state = ((await request.json()) as { state: "active" | "drained" })
					.state;
				return HttpResponse.json(member({ id: "1", name: "hotel-1", state }));
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");
		await userEvent.click(screen.getByRole("button", { name: /^Activate$/i }));
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: /^Drain$/i }),
			).toBeInTheDocument(),
		);
	});

	it("disables Drain for the sole active member", async () => {
		// One active member (plus a drained one) means draining the active one would
		// empty the routing pool, so its Drain control is disabled.
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1", state: "active" }),
					member({ id: "2", name: "hotel-2", state: "drained" }),
				]),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		expect(screen.getByRole("button", { name: /^Drain$/i })).toBeDisabled();
	});

	it("enables Drain when another active member remains", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1", state: "active" }),
					member({ id: "2", name: "hotel-2", state: "active" }),
				]),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		const drains = screen.getAllByRole("button", { name: /^Drain$/i });
		expect(drains).toHaveLength(2);
		for (const b of drains) expect(b).toBeEnabled();
	});

	it("shows an unknown health badge when the poller has no reading yet", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						status: {
							health: {
								known: false,
								healthy: false,
								latency_ms: 0,
								checked_at: "",
							},
						},
					}),
				]),
			),
		);
		renderPage();
		const row = (await screen.findByText("hotel-1")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row).getByText(/Unknown/i)).toBeInTheDocument();
	});

	it("badges the fleet primary and pins it to the top", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "2" }),
			),
		);
		renderPage();

		const badge = await screen.findByTestId("primary-badge");
		const primaryRow = badge.closest("tr") as HTMLElement;
		expect(within(primaryRow).getByText("hotel-2")).toBeInTheDocument();
		expect(primaryRow).toHaveClass("fd-row-primary");

		// The primary is the config source, so it has no "last config sync" of its
		// own: the cell reads "n/a" with an explanatory tooltip, never an em dash.
		const lastSyncCell = within(primaryRow).getByTitle(
			"This member is the source of config.",
		);
		expect(lastSyncCell).toHaveTextContent("n/a");
		expect(primaryRow.textContent).not.toContain("—");

		// hotel-2 is the recorded primary, so it sorts above hotel-1 even though it
		// was returned second by the API.
		const bodyRows = within(screen.getByRole("table")).getAllByRole("row");
		expect(within(bodyRows[1]).getByText("hotel-2")).toBeInTheDocument();
	});

	it("shows the commit instead of the shared 'dev' placeholder", async () => {
		const health = {
			known: true,
			healthy: true,
			latency_ms: 9,
			checked_at: "",
		};
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						status: { health, version: "dev", commit: "b80c04d4494f" },
					}),
					// A release tag identifies itself, so it stays the label.
					member({
						id: "2",
						name: "hotel-2",
						status: { health, version: "v1.2.3", commit: "aaaaaaaaaaaa" },
					}),
					// Nothing to show but the version: a member that reports no commit.
					member({
						id: "3",
						name: "hotel-3",
						status: { health, version: "dev", commit: "unknown" },
					}),
				]),
			),
		);
		renderPage();

		const row1 = (await screen.findByText("hotel-1")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row1).getByText("b80c04d4494f")).toBeInTheDocument();
		expect(within(row1).queryByText("dev")).toBeNull();
		// The version is not lost, it moves to the hover detail.
		expect(within(row1).getByTitle("dev · b80c04d4494f")).toBeInTheDocument();

		const row2 = (await screen.findByText("hotel-2")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row2).getByText("v1.2.3")).toBeInTheDocument();

		const row3 = (await screen.findByText("hotel-3")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row3).getByText("dev")).toBeInTheDocument();
		expect(within(row3).queryByText("unknown")).toBeNull();
	});

	it("flags the odd build out when no primary is set", async () => {
		// No primary designated, so the majority tally is what flags divergence.
		// Every member reports "dev": a version-keyed tally sees one bucket and
		// flags nothing, which is the whole reason it counts builds.
		const health = {
			known: true,
			healthy: true,
			latency_ms: 9,
			checked_at: "",
		};
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						status: { health, version: "dev", commit: "aaaaaaaaaaaa" },
					}),
					member({
						id: "2",
						name: "hotel-2",
						status: { health, version: "dev", commit: "aaaaaaaaaaaa" },
					}),
					member({
						id: "3",
						name: "hotel-3",
						status: { health, version: "dev", commit: "bbbbbbbbbbbb" },
					}),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "" }),
			),
		);
		renderPage();

		const row3 = (await screen.findByText("hotel-3")).closest(
			"tr",
		) as HTMLElement;
		expect(
			within(row3).getByTitle(/differs from the group/i),
		).toBeInTheDocument();

		const row1 = (await screen.findByText("hotel-1")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row1).queryByTitle(/differs from the group/i)).toBeNull();
	});

	it("badges every member as held when the primary's own build is unread", async () => {
		// The gate fails closed on an unreadable version, and the primary's counts:
		// with nothing confirmed to push from, sync is refused fleet-wide. A badge
		// that stayed silent here would report the fleet in sync at the one moment
		// none of it is.
		const health = {
			known: true,
			healthy: true,
			latency_ms: 9,
			checked_at: "",
		};
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						has_token: true,
						status: { health },
					}),
					member({
						id: "2",
						name: "hotel-2",
						has_token: true,
						status: { health, version: "dev", commit: "aaaaaaaaaaaa" },
					}),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
		);
		renderPage();

		const row2 = (await screen.findByText("hotel-2")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row2).getByTestId("member-sync-held")).toBeInTheDocument();
	});

	it("badges a commit-only skew as sync held, where the versions match", async () => {
		// The case the version cannot see, and the one the backend gate now holds
		// on: both members report "dev", and only the commit separates them. A
		// badge that compared versions would call this member in sync while sync
		// is refusing it.
		const health = {
			known: true,
			healthy: true,
			latency_ms: 9,
			checked_at: "",
		};
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						has_token: true,
						status: { health, version: "dev", commit: "aaaaaaaaaaaa" },
					}),
					member({
						id: "2",
						name: "hotel-2",
						has_token: true,
						status: { health, version: "dev", commit: "bbbbbbbbbbbb" },
					}),
					member({
						id: "3",
						name: "hotel-3",
						has_token: true,
						status: { health, version: "dev", commit: "aaaaaaaaaaaa" },
					}),
					// Reports no commit: the gate falls back to the version and syncs
					// it, so the badge must stay off rather than invent a difference.
					member({
						id: "4",
						name: "hotel-4",
						has_token: true,
						status: { health, version: "dev", commit: "unknown" },
					}),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
		);
		renderPage();

		const row2 = (await screen.findByText("hotel-2")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row2).getByTestId("member-sync-held")).toBeInTheDocument();

		const row3 = (await screen.findByText("hotel-3")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row3).queryByTestId("member-sync-held")).toBeNull();

		const row4 = (await screen.findByText("hotel-4")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row4).queryByTestId("member-sync-held")).toBeNull();
	});

	it("badges a member whose version differs from the primary's as sync held", async () => {
		const health = {
			known: true,
			healthy: true,
			latency_ms: 9,
			checked_at: "",
		};
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({
						id: "1",
						name: "hotel-1",
						has_token: true,
						status: { health, version: "v1.0.0" },
					}),
					member({
						id: "2",
						name: "hotel-2",
						has_token: true,
						status: { health, version: "v0.9.0" },
					}),
					member({
						id: "3",
						name: "hotel-3",
						has_token: true,
						status: { health, version: "v1.0.0" },
					}),
					// Tokened but its version is unreadable: the backend gate fails
					// closed and holds it, so the badge must show.
					member({
						id: "4",
						name: "hotel-4",
						has_token: true,
						status: { health },
					}),
					// Tokenless members are skipped by sync entirely (never held).
					member({
						id: "5",
						name: "hotel-5",
						status: { health },
					}),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "1" }),
			),
		);
		renderPage();

		const row2 = (await screen.findByText("hotel-2")).closest(
			"tr",
		) as HTMLElement;
		expect(within(row2).getByTestId("member-sync-held")).toBeInTheDocument();
		// With a primary set, the primary-anchored badge replaces the majority
		// "odd one out" flag, so the row is not double-badged.
		expect(within(row2).queryByTitle(/differs from the group/i)).toBeNull();

		// Neither the primary itself nor an aligned member carries the badge.
		const row1 = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		expect(within(row1).queryByTestId("member-sync-held")).toBeNull();
		const row3 = screen.getByText("hotel-3").closest("tr") as HTMLElement;
		expect(within(row3).queryByTestId("member-sync-held")).toBeNull();

		// Unknown version on a tokened member is held (fail closed): badge shows.
		const row4 = screen.getByText("hotel-4").closest("tr") as HTMLElement;
		expect(within(row4).getByTestId("member-sync-held")).toBeInTheDocument();
		// A tokenless member is skipped by sync, not held: no badge.
		const row5 = screen.getByText("hotel-5").closest("tr") as HTMLElement;
		expect(within(row5).queryByTestId("member-sync-held")).toBeNull();
	});

	it("marks no primary when no fleet sync has run", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			// last-sync defaults to 204 (no primary) from beforeEach.
		);
		renderPage();
		await screen.findByText("hotel-1");
		expect(screen.queryByTestId("primary-badge")).not.toBeInTheDocument();
	});

	it("gives the primary no Remove button (it can only change via the wizard)", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "2" }),
			),
		);
		renderPage();

		const badge = await screen.findByTestId("primary-badge");
		const primaryRow = badge.closest("tr") as HTMLElement;
		// The primary is the config source of truth: no Remove button on its row.
		expect(
			within(primaryRow).queryByRole("button", { name: /^Remove$/i }),
		).not.toBeInTheDocument();

		// A non-primary member still has its Remove button.
		const otherRow = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		expect(
			within(otherRow).getByRole("button", { name: /^Remove$/i }),
		).toBeInTheDocument();
	});

	it("removes a lone bootstrap row via the disband confirm", async () => {
		let deleted = false;
		let deleteBody = "unset";
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json(
					deleted ? [] : [member({ id: "2", name: "hotel-2" })],
				),
			),
			// hotel-2 is NOT the primary (default autosync primary_id: "").
			http.delete("/api/members/2", async ({ request }) => {
				deleteBody = await request.text();
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		await screen.findByText("hotel-2");

		await userEvent.click(screen.getByRole("button", { name: /^Remove$/i }));
		const dialog = await screen.findByRole("dialog");
		// A single row is below the two-member floor, so the confirm is the
		// disband variant. No token field: it is still a plain confirm.
		expect(
			within(dialog).getByText(/disbands the whole fleet/i),
		).toBeInTheDocument();
		expect(
			within(dialog).queryByLabelText(/Admin token/i),
		).not.toBeInTheDocument();
		await userEvent.click(
			within(dialog).getByRole("button", { name: /Disband fleet/i }),
		);

		// The delete request carried no confirm_token.
		await waitFor(() => expect(deleteBody).not.toContain("confirm_token"));
		await waitFor(() =>
			expect(screen.getByText(/No members yet/i)).toBeInTheDocument(),
		);
	});

	it("warns that removing a member of a two-member fleet disbands it, primary included", async () => {
		let deleted = false;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json(
					deleted
						? []
						: [
								member({ id: "1", name: "hotel-1" }),
								member({ id: "2", name: "hotel-2" }),
							],
				),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: true, primary_id: "2" }),
			),
			http.delete("/api/members/1", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");

		// Only the non-primary row has a Remove button; its tooltip and modal both
		// carry the disband warning.
		const otherRow = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(otherRow).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		expect(within(dialog).getByText(/disband the fleet/i)).toBeInTheDocument();
		expect(
			within(dialog).getByText(/every member including the primary/i),
		).toBeInTheDocument();
		await userEvent.click(
			within(dialog).getByRole("button", { name: /Disband fleet/i }),
		);

		await waitFor(() => expect(deleted).toBe(true));
		// The success toast says the fleet is gone, not just the member.
		expect(
			await screen.findByText(/hotel-1 removed; fleet disbanded/i),
		).toBeInTheDocument();
	});

	it("keeps the plain remove confirm in a fleet of three", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
					member({ id: "3", name: "hotel-3" }),
				]),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");

		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		expect(
			within(dialog).queryByText(/disbands the whole fleet/i),
		).not.toBeInTheDocument();
		expect(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		).toBeInTheDocument();
	});

	it("surfaces the last-active-member refusal instead of a generic error", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2", state: "drained" }),
					member({ id: "3", name: "hotel-3", state: "drained" }),
				]),
			),
			http.delete("/api/members/1", () =>
				HttpResponse.json(
					{
						code: "last_active_member",
						error:
							"cannot remove the last active member: the fleet would have no routable backends",
					},
					{ status: 409 },
				),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");

		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);

		expect(
			await screen.findByText(/Cannot remove the last active member/i),
		).toBeInTheDocument();
	});

	it("gives a lone designated primary a Remove button (disband is its only exit)", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			// Legacy state from before the two-member floor: the sole row is the
			// designated primary. It must still be removable via disband.
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "1" }),
			),
		);
		renderPage();
		await screen.findByTestId("primary-badge");

		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		expect(
			within(dialog).getByText(/disbands the whole fleet/i),
		).toBeInTheDocument();
	});

	it("surfaces the membership-changed refusal and refetches the roster", async () => {
		let membersCalls = 0;
		server.use(
			http.get("/api/members", () => {
				membersCalls += 1;
				return HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
					member({ id: "3", name: "hotel-3" }),
				]);
			}),
			http.delete("/api/members/1", () =>
				HttpResponse.json(
					{
						code: "membership_changed",
						error:
							"the fleet membership changed while removing this member; review the updated list and retry",
					},
					{ status: 409 },
				),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");
		const callsBefore = membersCalls;

		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);

		expect(
			await screen.findByText(/fleet changed while removing/i),
		).toBeInTheDocument();
		// The stale roster is what caused the mismatch, so the page refetches it.
		await waitFor(() => expect(membersCalls).toBeGreaterThan(callsBefore));
	});

	it("falls back to the generic error toast on an uncoded failure", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
					member({ id: "3", name: "hotel-3" }),
				]),
			),
			http.delete(
				"/api/members/1",
				() => new HttpResponse("boom", { status: 500 }),
			),
		);
		renderPage();
		await screen.findByText("hotel-1");

		const row = screen.getByText("hotel-1").closest("tr") as HTMLElement;
		await userEvent.click(
			within(row).getByRole("button", { name: /^Remove$/i }),
		);
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);

		expect(
			await screen.findByText(/Something went wrong/i),
		).toBeInTheDocument();
	});
});
