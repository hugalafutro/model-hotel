import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { MemberTraffic, MemberView } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { TrafficPage } from "../TrafficPage";

function member(
	over: Partial<MemberView> & { id: string; name: string },
): MemberView {
	return {
		url: `https://${over.name}.example.com`,
		state: "active",
		has_token: true,
		created_at: "",
		updated_at: "",
		status: {
			health: { known: true, healthy: true, latency_ms: 1, checked_at: "" },
		},
		...over,
	};
}

function traffic(
	over: Partial<MemberTraffic> & { member_id: string },
): MemberTraffic {
	return {
		reachable: true,
		window_minutes: 60,
		total_requests: 0,
		total_errors: 0,
		points: [],
		...over,
	};
}

function renderPage() {
	return render(
		<ToastProvider>
			<TrafficPage />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	server.use(sseHandler());
});

describe("TrafficPage", () => {
	it("shows the empty state when no member has a token", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1", has_token: false }),
				]),
			),
		);
		renderPage();
		expect(
			await screen.findByText(/No members with a stored admin token/i),
		).toBeInTheDocument();
	});

	it("renders request/error metrics for a reachable member", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 100,
						total_errors: 5,
						points: [{ bucket: "b1", requests: 100, errors: 5 }],
					}),
				),
			),
		);
		renderPage();
		expect(await screen.findByText("100")).toBeInTheDocument();
		// 5/100 = 5.0% error rate.
		expect(screen.getByText("5.0%")).toBeInTheDocument();
	});

	it("stamps each graph with a last-updated time and refreshes on demand", async () => {
		let calls = 0;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () => {
				calls += 1;
				return HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: calls === 1 ? 100 : 250,
						points: [
							{ bucket: "b1", requests: calls === 1 ? 100 : 250, errors: 0 },
						],
					}),
				);
			}),
		);
		const user = userEvent.setup();
		renderPage();

		// First load: the metric plus an "Updated ..." stamp under the graph.
		expect(await screen.findByText("100")).toBeInTheDocument();
		expect(screen.getByTestId("traffic-updated")).toHaveTextContent(/Updated/);

		// Refresh re-pulls the traffic endpoint; the new total replaces the old.
		await user.click(screen.getByTestId("traffic-refresh"));
		expect(await screen.findByText("250")).toBeInTheDocument();
		expect(calls).toBeGreaterThanOrEqual(2);
	});

	it("drops the last-updated stamp when a refresh fails", async () => {
		let calls = 0;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () => {
				calls += 1;
				if (calls === 1) {
					return HttpResponse.json(
						traffic({
							member_id: "1",
							total_requests: 100,
							points: [{ bucket: "b1", requests: 100, errors: 0 }],
						}),
					);
				}
				return new HttpResponse(null, { status: 500 });
			}),
		);
		const user = userEvent.setup();
		renderPage();

		// First load shows the "Updated ..." stamp.
		expect(await screen.findByText("100")).toBeInTheDocument();
		expect(screen.getByTestId("traffic-updated")).toBeInTheDocument();

		// A failed refresh must clear the stamp rather than leave a stale time
		// next to the read-failure state.
		await user.click(screen.getByTestId("traffic-refresh"));
		await waitFor(() =>
			expect(screen.queryByTestId("traffic-updated")).not.toBeInTheDocument(),
		);
	});

	it("shows an unreachable note for a member whose stats can't be read", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(traffic({ member_id: "1", reachable: false })),
			),
		);
		renderPage();
		expect(
			await screen.findByText(/Could not read metrics/i),
		).toBeInTheDocument();
	});

	it("retries a single unreachable member from its own Refresh button", async () => {
		let call = 0;
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () => {
				call += 1;
				// Unreachable on first read, recovers on the retry.
				return HttpResponse.json(
					call === 1
						? traffic({ member_id: "1", reachable: false })
						: traffic({
								member_id: "1",
								reachable: true,
								total_requests: 100,
							}),
				);
			}),
		);
		const user = userEvent.setup();
		renderPage();

		await screen.findByText(/Could not read metrics/i);
		// The refresh sits inside the unreachable card, next to the reason
		// (distinct from the page-level refresh in the header).
		await user.click(screen.getByTestId("traffic-member-refresh"));

		expect(await screen.findByText("100")).toBeInTheDocument();
		expect(
			screen.queryByText(/Could not read metrics/i),
		).not.toBeInTheDocument();
	});
});
