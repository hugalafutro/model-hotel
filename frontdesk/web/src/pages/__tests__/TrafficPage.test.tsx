import { render, screen } from "@testing-library/react";
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
});
