import {
	act,
	fireEvent,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { AuditEntry } from "../../../api/types";
import i18n from "../../../i18n";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Audit } from "../index";

// Scroll mode is a virtual table, and jsdom lays nothing out: give the scroll
// box (the only tabIndex=-1 element) and each row a height, so the virtualizer
// mounts rows the way a browser would.
function mockLayout() {
	const original = Object.getOwnPropertyDescriptor(
		HTMLElement.prototype,
		"offsetHeight",
	);
	Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
		configurable: true,
		get(this: HTMLElement) {
			if (this.getAttribute("tabindex") === "-1") return 600;
			if (this.tagName === "TR") return 45;
			return 0;
		},
	});
	return () => {
		if (original)
			Object.defineProperty(HTMLElement.prototype, "offsetHeight", original);
	};
}

/** The scroll box holding the rows. */
function scroller(rowText: string): HTMLElement {
	return screen.getByText(rowText).closest('[tabindex="-1"]') as HTMLElement;
}

/**
 * The footer status as a pattern: the start and end become digit groups, the
 * end a back-reference to the start when `sameEnds` is set.
 */
function rangePattern(total: number, sameEnds = false): RegExp {
	const text = i18n.t("common.showingRange", {
		start: "\u0000S",
		end: "\u0000E",
		total: String(total),
	});
	const escaped = text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	return new RegExp(
		`^${escaped.replace("\u0000S", "(\\d+)").replace("\u0000E", sameEnds ? "\\1" : "(\\d+)")}$`,
	);
}

const range = (start: number, end: number, total: number) =>
	i18n.t("common.showingRange", {
		start: String(start),
		end: String(end),
		total: String(total),
	});

function entry(overrides: Partial<AuditEntry>): AuditEntry {
	return {
		id: crypto.randomUUID(),
		created_at: "2026-07-03T10:00:00Z",
		actor: "admin",
		actor_role: "admin",
		method: "POST",
		route: "/providers",
		path: "/providers",
		status_code: 201,
		remote_addr: "10.0.0.1:1",
		...overrides,
	};
}

