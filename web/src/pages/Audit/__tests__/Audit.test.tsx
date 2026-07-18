import { act, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AuditEntry } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Audit } from "../index";

// Controllable IntersectionObserver: the page arms one on its scroll-foot
// sentinel, and tests call trigger() to simulate it scrolling into view.
class MockIntersectionObserver {
	static instances: MockIntersectionObserver[] = [];
	private readonly cb: IntersectionObserverCallback;
	private elements: Element[] = [];
	constructor(cb: IntersectionObserverCallback) {
		this.cb = cb;
		MockIntersectionObserver.instances.push(this);
	}
	observe(el: Element) {
		this.elements.push(el);
	}
	unobserve() {}
	disconnect() {}
	takeRecords() {
		return [];
	}
	trigger(isIntersecting = true) {
		this.cb(
			this.elements.map(
				(target) => ({ isIntersecting, target }) as IntersectionObserverEntry,
			),
			this as unknown as IntersectionObserver,
		);
	}
}

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
	beforeEach(() => {
		server.resetHandlers();
		localStorage.clear();
		MockIntersectionObserver.instances = [];
		vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
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
		// Unresolved entity falls back to the truncated UUID; a resolved one
		// shows its current display name instead.
		expect(screen.getByText("11111111…")).toBeInTheDocument();
		expect(screen.getByText("gpt-nice-name")).toBeInTheDocument();
		expect(screen.queryByText("22222222…")).not.toBeInTheDocument();
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
		// Full path and UUID appear in the modal (the table truncates both).
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

	it("appends the next page when the scroll sentinel comes into view", async () => {
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
		// Simulate the foot sentinel scrolling into view -> next page loads.
		act(() => {
			MockIntersectionObserver.instances.at(-1)?.trigger();
		});
		expect(await screen.findByText("/second-page")).toBeInTheDocument();
		// The first page stays appended above the second.
		expect(screen.getByText("/first-page")).toBeInTheDocument();
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
			screen.getByRole("button", { name: "Switch to pagination mode" }),
		);
		// Jump to page two -> the next offset is requested.
		await user.click(await screen.findByRole("button", { name: "2" }));
		expect(await screen.findByText("/page-two")).toBeInTheDocument();
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
