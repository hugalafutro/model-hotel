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
	localStorage.setItem("fdAuthToken", "tok");
	server.use(sseHandler());
	// Default: no fleet sync has run, so no member is the primary. Tests that
	// care override this with a 200 carrying a primary_id.
	server.use(
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
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByText("hotel-1")).toBeInTheDocument();
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
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/must use https/i,
		);
	});

	it("warns when a saved member's token could not be confirmed", async () => {
		server.use(
			http.get("/api/members", () => HttpResponse.json([])),
			http.post("/api/members", () =>
				HttpResponse.json(
					member({
						id: "1",
						name: "hotel-1",
						token_warning:
							"Saved, but Front Desk could not reach this member to verify the token yet.",
					}),
					{ status: 201 },
				),
			),
		);
		renderPage();
		await screen.findByText(/No members yet/i);
		await userEvent.type(screen.getByLabelText(/Display name/i), "hotel-1");
		await userEvent.type(
			screen.getByLabelText(/Base URL/i),
			"https://hotel-1.example.com",
		);
		await userEvent.type(screen.getByLabelText(/Admin token/i), "tok");
		await userEvent.click(screen.getByRole("button", { name: /^Add$/i }));
		expect(
			await screen.findByText(/could not reach this member to verify/i),
		).toBeInTheDocument();
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
		await userEvent.click(screen.getByRole("button", { name: /^Drain$/i }));
		// The action toggles to "Activate" and the state badge flips to Drained.
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: /^Activate$/i }),
			).toBeInTheDocument(),
		);
	});

	it("removes a member after confirming", async () => {
		let deleted = false;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json(
					deleted ? [] : [member({ id: "1", name: "hotel-1" })],
				),
			),
			http.delete("/api/members/1", () => {
				deleted = true;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		await screen.findByText("hotel-1");
		await userEvent.click(screen.getByRole("button", { name: /^Remove$/i }));
		// Confirm modal → click the destructive Remove inside it.
		const dialog = await screen.findByRole("dialog");
		await userEvent.click(
			within(dialog).getByRole("button", { name: /^Remove$/i }),
		);
		await waitFor(() =>
			expect(screen.getByText(/No members yet/i)).toBeInTheDocument(),
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
			http.get("/api/fleet/last-sync", () =>
				HttpResponse.json({
					last_run_at: new Date().toISOString(),
					primary_id: "2",
					primary_name: "hotel-2",
				}),
			),
		);
		renderPage();

		const badge = await screen.findByTestId("primary-badge");
		const primaryRow = badge.closest("tr") as HTMLElement;
		expect(within(primaryRow).getByText("hotel-2")).toBeInTheDocument();
		expect(primaryRow).toHaveClass("fd-row-primary");

		// hotel-2 is the recorded primary, so it sorts above hotel-1 even though it
		// was returned second by the API.
		const bodyRows = within(screen.getByRole("table")).getAllByRole("row");
		expect(within(bodyRows[1]).getByText("hotel-2")).toBeInTheDocument();
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
});
