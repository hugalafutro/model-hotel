import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { AuditEntry } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Audit } from "../index";

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
	});

	it("renders entries with actor, action, and status", async () => {
		server.use(
			http.get("/api/audit", () =>
				HttpResponse.json({
					entries: [
						entry({
							actor: "alice",
							actor_role: "user",
							route: "/virtual-keys",
						}),
						entry({
							method: "DELETE",
							route: "/users/{id}",
							entity_id: "11111111-2222-4333-8444-555555555555",
							status_code: 204,
						}),
					],
					total: 2,
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
		// Entity id is shown truncated.
		expect(screen.getByText("11111111…")).toBeInTheDocument();
	});

	it("loads the next page through the cursor", async () => {
		server.use(
			http.get("/api/audit", ({ request }) => {
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
		const { user } = renderWithProviders(<Audit />);

		expect(await screen.findByText("/first-page")).toBeInTheDocument();
		await user.click(screen.getByTestId("audit-load-more"));
		expect(await screen.findByText("/second-page")).toBeInTheDocument();
		// First page stays visible; the load-more button is gone.
		expect(screen.getByText("/first-page")).toBeInTheDocument();
		expect(screen.queryByTestId("audit-load-more")).not.toBeInTheDocument();
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
