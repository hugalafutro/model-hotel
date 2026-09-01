import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { MembersPage } from "../MembersPage";

const GROUP_ID = "11111111-2222-3333-4444-555555555555";

function member(id: string, name: string, hasToken: boolean): MemberView {
	return {
		id,
		name,
		url: `https://${name}.example.com`,
		state: "active",
		has_token: hasToken,
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
	};
}

// Locale-independent: the button, the select and the confirm are found by
// testid, the outcome by the request the page sends.
describe("MembersPage fleet circuit reset", () => {
	beforeEach(() => {
		server.use(
			sseHandler(),
			http.get(
				"/api/fleet/last-sync",
				() => new HttpResponse(null, { status: 204 }),
			),
		);
	});

	it("is offered only with a primary, picks a group, and posts that group's id", async () => {
		let posted: unknown;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member("m1", "primary", true),
					member("m2", "second", true),
				]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "m1" }),
			),
			http.get("/api/fleet/failover-groups", ({ request }) => {
				expect(new URL(request.url).searchParams.get("primary_id")).toBe("m1");
				return HttpResponse.json([
					{
						id: GROUP_ID,
						display_model: "glm53",
						display_name: "GLM 5.3",
						entries: 3,
						group_enabled: true,
					},
				]);
			}),
			http.post("/api/fleet/circuit-breaker/reset", async ({ request }) => {
				posted = await request.json();
				return HttpResponse.json({
					group_id: GROUP_ID,
					members: [
						{
							member_id: "m1",
							name: "primary",
							ok: true,
							cleared: 2,
							recovered: 1,
						},
						{
							member_id: "m2",
							name: "second",
							ok: true,
							cleared: 1,
							recovered: 0,
						},
					],
					cleared: 3,
					recovered: 1,
					failed: 0,
				});
			}),
		);
		render(
			<ToastProvider>
				<MembersPage />
			</ToastProvider>,
		);
		const user = userEvent.setup();
		const button = await screen.findByTestId("fleet-reset-circuits");
		await user.click(button);
		const select = await screen.findByTestId("fleet-reset-circuits-group");
		// Nothing chosen yet: the confirm is inert.
		const confirm = screen.getByTestId("fleet-reset-circuits-confirm");
		expect(confirm).toBeDisabled();
		await user.selectOptions(select, GROUP_ID);
		expect(confirm).not.toBeDisabled();
		await user.click(confirm);
		await waitFor(() => expect(posted).toEqual({ group_id: GROUP_ID }));
		await waitFor(() =>
			expect(
				screen.queryByTestId("fleet-reset-circuits-group"),
			).not.toBeInTheDocument(),
		);
	});

	it("is absent without a designated primary", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member("m1", "solo", true)]),
			),
			http.get("/api/fleet/autosync", () =>
				HttpResponse.json({ enabled: false, primary_id: "" }),
			),
		);
		render(
			<ToastProvider>
				<MembersPage />
			</ToastProvider>,
		);
		await screen.findByText("solo");
		expect(
			screen.queryByTestId("fleet-reset-circuits"),
		).not.toBeInTheDocument();
	});
});
