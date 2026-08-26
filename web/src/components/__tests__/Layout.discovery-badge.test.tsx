import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import type userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockSystemStats } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

/**
 * Unrolls the first provider pill and one of its bucket lines.
 *
 * The claims accordion mounts model rows ONLY while their bucket is open, so any
 * assertion about a row has to ask for it. Defaults to `gone`, which is where the
 * per-row Dismiss control lives.
 */
async function openFirstBucket(
	user: ReturnType<typeof userEvent.setup>,
	bucket: "gone" | "stale" | "suspect" | "retired" | "pinned" = "gone",
	nth = 0,
) {
	await user.click(
		(await screen.findAllByTestId("discrepancy-provider-pill"))[nth],
	);
	// Scoped to the opened provider's section: only one provider is unrolled at a
	// time, so a document-wide query would still be ambiguous while two are listed.
	const section = screen.getAllByTestId("discrepancy-provider")[nth];
	await user.click(
		section.querySelector(
			`[data-testid='discrepancy-group-${bucket}-toggle']`,
		) as HTMLElement,
	);
}

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
		// Auth is the readable mh_csrf cookie (seeded once in setup.ts). The logout
		// tests clear it, so re-seed before every test to keep the suite
		// order-independent.
		document.cookie = "mh_csrf=test-csrf; path=/";
	});

	describe("Discovery Discrepancies Badge", () => {
		const claim = (model_id: string) => ({
			model_id,
			state: "gone",
			last_seen_at: "2026-07-01T00:00:00Z",
			missing_scans: 3,
			flap_window: 0,
			flap_since_review: 0,
		});

		const providerClaims = (
			provider_id: string,
			provider_name: string,
			gone: ReturnType<typeof claim>[],
		) => ({
			provider_id,
			provider_name,
			gone,
			stale: [],
			suspect: [],
			retired: [],
		});

		const status = (over: Record<string, unknown> = {}) => ({
			claims: [],
			group_claims: [],
			informational: [],
			claim_count: 0,
			informational_unseen: 0,
			...over,
		});

		const infoEntry = {
			provider_id: "p1",
			provider_name: "One",
			source: "background",
			detected_at: "2026-07-25T00:00:00Z",
			diff: { added: [{ model_id: "brand-new", reason: "new model" }] },
		};

		it("shows the claim count on the Models badge", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status({ claim_count: 3 })),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			expect(badge).toHaveAttribute("data-variant", "count");
			expect(badge).toHaveTextContent("3");
			expect(badge.getAttribute("aria-label")).not.toMatch(/^layout\.nav\./);
		});

		it("shows a dot rather than a number when only informational news is unseen", async () => {
			// The badge means "things that might be wrong". A price move is news, not
			// a problem, so it gets attention once without ever showing a count.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status({ informational_unseen: 4 })),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			expect(badge).toHaveAttribute("data-variant", "dot");
			expect(badge.textContent).toBe("");
			// With no text, the accessible name is the control's ONLY affordance, so
			// an unresolved key here is not cosmetic. Matched on the key prefix
			// rather than on the copy, to stay locale-independent.
			expect(badge.getAttribute("aria-label")).not.toMatch(/^layout\.nav\./);
		});

		it("explains what the dot means instead of leaving it an unlabelled mark", async () => {
			// The dot fires on unseen informational news while the modal's "Recent
			// changes" header shows every entry in that zone. Both numbers are right
			// and they mean different things, so the dot has to say which one it is.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status({ informational_unseen: 31 })),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			expect(badge).toHaveAttribute("data-variant", "dot");
			const label = badge.getAttribute("aria-label") ?? "";
			// Key prefix, not copy: this has to hold in all 29 locales.
			expect(label).not.toMatch(/^layout\.nav\./);
			// It names the UNSEEN count, the number the dot is actually triggered
			// by, which is what makes it legible next to the zone's total.
			expect(label).toContain("31");
			// A sighted user reads the tooltip and a screen-reader user hears the
			// accessible name. If the two ever diverge the control says two
			// different things about itself.
			expect(badge.getAttribute("title")).toBe(label);
		});

		it("hides the badge when there is nothing at all", async () => {
			let fetches = 0;
			server.use(
				http.get("/api/discovery/status", () => {
					fetches++;
					return HttpResponse.json(status());
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Wait for a real answer first, so this cannot pass merely because the
			// query had not resolved yet.
			await waitFor(() => expect(fetches).toBeGreaterThanOrEqual(1));
			await screen.findByRole("navigation");
			expect(screen.queryByTestId("discovery-status-badge")).toBeNull();
		});

		it("never stamps the review marker from the badge poll", async () => {
			// ?review=1 rebaselines "since your last visit" server-side. A poll doing
			// that would hold every flap count at zero forever.
			const urls: string[] = [];
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					urls.push(request.url);
					return HttpResponse.json(status({ claim_count: 1 }));
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await screen.findByTestId("discovery-status-badge");
			expect(urls.length).toBeGreaterThan(0);
			expect(urls.some((u) => u.includes("review=1"))).toBe(false);
		});

		it("renders the failover group claims that the badge count includes", async () => {
			// claim_count counts discovery-disabled failover groups. If the modal
			// cannot show them the badge points at rows that do not exist, which is
			// the badge-that-lies defect this rework exists to remove.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							group_claims: [
								{
									display_model: "gpt-oss-120b",
									member_count: 3,
									routable_count: 1,
									disabled_at: "2026-07-20T00:00:00Z",
								},
							],
						}),
					),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));

			const rows = await screen.findAllByTestId("discrepancy-group-claim");
			expect(rows).toHaveLength(1);
			expect(rows[0]).toHaveAttribute("data-display-model", "gpt-oss-120b");
			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
		});

		it("refetches and re-stamps the review marker on a second open", async () => {
			// The hook keys its fetch on a counter that only advances on an open
			// transition, which works only while the hook stays mounted. If Layout
			// ever unmounts it on close the counter resets, the first key is reused,
			// the cache answers, and both bugs return: stale rows on reopen and a
			// review stamp that stops firing per open.
			let reviewStamps = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					const review =
						new URL(request.url).searchParams.get("review") === "1";
					if (review) reviewStamps++;
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [
								providerClaims("p1", "NanoGPT", [
									claim(reviewStamps <= 1 ? "first-open" : "second-open"),
								]),
							],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"first-open",
			);
			await waitFor(() => expect(reviewStamps).toBe(1));

			// Escape, not the close button: that control is labelled with a
			// translated string and this suite stays locale-independent.
			fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			// The reopen starts fully collapsed, so the row has to be asked for again.
			// A cache replay would put the first open's row back and never reach the
			// server, so this pins both halves at once: a real refetch, and a real
			// second stamp.
			await openFirstBucket(user);
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-model-id",
					"second-open",
				),
			);
			expect(reviewStamps).toBe(2);
		});

		it("stops Retest all after the provider already in flight", async () => {
			const discovered: string[] = [];
			let release: (() => void) | undefined;
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
							],
						}),
					),
				),
				http.post("/api/providers/:id/discover", async ({ params }) => {
					discovered.push(String(params.id));
					await new Promise<void>((resolve) => {
						release = resolve;
					});
					return HttpResponse.json({ discovered: 0, diff: {} });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));

			// p1's discovery run is out; Cancel has taken the Retest all slot.
			await waitFor(() => expect(discovered).toEqual(["p1"]));
			expect(screen.queryByTestId("discrepancy-retest-all")).toBeNull();
			await user.click(
				await screen.findByTestId("discrepancy-retest-all-cancel"),
			);

			// Cancel must not abort p1: a half-applied discovery run is worse than a
			// slow one, so the walk only declines to start p2.
			release?.();
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			expect(discovered).toEqual(["p1"]);
			// The walk stopped early, so it must not sign off as a completed run.
			// Asserted on the toast's type rather than its text, to stay
			// locale-independent: a success toast here is the "done: 1" bug.
			expect(await screen.findByTestId("toast")).toHaveAttribute(
				"data-toast-type",
				"info",
			);
		});

		it("reports a Retest all walk once, not once per provider", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 3,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
								providerClaims("p3", "Three", [claim("c")]),
							],
						}),
					),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));

			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			// Three providers, one toast. ToastContext dedupes by message and each
			// per-provider message names a different provider, so nothing would
			// collapse them if the walk did not silence them.
			expect(screen.getAllByTestId("toast")).toHaveLength(1);
		});

		// The mixed-fleet case, which is where gating only the BUTTON was not enough.
		// With one provider that needs retesting the button renders, and the walk
		// then visited every provider with anything pending — including ones whose
		// own pill sits disabled saying a retest proves nothing. Each of those is a
		// slow upstream call that cannot change the answer.
		it("skips retired-only providers in a Retest all walk", async () => {
			const discovered: string[] = [];
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								// Nothing gone, one retirement: discovery has nothing to
								// learn here, the provider still lists the model.
								{
									...providerClaims("p2", "Two", []),
									retired: [
										{
											...claim("dead"),
											state: "retired",
											retired_at: "2026-07-28T00:00:00Z",
										},
									],
								},
							],
						}),
					),
				),
				http.post("/api/providers/:id/discover", async ({ params }) => {
					discovered.push(String(params.id));
					return HttpResponse.json({ discovered: 0, diff: {} });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));

			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			// The assertion the previous test could not make: what the walk actually
			// requested, not whether a button was on screen.
			expect(discovered).toStrictEqual(["p1"]);
		});

		it("dismisses one model per request", async () => {
			// Exactly one model per request: the endpoint 200s with a short `updated`
			// for a mixed list and only 404s when NOTHING matched, so one at a time
			// makes `updated: 0` an unambiguous failure for this model.
			//
			// No undo is offered. A dismissal self-heals: models.Upsert nulls
			// discovery_dismissed_at on any sighting, so the next discovery run brings
			// back anything that actually came back.
			const bodies: { model_ids: string[] }[] = [];
			// listClaimRows filters on discovery_dismissed_at IS NULL, so a dismissed
			// model stops being reported. The mock has to do the same or the refresh
			// would rebuild the row as pending and the status assertion below would be
			// testing the mock rather than the merge.
			const dismissed = new Set<string>();
			server.use(
				http.get("/api/discovery/status", () => {
					const gone = ["a"]
						.filter((m) => !dismissed.has(m))
						.map((m) => claim(m));
					return HttpResponse.json(
						status({
							claim_count: gone.length,
							claims: [providerClaims("p1", "One", gone)],
						}),
					);
				}),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					bodies.push(body);
					for (const m of body.model_ids) dismissed.add(m);
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			const rowA = () =>
				screen
					.getAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "a");
			await waitFor(() => expect(rowA()).toBeTruthy());
			await user.click(
				rowA()?.querySelector(
					'[data-testid="discrepancy-dismiss"]',
				) as HTMLElement,
			);

			// Struck through in place rather than removed: the row that vanishes on a
			// click is the operator complaint this rework exists to fix. `dismissed`,
			// not `resolved`: the cleared summary reports the latter as "is listed
			// again", which is false for a model the operator retired by hand.
			await waitFor(() =>
				expect(rowA()).toHaveAttribute("data-status", "dismissed"),
			);
			expect(bodies).toHaveLength(1);
			expect(bodies[0].model_ids).toEqual(["a"]);
			expect(screen.queryByTestId("toast-action")).toBeNull();
		});

		it("unpins one model per request and keeps the row in place", async () => {
			// An unpinned model leaves /api/discovery/status entirely: unpin clears
			// the pin AND resets the miss streak, so there is no claim left to
			// report. The mock has to do the same, or the assertion below would be
			// testing the mock rather than the merge.
			const bodies: { provider_id: string; model_ids: string[] }[] = [];
			const unpinned = new Set<string>();
			server.use(
				http.get("/api/discovery/status", () => {
					const pinned = ["k"]
						.filter((m) => !unpinned.has(m))
						.map((m) => ({
							...claim(m),
							state: "pinned",
							pinned_at: "2026-07-15T00:00:00Z",
						}));
					return HttpResponse.json(
						status({
							// Pinned rows are deliberately not counted, so the badge is
							// carried by the journal dot instead: the modal still has
							// something to show while the number stays at zero.
							claim_count: 0,
							informational: [infoEntry],
							informational_unseen: 1,
							claims: [{ ...providerClaims("p1", "One", []), pinned }],
						}),
					);
				}),
				http.post("/api/discovery/unpin", async ({ request }) => {
					const body = (await request.json()) as {
						provider_id: string;
						model_ids: string[];
					};
					bodies.push(body);
					for (const m of body.model_ids) unpinned.add(m);
					// No `updated` key: the endpoint names the rows it cleared.
					return HttpResponse.json({ unpinned: body.model_ids });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user, "pinned");
			const rowK = () =>
				screen
					.getAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "k");
			await waitFor(() => expect(rowK()).toBeTruthy());
			await user.click(
				rowK()?.querySelector(
					'[data-testid="discrepancy-unpin"]',
				) as HTMLElement,
			);

			await waitFor(() => expect(bodies).toHaveLength(1));
			expect(bodies[0]).toEqual({ provider_id: "p1", model_ids: ["k"] });
			// Struck through where it sat rather than vanishing: the row is absent
			// from the refetch because the operator cleared it, which is the same
			// shape as a dismissal and must never read as "listed again".
			await waitFor(() =>
				expect(rowK()).toHaveAttribute("data-status", "dismissed"),
			);
		});

		// One pinned model and nothing else: neither counter moves, because a pin
		// is never counted and is not informational news either.
		const pinOnlyStatus = () =>
			status({
				claim_count: 0,
				informational_unseen: 0,
				claims: [
					{
						...providerClaims("p1", "One", []),
						pinned: [
							{
								...claim("k"),
								state: "pinned",
								pinned_at: "2026-07-15T00:00:00Z",
							},
						],
					},
				],
			});

		it("opens the modal when a pin is the only thing to report", async () => {
			// The badge is the ONLY way into the modal, so keying it on the two
			// counters alone strands the pinned bucket and its Unpin control behind a
			// badge that never renders: a forgotten pin could then neither be found nor
			// undone from the dashboard.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(pinOnlyStatus()),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			// A pin is a decision, not a problem, so it must never produce a count.
			expect(badge).toHaveAttribute("data-variant", "dot");
			expect(badge.textContent).toBe("");
			const label = badge.getAttribute("aria-label") ?? "";
			// Key prefix, not copy: this has to hold in all 29 locales.
			expect(label).not.toMatch(/^layout\.nav\./);
			// The news label is counted, so borrowing it here would announce the dot as
			// "0 unreviewed changes" to a screen-reader user.
			expect(label).not.toContain("0");
			expect(badge.getAttribute("title")).toBe(label);

			await user.click(badge);
			await openFirstBucket(user, "pinned");
			const row = await screen.findByTestId("discrepancy-claim");
			expect(row).toHaveAttribute("data-state", "pinned");
			expect(row).toHaveAttribute("data-model-id", "k");
		});

		it("takes Unpin away on a managed fleet member", async () => {
			// A pin is synced config. On a managed member the primary's list is
			// re-applied on the next sync pass, so a local unpin would look like it
			// worked and silently come back.
			server.use(
				http.get("/api/system", () =>
					HttpResponse.json({
						...mockSystemStats,
						fleet: { state: "member", is_primary: false },
					}),
				),
				http.get("/api/discovery/status", () =>
					HttpResponse.json(pinOnlyStatus()),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user, "pinned");
			// Disabled rather than hidden, so it does not read as a missing feature.
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-unpin")).toBeDisabled(),
			);
		});

		it("re-reads the rows and the badge after an unpin that reports failure", async () => {
			// A rejected request does not prove the write did not land: the server can
			// commit and the response be lost. The badge is the half that matters most
			// here, because a pinned row is what keeps it lit when nothing is counted,
			// so a landed unpin has to be able to put it out.
			let cleared = false;
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(cleared ? status() : pinOnlyStatus()),
				),
				http.post("/api/discovery/unpin", () => {
					cleared = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user, "pinned");
			await user.click(await screen.findByTestId("discrepancy-unpin"));
			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "error"),
				).toBe(true),
			);

			// Nothing is marked before the response, so the re-read is what takes the
			// row's control away once the pin has left the payload.
			await waitFor(() =>
				expect(
					screen
						.getByTestId("discrepancy-claim")
						.querySelector("[data-testid='discrepancy-unpin']"),
				).toBeNull(),
			);
			await expectBadgeAfterClose(null);
		});

		it("dismisses a whole provider in one request", async () => {
			// ONE request carrying every id, not one request per model. No undo: a
			// dismissal is reversed by discovery sighting the model again.
			const bodies: { model_ids: string[] }[] = [];
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [providerClaims("p1", "One", [claim("a"), claim("b")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					bodies.push(body);
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-dismiss-all"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-all-confirm"),
			);

			await waitFor(() => expect(bodies).toHaveLength(1));
			expect(bodies[0].model_ids.sort()).toEqual(["a", "b"]);
			expect(screen.queryByTestId("toast-action")).toBeNull();
		});

		it("warns when a provider batch comes back short", async () => {
			// A short `updated` cannot say WHICH ids it missed, so the toast reports the
			// shortfall and the refresh underneath is what corrects the rows.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [providerClaims("p1", "One", [claim("a"), claim("b")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					// One of the two took, and the response names it.
					HttpResponse.json({ dismissed: ["a"], updated: 1 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-dismiss-all"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-all-confirm"),
			);

			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "warning"),
				).toBe(true),
			);
		});

		it("rolls a provider batch back when its request fails", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [providerClaims("p1", "One", [claim("a"), claim("b")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					HttpResponse.json({ error: "boom" }, { status: 500 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-dismiss-all"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-all-confirm"),
			);

			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "error"),
				).toBe(true),
			);
			// Rolled back, not left struck through: the write never landed.
			await openFirstBucket(user);
			for (const row of screen.getAllByTestId("discrepancy-claim")) {
				expect(row).toHaveAttribute("data-status", "pending");
			}
		});

		it("keeps the providers that succeeded when one batch of a modal-wide dismiss fails", async () => {
			// allSettled, not all: one provider failing must neither abandon the others
			// nor roll them back. Only the failed provider's rows return to pending.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
							],
						}),
					),
				),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					if (body.model_ids.includes("b")) {
						return HttpResponse.json({ error: "boom" }, { status: 500 });
					}
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything"),
			);
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything-confirm"),
			);

			// p1 landed, so a success-shaped toast is raised.
			await waitFor(() =>
				expect(screen.queryAllByTestId("toast").length).toBeGreaterThan(0),
			);
			const row = (id: string) =>
				screen
					.queryAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === id);
			await openFirstBucket(user, "gone", 1);
			await waitFor(() => expect(row("b")).toBeTruthy());
			// b's provider failed, so b is back to pending rather than struck through.
			expect(row("b")).toHaveAttribute("data-status", "pending");
		});

		it("reports outright failure when no batch of a modal-wide dismiss lands", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					HttpResponse.json({ error: "boom" }, { status: 500 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything"),
			);
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything-confirm"),
			);

			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "error"),
				).toBe(true),
			);
			// Nothing landed, so no Undo is offered: there is nothing to undo.
			expect(screen.queryByTestId("toast-action")).toBeNull();
		});
		it("surfaces the failure when the reconciling refresh after a dismissal fails", async () => {
			// A short `updated` cannot say which ids the server skipped, so only a
			// successful refresh reconciles them. When that refresh fails the rows keep
			// their optimistic `dismissed` state, which over-claims. What must NOT
			// happen is that going unreported: the operator has to be able to tell that
			// the list is unconfirmed rather than clean.
			//
			// The guarantee is the modal's refresh-error banner, which is its existing
			// vocabulary for "we could not find out". A rollback was tried instead and
			// removed: revertDismissal compares on optimisticFrom, which any merge
			// strips, so a concurrent successful refresh silently made it a no-op.
			let statusCalls = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					statusCalls++;
					const isReview =
						new URL(request.url).searchParams.get("review") === "1";
					// The modal's own opening fetch succeeds; the reconciling refresh
					// after the dismiss is the one that fails.
					if (!isReview && statusCalls > 2) {
						return HttpResponse.json({ error: "down" }, { status: 500 });
					}
					return HttpResponse.json(
						status({
							claim_count: 2,
							claims: [providerClaims("p1", "One", [claim("a"), claim("b")])],
						}),
					);
				}),
				// Two requested, one applied: membership unknown.
				http.post("/api/discovery/dismiss", () =>
					// One of the two took, and the response names it.
					HttpResponse.json({ dismissed: ["a"], updated: 1 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-dismiss-all"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-all-confirm"),
			);

			// The shortfall is named...
			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "warning"),
				).toBe(true),
			);
			// ...the failure is visible as a live region...
			const banner = await screen.findByTestId("discrepancy-load-error");
			expect(banner).toHaveAttribute("role", "alert");

			// ...and the rows land exactly as the response described them. The server
			// named `a`, so `a` is dismissed and `b` is untouched. Marking both would
			// strike through a model the server skipped and make providerHasNoPending
			// true, swapping Retest and Dismiss all for the Clean broom; marking
			// neither would let the merge read `a`'s absence as "listed again".
			expect(screen.queryByTestId("discrepancy-clean")).toBeNull();
			expect(screen.getByTestId("discrepancy-retest")).toBeInTheDocument();
			expect(screen.getByTestId("discrepancy-dismiss-all")).toBeInTheDocument();
			await openFirstBucket(user);
			const statusOf = (id: string) =>
				screen
					.queryAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === id)
					?.getAttribute("data-status");
			expect(statusOf("a")).toBe("dismissed");
			expect(statusOf("b")).toBe("pending");
		});

		it("dismisses every provider at once, one request per provider", async () => {
			// The endpoint is provider-scoped, so a modal-wide dismiss is N requests.
			const bodies: { model_ids: string[] }[] = [];
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
							],
						}),
					),
				),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					bodies.push(body);
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything"),
			);
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything-confirm"),
			);

			await waitFor(() => expect(bodies).toHaveLength(2));
			expect(bodies.flatMap((b) => b.model_ids).sort()).toEqual(["a", "b"]);
		});

		/**
		 * A status endpoint whose `claim_count` actually falls as models are
		 * dismissed, the way listClaimRows does (it filters on
		 * discovery_dismissed_at IS NULL).
		 *
		 * A fixed payload cannot test the badge at all: the number it shows would
		 * be right by accident whether or not anything re-read it.
		 */
		function serveDismissable(providers: [string, string, string[]][]) {
			const dismissed = new Set<string>();
			server.use(
				http.get("/api/discovery/status", () => {
					const live = providers
						.map(([id, name, models]) =>
							providerClaims(
								id,
								name,
								models.filter((m) => !dismissed.has(m)).map(claim),
							),
						)
						.filter((p) => p.gone.length > 0);
					return HttpResponse.json(
						status({
							claim_count: live.reduce((n, p) => n + p.gone.length, 0),
							claims: live,
						}),
					);
				}),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					for (const m of body.model_ids) dismissed.add(m);
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			return dismissed;
		}

		/**
		 * Escape, not the close button: that control is labelled with a translated
		 * string and this suite stays locale-independent.
		 *
		 * Addressed through the modal's own testid rather than by role, because a
		 * ConfirmDialog is a SIBLING dialog and lingers through its fade-out.
		 */
		async function closeDiscrepancyModal() {
			const dialog = screen
				.getByTestId("discrepancy-modal")
				.closest('[role="dialog"]') as HTMLElement;
			fireEvent.keyDown(dialog, { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);
		}

		/**
		 * The badge is hidden while the modal is open, so what it says is only
		 * observable after a close — which is exactly where the operator reads it,
		 * and exactly where it used to lie.
		 *
		 * `null` means the badge is gone entirely. Every caller below asserts a
		 * value the STALE poll response would not produce, so none of them can pass
		 * on a badge that was never re-read.
		 */
		async function expectBadgeAfterClose(text: string | null) {
			await closeDiscrepancyModal();
			await waitFor(() => {
				const badge = screen.queryByTestId("discovery-status-badge");
				if (text === null) expect(badge).toBeNull();
				else expect(badge).toHaveTextContent(text);
			});
		}

		it("re-reads the badge after a single dismissal", async () => {
			// The modal's own refresh writes to the modal's snapshot alone: a
			// different query key, fetched straight through the api client. Nothing
			// it does reaches the nav badge, which otherwise keeps showing whatever
			// the 60s poll last saw.
			const dismissed = serveDismissable([["p1", "One", ["a", "b"]]]);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(
				await screen.findByTestId("discovery-status-badge"),
			).toHaveTextContent("2");
			await user.click(screen.getByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			await user.click(
				screen
					.getAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "a")
					?.querySelector('[data-testid="discrepancy-dismiss"]') as HTMLElement,
			);
			await waitFor(() => expect(dismissed.size).toBe(1));

			await expectBadgeAfterClose("1");
		});

		it("still closes on Escape after a dismissal has unmounted the focused button", async () => {
			// The row's Dismiss button holds focus when it is clicked, and
			// dismissing unmounts it, so the browser hands focus back to <body>.
			// A key pressed from there does not originate inside the dialog, which
			// is why Escape is handled on the document: scoped to the dialog node
			// it went unheard and the modal could no longer be closed by keyboard.
			const dismissed = serveDismissable([["p1", "One", ["a", "b"]]]);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			await user.click(
				(await screen.findAllByTestId("discrepancy-dismiss"))[0],
			);
			await waitFor(() => expect(dismissed.size).toBe(1));

			// Dispatched on the body rather than the dialog: addressing the dialog
			// would sidestep the very thing that broke.
			fireEvent.keyDown(document.body, { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);
		});

		it("re-reads the badge after a dismissal that reports failure", async () => {
			// A rejected request does not prove the write did not land: the server can
			// commit and the response be lost. The row correctly stays actionable and
			// the toast reports the failure, but the badge must not keep asserting a
			// count it can no longer back.
			let dismissed = false;
			server.use(
				http.get("/api/discovery/status", () => {
					const gone = dismissed ? [] : [claim("a")];
					return HttpResponse.json(
						status({
							claim_count: gone.length,
							claims: gone.length ? [providerClaims("p1", "One", gone)] : [],
						}),
					);
				}),
				http.post("/api/discovery/dismiss", () => {
					dismissed = true;
					return HttpResponse.json({ error: "boom" }, { status: 500 });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			await user.click(await screen.findByTestId("discrepancy-dismiss"));
			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "error"),
				).toBe(true),
			);

			await expectBadgeAfterClose(null);
		});

		it("re-reads the badge after a provider-wide dismiss", async () => {
			const dismissed = serveDismissable([
				["p1", "One", ["a", "b"]],
				["p2", "Two", ["c"]],
			]);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(
				(await screen.findAllByTestId("discrepancy-dismiss-all"))[0],
			);
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-all-confirm"),
			);
			// ConfirmDialog confirms through the modal's fade-out, so the request is
			// a timer away from the click, not a microtask.
			await waitFor(() => expect(dismissed.size).toBe(2));

			// The other provider's claim survives, so this pins a re-read rather than
			// a badge that merely happens to be empty.
			await expectBadgeAfterClose("1");
		});

		it("clears the badge when a modal-wide dismiss empties it", async () => {
			// The reported sequence: dismiss everything, close, and find the badge
			// still lit with a count for claims the server no longer has.
			const dismissed = serveDismissable([
				["p1", "One", ["a"]],
				["p2", "Two", ["b"]],
			]);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything"),
			);
			await user.click(
				await screen.findByTestId("discrepancy-dismiss-everything-confirm"),
			);
			await waitFor(() => expect(dismissed.size).toBe(2));

			await expectBadgeAfterClose(null);
		});

		it("re-reads the badge after a single provider retest", async () => {
			// A retest clears a claim by changing what the server reports, which
			// leaves the badge exactly as stale as a dismissal does.
			const gone = new Set(["a", "b"]);
			server.use(
				http.get("/api/discovery/status", () => {
					const rows = [...gone].map(claim);
					return HttpResponse.json(
						status({
							claim_count: rows.length,
							claims: rows.length ? [providerClaims("p1", "One", rows)] : [],
						}),
					);
				}),
				http.post("/api/providers/:id/discover", () => {
					gone.delete("a");
					return HttpResponse.json({ discovered: 1, diff: {} });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest"));
			await waitFor(() => expect(gone.has("a")).toBe(false));

			await expectBadgeAfterClose("1");
		});

		it("re-reads the badge once a Retest all walk has finished", async () => {
			// The walk silences the per-provider refresh and reports once at the end,
			// so the badge has to be re-read there or not at all.
			const gone = new Map([
				["p1", "a"],
				["p2", "b"],
			]);
			server.use(
				http.get("/api/discovery/status", () => {
					const live = [...gone].map(([id, model]) =>
						providerClaims(id, id === "p1" ? "One" : "Two", [claim(model)]),
					);
					return HttpResponse.json(
						status({ claim_count: live.length, claims: live }),
					);
				}),
				http.post("/api/providers/:id/discover", ({ params }) => {
					gone.delete(String(params.id));
					return HttpResponse.json({ discovered: 1, diff: {} });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			expect(gone.size).toBe(0);

			await expectBadgeAfterClose(null);
		});

		it("treats updated: 0 as a failed dismissal", async () => {
			// The endpoint 200s with a short `updated` and only 404s when nothing at
			// all matched, so HTTP status alone would report a phantom success.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					HttpResponse.json({ updated: 0 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			await user.click(await screen.findByTestId("discrepancy-dismiss"));

			// No confirmation toast, so no Undo control: the success path is the only
			// path that renders one.
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-status",
					"pending",
				),
			);
			expect(screen.queryByTestId("toast-action")).toBeNull();
		});

		it("discards a stale status read instead of un-dismissing a row", async () => {
			// Retest and dismiss each refresh, so their status reads overlap. A read
			// issued BEFORE a dismissal still reports the model, and if it lands after
			// the write the merge would rebuild that row as pending, handing back
			// controls for a model that is gone.
			//
			// Retest is what puts a read in flight here: its refresh is held open, the
			// dismissal completes underneath it, and only then is it released. The held
			// payload is built BEFORE the await so it is genuinely stale.
			let releaseStale: (() => void) | undefined;
			const gate = new Promise<void>((resolve) => {
				releaseStale = resolve;
			});
			let discovered = false;
			let heldOne = false;
			const dismissed = new Set<string>();
			server.use(
				http.get("/api/discovery/status", async ({ request }) => {
					const isReview =
						new URL(request.url).searchParams.get("review") === "1";
					const gone = ["a"]
						.filter((m) => !dismissed.has(m))
						.map((m) => claim(m));
					const payload = status({
						claim_count: gone.length,
						claims: [providerClaims("p1", "One", gone)],
					});
					// The retest's own refresh: snapshot taken above, then held.
					if (!isReview && discovered && !heldOne) {
						heldOne = true;
						await gate;
					}
					return HttpResponse.json(payload);
				}),
				http.post("/api/providers/:id/discover", () => {
					discovered = true;
					return HttpResponse.json({ discovered: 0, diff: {} });
				}),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as { model_ids: string[] };
					for (const m of body.model_ids) dismissed.add(m);
					return HttpResponse.json({
						dismissed: body.model_ids,
						updated: body.model_ids.length,
					});
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			const rowA = () =>
				screen
					.queryAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "a");
			await waitFor(() => expect(rowA()).toBeTruthy());

			// Retest puts a status read in flight and leaves it there.
			await user.click(screen.getByTestId("discrepancy-retest"));
			await waitFor(() => expect(heldOne).toBe(true));

			// Dismiss lands while that read is still out.
			await user.click(
				rowA()?.querySelector(
					'[data-testid="discrepancy-dismiss"]',
				) as HTMLElement,
			);
			await waitFor(() =>
				expect(rowA()).toHaveAttribute("data-status", "dismissed"),
			);

			// The stale read returns, still listing `a` as gone. It must be discarded.
			releaseStale?.();
			await waitFor(() => expect(heldOne).toBe(true));
			await new Promise((r) => setTimeout(r, 50));
			expect(rowA()).toHaveAttribute("data-status", "dismissed");
		});

		it("leaves a failed dismissal actionable and says so", async () => {
			// Nothing is marked before the server confirms, so a failed dismiss has
			// nothing to roll back: the row simply never left `pending`. What must
			// happen is that the failure is reported rather than swallowed.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					HttpResponse.json({ error: "boom" }, { status: 500 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			const rowA = () =>
				screen
					.getAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "a");
			await waitFor(() => expect(rowA()).toBeTruthy());
			await user.click(
				rowA()?.querySelector(
					'[data-testid="discrepancy-dismiss"]',
				) as HTMLElement,
			);

			await waitFor(() =>
				expect(
					screen
						.getAllByTestId("toast")
						.some((el) => el.getAttribute("data-toast-type") === "error"),
				).toBe(true),
			);
			expect(rowA()).toHaveAttribute("data-status", "pending");
			expect(
				rowA()?.querySelector('[data-testid="discrepancy-dismiss"]'),
			).not.toBeNull();
		});

		it("acknowledges the journal once it is expanded, without re-stamping review", async () => {
			let acks = 0;
			let reviewStamps = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						reviewStamps++;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
							informational: [infoEntry],
							informational_unseen: 1,
						}),
					);
				}),
				http.post("/api/discovery/changes/ack", () => {
					acks++;
					return HttpResponse.json({ entries: [], count: 0 });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			const toggle = await screen.findByTestId(
				"discrepancy-informational-toggle",
			);
			// Opening must NOT ack. The destructive ack-on-open is what let the badge
			// clear while the problem was still outstanding.
			expect(acks).toBe(0);
			await waitFor(() => expect(reviewStamps).toBe(1));

			await user.click(toggle);
			await waitFor(() => expect(acks).toBe(1));

			// The ack's follow-up invalidation must be `exact`. Query keys match by
			// prefix, so a non-exact invalidate of ["discovery-status"] also refetches
			// the modal's ["discovery-status","modal",n] query, which fetches with
			// review=1 and moves the server's "since your last visit" baseline to now
			// — silently zeroing every flap count for the next visit, on a routine
			// click rather than a timer.
			await waitFor(() => expect(acks).toBe(1));
			expect(reviewStamps).toBe(1);
		});

		it("does not re-stamp review when an SSE event lands while the modal is open", async () => {
			// Same prefix-matching hazard as the ack path, on the listener that fires
			// exactly when flap counts have just moved.
			let reviewStamps = 0;
			let polls = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						reviewStamps++;
					} else {
						polls++;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			await screen.findByTestId("discrepancy-claim");
			await waitFor(() => expect(reviewStamps).toBe(1));
			const pollsBefore = polls;

			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: { type: "discovery.changes_pending" },
					}),
				);
			});

			// The badge poll must refetch (that is the point of the listener) while
			// the modal's review query must not.
			await waitFor(() => expect(polls).toBeGreaterThan(pollsBefore));
			expect(reviewStamps).toBe(1);
		});

		it("shows a failure state instead of the empty state when the fetch fails", async () => {
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					// The poll succeeds so the badge renders; only the modal's own
					// review fetch fails.
					if (new URL(request.url).searchParams.get("review") === "1") {
						return HttpResponse.json({ error: "boom" }, { status: 500 });
					}
					return HttpResponse.json(status({ claim_count: 2 }));
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));

			expect(
				await screen.findByTestId("discrepancy-load-error"),
			).toBeInTheDocument();
			// "Nothing is wrong" when we could not find out is the same false
			// reassurance this rework exists to remove.
			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
		});

		it("shows a loading state instead of the empty state while the fetch is out", async () => {
			// One step earlier than the failure case above: the operator clicked a
			// badge reading 1, and until the answer lands the modal knows nothing.
			// Telling them there are no discrepancies in that window is the same
			// false reassurance, just shorter-lived.
			let release: (() => void) | undefined;
			const gate = new Promise<void>((resolve) => {
				release = resolve;
			});
			let modalFetches = 0;
			server.use(
				http.get("/api/discovery/status", async ({ request }) => {
					// Only the modal's review fetch is held; the badge poll answers
					// immediately so the badge is there to click.
					if (new URL(request.url).searchParams.get("review") === "1") {
						modalFetches++;
						await gate;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "NanoGPT", [claim("a")])],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await screen.findByTestId("discrepancy-modal");
			await waitFor(() => expect(modalFetches).toBe(1));

			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
			expect(screen.getByTestId("discrepancy-loading")).toBeInTheDocument();

			release?.();
			await screen.findByTestId("discrepancy-provider-pill");
			await openFirstBucket(user);
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"a",
			);
			// And the loading line is a state, not a permanent header.
			expect(screen.queryByTestId("discrepancy-loading")).toBeNull();
		});

		it("does not paint the previous session's rows when reopened", async () => {
			// Close clears what the last visit collected. Without that the second
			// open renders the first open's snapshot until its own fetch lands:
			// struck-through resolved rows and already-dismissed models, presented
			// as the current state of the world.
			let opens = 0;
			let release: (() => void) | undefined;
			server.use(
				http.get("/api/discovery/status", async ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						opens++;
						if (opens === 2) {
							await new Promise<void>((resolve) => {
								release = resolve;
							});
						}
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [
								providerClaims("p1", "NanoGPT", [
									claim(opens <= 1 ? "first-open" : "second-open"),
								]),
							],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await openFirstBucket(user);
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"first-open",
			);

			// Escape, not the close button: that control is labelled with a
			// translated string and this suite stays locale-independent.
			fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await screen.findByTestId("discrepancy-modal");
			await waitFor(() => expect(opens).toBe(2));

			// The second open's fetch is still out, so there is nothing yet to show.
			// Asserted on the PILL, not on a row: rows are unmounted until a bucket is
			// opened, so a row-level check would pass here even if the previous
			// session's providers were still painted. The pill is the top-level render
			// of a claim, which makes its absence the real invariant.
			expect(screen.queryByTestId("discrepancy-provider-pill")).toBeNull();
			expect(screen.queryByTestId("discrepancy-claim")).toBeNull();
			expect(screen.getByTestId("discrepancy-loading")).toBeInTheDocument();

			release?.();
			await screen.findByTestId("discrepancy-provider-pill");
			await openFirstBucket(user);
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-model-id",
					"second-open",
				),
			);
		});

		it("refetches the badge on a discovery.changes_pending SSE event", async () => {
			let fetches = 0;
			server.use(
				http.get("/api/discovery/status", () => {
					fetches++;
					return HttpResponse.json(status());
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => expect(fetches).toBeGreaterThanOrEqual(1));
			const initial = fetches;

			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: { type: "discovery.changes_pending" },
					}),
				);
			});

			await waitFor(() => expect(fetches).toBeGreaterThan(initial));
		});
	});
});
