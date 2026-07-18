import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

afterEach(() => {
	// Tests that opt into fake timers reset here; a no-op for the real-timer ones.
	vi.useRealTimers();
	vi.restoreAllMocks();
});

// settle drives the fake-timer clock forward a little, flushing the mount fetch
// chain (members -> per-card traffic) without reaching the 5s auto-refresh tick.
async function settle() {
	await act(async () => {
		await vi.advanceTimersByTimeAsync(50);
	});
}

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

	it("charts an all-zero series instead of the No-data state", async () => {
		// Reachable member, buckets present but no requests: the chart must render
		// (showing the flat green baseline), not the No-data empty state.
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 0,
						total_errors: 0,
						points: [
							{ bucket: "b1", requests: 0, errors: 0 },
							{ bucket: "b2", requests: 0, errors: 0 },
						],
					}),
				),
			),
		);
		renderPage();
		// The metrics row (chart branch only) shows the zero totals...
		expect(await screen.findByText("Requests")).toBeInTheDocument();
		expect(screen.getByText("Error rate")).toBeInTheDocument();
		// ...and the No-data empty state is not shown.
		expect(screen.queryByText("No data")).not.toBeInTheDocument();
	});

	it("shows the No-data state only when the series is empty", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(traffic({ member_id: "1", points: [] })),
			),
		);
		renderPage();
		expect(await screen.findByText("No data")).toBeInTheDocument();
	});

	it("renders with a multi-bucket error spike without crashing", async () => {
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 300,
						total_errors: 7,
						points: [
							{ bucket: "b1", requests: 100, errors: 0 },
							{ bucket: "b2", requests: 100, errors: 7 },
							{ bucket: "b3", requests: 100, errors: 0 },
						],
					}),
				),
			),
		);
		renderPage();
		expect(await screen.findByText("300")).toBeInTheDocument();
		// 7/300 ≈ 2.3% error rate.
		expect(screen.getByText("2.3%")).toBeInTheDocument();
	});

	it("stamps the page with a single shared last-updated time", async () => {
		vi.useFakeTimers();
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([member({ id: "1", name: "hotel-1" })]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 100,
						points: [{ bucket: "b1", requests: 100, errors: 0 }],
					}),
				),
			),
		);
		renderPage();
		await settle();

		expect(screen.getByText("100")).toBeInTheDocument();
		// A single page-level stamp, mirroring the Members tab - not a per-card one.
		expect(screen.getByTestId("traffic-updated")).toHaveTextContent(/Updated/);
		expect(
			screen.queryByTestId("traffic-updated-local"),
		).not.toBeInTheDocument();
	});

	it("keeps per-card stamps when one member's read fails", async () => {
		// Two token members, one reachable and one whose stats can't be read. The
		// page must NOT collapse to a single shared stamp (that would falsely imply
		// the failed member is fresh too); the healthy card keeps its own stamp and
		// the failed card shows none.
		vi.useFakeTimers();
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
				]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 100,
						points: [{ bucket: "b1", requests: 100, errors: 0 }],
					}),
				),
			),
			http.get("/api/members/2/traffic", () =>
				HttpResponse.json(traffic({ member_id: "2", reachable: false })),
			),
		);
		renderPage();
		await settle();

		expect(screen.getByText("100")).toBeInTheDocument();
		// No misleading page-level shared stamp while a card is unfresh.
		expect(screen.queryByTestId("traffic-updated")).not.toBeInTheDocument();
		// The healthy card keeps exactly one local stamp; the failed one has none.
		expect(screen.getAllByTestId("traffic-updated-local")).toHaveLength(1);
	});

	it("keeps one shared stamp when reachable cards report different times", async () => {
		// Regression: two reachable cards can commit slightly different fetch times
		// (one lands a beat after the other on a refresh tick). That difference must
		// NOT split the page into per-card stamps - which flashed a ~15px line under
		// the second graph - since both are fresh. The page keeps a single shared
		// stamp using the latest time. Distinct per-call times are forced here.
		let tick = 0;
		vi.spyOn(Date.prototype, "toISOString").mockImplementation(() => {
			tick += 1;
			return `2026-01-01T00:00:0${tick}.000Z`;
		});
		vi.useFakeTimers();
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					member({ id: "1", name: "hotel-1" }),
					member({ id: "2", name: "hotel-2" }),
				]),
			),
			http.get("/api/members/1/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "1",
						total_requests: 10,
						points: [{ bucket: "b1", requests: 10, errors: 0 }],
					}),
				),
			),
			http.get("/api/members/2/traffic", () =>
				HttpResponse.json(
					traffic({
						member_id: "2",
						total_requests: 20,
						points: [{ bucket: "b1", requests: 20, errors: 0 }],
					}),
				),
			),
		);
		renderPage();
		await settle();

		expect(screen.getByText("10")).toBeInTheDocument();
		expect(screen.getByText("20")).toBeInTheDocument();
		// Both reachable but with different reported times: still exactly one shared
		// stamp, and no per-card stamp flashed in.
		expect(screen.getByTestId("traffic-updated")).toBeInTheDocument();
		expect(screen.queryAllByTestId("traffic-updated-local")).toHaveLength(0);
	});

	it("auto-refreshes every graph on an interval with no user action", async () => {
		vi.useFakeTimers();
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
		renderPage();
		await settle();
		expect(screen.getByText("100")).toBeInTheDocument();

		// One auto-refresh tick re-pulls the endpoint; the new total replaces it.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(5000);
		});
		expect(screen.getByText("250")).toBeInTheDocument();
		expect(calls).toBeGreaterThanOrEqual(2);
	});

	it("pausing halts the auto-refresh", async () => {
		vi.useFakeTimers();
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
						total_requests: 100,
						points: [{ bucket: "b1", requests: 100, errors: 0 }],
					}),
				);
			}),
		);
		renderPage();
		await settle();
		expect(screen.getByText("100")).toBeInTheDocument();

		const toggle = screen.getByTestId("traffic-pause");
		expect(toggle).toHaveTextContent(/Pause/);
		act(() => {
			fireEvent.click(toggle);
		});
		// The control flips to a resume affordance and reports its pressed state.
		expect(toggle).toHaveTextContent(/Resume/);
		expect(toggle).toHaveAttribute("aria-pressed", "true");

		const before = calls;
		await act(async () => {
			await vi.advanceTimersByTimeAsync(15000);
		});
		// No further reads while paused, even across several would-be ticks.
		expect(calls).toBe(before);
	});

	it("drops the last-updated stamp when a refresh fails", async () => {
		vi.useFakeTimers();
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
		renderPage();
		await settle();

		// First load shows the shared "Updated ..." stamp.
		expect(screen.getByText("100")).toBeInTheDocument();
		expect(screen.getByTestId("traffic-updated")).toBeInTheDocument();

		// A failed auto-refresh must clear the stamp rather than leave a stale time
		// next to the read-failure state.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(5000);
		});
		expect(screen.queryByTestId("traffic-updated")).not.toBeInTheDocument();
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
								points: [{ bucket: "b1", requests: 100, errors: 0 }],
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