describe("Audit page", () => {
	let restoreLayout: () => void = () => {};
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		restoreLayout = mockLayout();
	});

	afterEach(() => {
		restoreLayout();
	});

	it("renders entries with actor, action, entity, remote address, and status", async () => {
		server.use(
			http.get("/api/audit", () =>
				HttpResponse.json({
					entries: [
						entry({
							actor: "alice",
							actor_role: "user",
							route: "/virtual-keys",
							remote_addr: "192.168.7.9:4242",
						}),
						entry({
							method: "DELETE",
							route: "/users/{id}",
							entity_id: "11111111-2222-4333-8444-555555555555",
							status_code: 204,
						}),
						entry({
							route: "/models/{id}/test",
							entity_id: "22222222-2222-4333-8444-555555555555",
							entity_name: "gpt-nice-name",
						}),
					],
					total: 3,
					has_more: false,
				}),
			),
		);
		renderWithProviders(<Audit />);

		expect(await screen.findByText("alice")).toBeInTheDocument();
		expect(screen.getByText("/virtual-keys")).toBeInTheDocument();
		expect(screen.getByText("/users/{id}")).toBeInTheDocument();
		expect(screen.getByText("DELETE")).toBeInTheDocument();
		expect(screen.getByText("204")).toBeInTheDocument();
		expect(screen.getByText("192.168.7.9:4242")).toBeInTheDocument();
		// Unresolved entity falls back to its full UUID (the cell clips it, the
		// markup does not); a resolved one shows its current display name instead.
		expect(
			screen.getByText("11111111-2222-4333-8444-555555555555"),
		).toBeInTheDocument();
		expect(screen.getByText("gpt-nice-name")).toBeInTheDocument();
		expect(
			screen.queryByText("22222222-2222-4333-8444-555555555555"),
		).not.toBeInTheDocument();
		// Regression pin: a row with no entity shows the shared "-" placeholder.
		const aliceRow = screen.getByText("alice").closest("tr") as HTMLElement;
		expect(within(aliceRow).getByText("-")).toBeInTheDocument();
	});

	it("opens the detail modal on row click", async () => {
		server.use(
			http.get("/api/audit", () =>
				HttpResponse.json({
					entries: [
						entry({
							actor: "alice",
							actor_role: "user",
							method: "DELETE",
							route: "/api/models/{id}",
							path: "/api/models/33333333-2222-4333-8444-555555555555",
							entity_id: "33333333-2222-4333-8444-555555555555",
							entity_name: "doomed-model",
							status_code: 204,
							remote_addr: "10.1.2.3:999",
						}),
					],
					total: 1,
					has_more: false,
				}),
			),
		);
		const { user } = renderWithProviders(<Audit />);

		await user.click(await screen.findByText("/api/models/{id}"));
		const dialog = await screen.findByRole("dialog");
		expect(dialog).toHaveTextContent("Audit Entry");
		// Full path and UUID appear in the modal; the table shows the route and
		// the resolved name.
		expect(dialog).toHaveTextContent(
			"/api/models/33333333-2222-4333-8444-555555555555",
		);
		expect(dialog).toHaveTextContent("doomed-model");
		expect(dialog).toHaveTextContent("10.1.2.3:999");

		await user.keyboard("{Escape}");
		await waitFor(() => {
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
		});
	});

	it("appends the next page when scrolled to the foot", async () => {
		server.use(
			http.get("/api/audit", ({ request }) => {
				// Scroll mode pages by keyset cursor, not offset, so inserts at the top
				// of this newest-first log never shift the window.
				const cursor = new URL(request.url).searchParams.get("cursor");
				if (cursor === "cur-1") {
					return HttpResponse.json({
						entries: [entry({ route: "/second-page" })],
						total: 2,
						has_more: false,
					});
				}
				return HttpResponse.json({
					entries: [entry({ route: "/first-page" })],
					total: 2,
					has_more: true,
					next_cursor: "cur-1",
				});
			}),
		);
		renderWithProviders(<Audit />);

		expect(await screen.findByText("/first-page")).toBeInTheDocument();
		// A scroll with the foot in view (jsdom: no scroll height) -> next page.
		fireEvent.scroll(scroller("/first-page"));
		expect(await screen.findByText("/second-page")).toBeInTheDocument();
		// The first page stays appended above the second.
		expect(screen.getByText("/first-page")).toBeInTheDocument();
	});

	it("names the rows on screen, not the rows loaded, and offers a way back up", async () => {
		server.use(
			http.get("/api/audit", () =>
				HttpResponse.json({
					entries: Array.from({ length: 30 }, (_, i) =>
						entry({ route: `/row-${i}` }),
					),
					total: 30,
					has_more: false,
				}),
			),
		);
		renderWithProviders(<Audit />);
		expect(await screen.findByText("/row-0")).toBeInTheDocument();

		// A 600px box of 45px rows holds far fewer than the 30 loaded.
		const status = screen.getByText(rangePattern(30));
		const [, first, last] =
			rangePattern(30).exec(status.textContent ?? "") ?? [];
		expect(first).toBe("1");
		const shown = Number(last);
		expect(shown).toBeGreaterThan(0);
		expect(shown).toBeLessThan(30);

		// More than one screen down, the back-to-top button appears.
		const box = scroller("/row-0");
		Object.defineProperty(box, "scrollHeight", {
			configurable: true,
			value: 1350,
		});
		Object.defineProperty(box, "clientHeight", {
			configurable: true,
			value: 600,
		});
		box.scrollTop = 700;
		fireEvent.scroll(box);
		const toTop = await screen.findByTestId("scroll-top-button");
		// Regression pin: the button precedes the scroller in DOM order, so the
		// keyboard reaches it before tabbing through every row.
		expect(
			toTop.compareDocumentPosition(box) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
	});

	it("does not fetch the next page on a scroll away from the foot", async () => {
		let requests = 0;
		server.use(
			http.get("/api/audit", ({ request }) => {
				requests += 1;
				const cursor = new URL(request.url).searchParams.get("cursor");
				return HttpResponse.json({
					entries: [entry({ route: cursor ? "/second-page" : "/first-page" })],
					total: 2,
					has_more: !cursor,
					next_cursor: cursor ? undefined : "cur-1",
				});
			}),
		);
		renderWithProviders(<Audit />);

		expect(await screen.findByText("/first-page")).toBeInTheDocument();
		const requestsAfterFirst = requests;
		// A long list scrolled near its top: the foot is out of reach, so the
		// scroll must not pull another page.
		const box = scroller("/first-page");
		Object.defineProperty(box, "scrollHeight", {
			configurable: true,
			value: 5000,
		});
		Object.defineProperty(box, "clientHeight", {
			configurable: true,
			value: 600,
		});
		fireEvent.scroll(box);
		await waitFor(() => {
			expect(requests).toBe(requestsAfterFirst);
		});
		expect(screen.queryByText("/second-page")).not.toBeInTheDocument();
	});

	it("shows a loading spinner while the next scroll page is fetching", async () => {
		// Hold the second page open so isFetchingNextPage stays true long enough to
		// assert the in-flight spinner, then release it.
		let releaseSecondPage: () => void = () => {};
		const secondPageGate = new Promise<void>((resolve) => {
			releaseSecondPage = resolve;
		});
		server.use(
			http.get("/api/audit", async ({ request }) => {
				const cursor = new URL(request.url).searchParams.get("cursor");
				if (cursor === "cur-1") {
					await secondPageGate;
					return HttpResponse.json({
						entries: [entry({ route: "/second-page" })],
						total: 2,
						has_more: false,
					});
				}
				return HttpResponse.json({
					entries: [entry({ route: "/first-page" })],
					total: 2,
					has_more: true,
					next_cursor: "cur-1",
				});
			}),
		);
		renderWithProviders(<Audit />);

		expect(await screen.findByText("/first-page")).toBeInTheDocument();
		fireEvent.scroll(scroller("/first-page"));
		// Spinner is visible while the next page is in flight.
		expect(
			await screen.findByRole("status", { name: "Loading" }),
		).toBeInTheDocument();

		releaseSecondPage();
		expect(await screen.findByText("/second-page")).toBeInTheDocument();
		expect(
			screen.queryByRole("status", { name: "Loading" }),
		).not.toBeInTheDocument();
	});

	it("keeps the filter input mounted while a new actor filter loads", async () => {
		// A new actor is a new query key. The previous pages stand in while it
		// loads, so the page never collapses to a spinner that unmounts the
		// input the user is typing in.
		let releaseFiltered: () => void = () => {};
		const filteredGate = new Promise<void>((resolve) => {
			releaseFiltered = resolve;
		});
		server.use(
			http.get("/api/audit", async ({ request }) => {
				const actor = new URL(request.url).searchParams.get("actor");
				if (actor) {
					await filteredGate;
					return HttpResponse.json({
						entries: [entry({ actor, route: "/filtered" })],
						total: 1,
						has_more: false,
					});
				}
				return HttpResponse.json({
					entries: [entry({ route: "/unfiltered" })],
					total: 1,
					has_more: false,
				});
			}),
		);
		renderWithProviders(<Audit />);
		expect(await screen.findByText("/unfiltered")).toBeInTheDocument();

		const input = screen.getByPlaceholderText("Filter by actor…");
		await userEvent.type(input, "adm");
		// Past the debounce, with the filtered fetch still held open.
		await waitFor(() => expect(input).toHaveValue("adm"));
		await new Promise((r) => setTimeout(r, 400));
		expect(screen.getByPlaceholderText("Filter by actor…")).toBe(input);
		expect(screen.getByText("/unfiltered")).toBeInTheDocument();

		releaseFiltered();
		expect(await screen.findByText("/filtered")).toBeInTheDocument();
	});

	it("navigates by page in pagination mode", async () => {
		server.use(
			http.get("/api/audit", ({ request }) => {
				const offset = Number(
					new URL(request.url).searchParams.get("offset") ?? "0",
				);
				return HttpResponse.json({
					entries: [entry({ route: offset > 0 ? "/page-two" : "/page-one" })],
					// Two pages' worth so the pager renders a second page button.
					total: 60,
					has_more: offset === 0,
				});
			}),
		);
		const { user } = renderWithProviders(<Audit />);

		expect(await screen.findByText("/page-one")).toBeInTheDocument();
		// Switch from infinite scroll to the paginated static table.
		await user.click(
			screen.getByTitle(
				"Click to toggle between pagination and infinite scrolling.",
			),
		);
		// Jump to page two -> the next offset is requested.
		await user.click(await screen.findByRole("button", { name: "2" }));
		expect(await screen.findByText("/page-two")).toBeInTheDocument();
	});

	it("keeps the footer on the rows shown while the next page loads", async () => {
		let releasePageTwo: () => void = () => {};
		const pageTwoHeld = new Promise<void>((r) => {
			releasePageTwo = r;
		});
		server.use(
			http.get("/api/audit", async ({ request }) => {
				const offset = Number(
					new URL(request.url).searchParams.get("offset") ?? "0",
				);
				if (offset > 0) await pageTwoHeld;
				return HttpResponse.json({
					entries: [entry({ route: offset > 0 ? "/page-two" : "/page-one" })],
					total: 60,
					has_more: offset === 0,
				});
			}),
		);
		const { user } = renderWithProviders(<Audit />);
		expect(await screen.findByText("/page-one")).toBeInTheDocument();
		await user.click(
			screen.getByTitle(
				"Click to toggle between pagination and infinite scrolling.",
			),
		);
		await user.click(await screen.findByRole("button", { name: "2" }));

		// Page one's row is still on screen, so the footer still describes it,
		// not page two's offset applied to page one's rows.
		expect(screen.getByText("/page-one")).toBeInTheDocument();
		expect(screen.getByText(range(1, 1, 60))).toBeInTheDocument();

		act(() => releasePageTwo());
		expect(await screen.findByText("/page-two")).toBeInTheDocument();
		expect(screen.queryByText(range(1, 1, 60))).toBeNull();
		expect(screen.getByText(rangePattern(60, true))).toBeInTheDocument();
	});

	it("purges after confirmation", async () => {
		let purged = false;
		server.use(
			http.get("/api/audit", () =>
				HttpResponse.json({
					entries: purged ? [] : [entry({ route: "/doomed" })],
					total: purged ? 0 : 1,
					has_more: false,
				}),
			),
			http.delete("/api/audit/purge", async ({ request }) => {
				const body = (await request.json()) as { older_than: string };
				if (body.older_than !== "all") {
					return HttpResponse.text("bad vocab", { status: 400 });
				}
				purged = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		const { user } = renderWithProviders(<Audit />);

		expect(await screen.findByText("/doomed")).toBeInTheDocument();
		await user.click(screen.getByTestId("audit-purge-button"));
		await user.click(await screen.findByTestId("audit-purge-confirm"));

		await waitFor(() => {
			expect(purged).toBe(true);
		});
		await waitFor(() => {
			expect(screen.queryByText("/doomed")).not.toBeInTheDocument();
		});
	});
});
