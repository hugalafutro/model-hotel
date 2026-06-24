import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FdEvent } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { EventsPage } from "../EventsPage";

function ev(id: string, over: Partial<FdEvent> = {}): FdEvent {
	return {
		id,
		type: "member.added",
		severity: "info",
		source: "frontdesk",
		message: `event ${id}`,
		created_at: new Date().toISOString(),
		...over,
	};
}

function renderPage() {
	return render(
		<ToastProvider>
			<EventsPage />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	server.use(
		sseHandler(),
		http.get("/api/members", () => HttpResponse.json([])),
	);
});

describe("EventsPage", () => {
	it("renders events with severity badges", async () => {
		server.use(
			http.get("/api/events", () =>
				HttpResponse.json({
					events: [ev("1", { severity: "error", message: "boom" })],
					total: 1,
				}),
			),
		);
		renderPage();
		const row = (await screen.findByText("boom")).closest("tr") as HTMLElement;
		// Severity renders its localized label ("Error"), not the raw key (scoped
		// to the row so it doesn't match the "Error" severity-filter option).
		expect(within(row).getByText("Error")).toBeInTheDocument();
	});

	it("passes the severity filter to the API", async () => {
		const seen: string[] = [];
		server.use(
			http.get("/api/events", ({ request }) => {
				seen.push(new URL(request.url).searchParams.get("severity") ?? "");
				return HttpResponse.json({ events: [], total: 0 });
			}),
		);
		renderPage();
		await screen.findByText(/No events match/i);
		await userEvent.selectOptions(
			screen.getByLabelText(/Severity/i),
			"warning",
		);
		await waitFor(() => expect(seen).toContain("warning"));
	});

	it("sends a since bound for a time range", async () => {
		let sawSince = false;
		server.use(
			http.get("/api/events", ({ request }) => {
				if (new URL(request.url).searchParams.get("since")) sawSince = true;
				return HttpResponse.json({ events: [], total: 0 });
			}),
		);
		renderPage();
		await screen.findByText(/No events match/i);
		await userEvent.selectOptions(screen.getByLabelText(/Time range/i), "24h");
		await waitFor(() => expect(sawSince).toBe(true));
	});

	it("paginates with offset and disables prev on the first page", async () => {
		const offsets: number[] = [];
		server.use(
			http.get("/api/events", ({ request }) => {
				const offset = Number(new URL(request.url).searchParams.get("offset"));
				offsets.push(offset);
				// 30 total → two pages of 25.
				const events = Array.from({ length: offset === 0 ? 25 : 5 }, (_, i) =>
					ev(`${offset + i}`),
				);
				return HttpResponse.json({ events, total: 30 });
			}),
		);
		renderPage();
		await screen.findByText("event 0");
		expect(screen.getByRole("button", { name: /Previous/i })).toBeDisabled();
		await userEvent.click(screen.getByRole("button", { name: /Next/i }));
		await waitFor(() => expect(offsets).toContain(25));
	});
});
