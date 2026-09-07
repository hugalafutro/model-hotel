import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FdEvent } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseEmitting, sseHandler } from "../../test/sse";
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

	// The filter list is hand-kept against what the backend persists; a type the
	// server writes to the event log but the list omits cannot be filtered for.
	it("offers every persisted event type as a filter option", async () => {
		server.use(
			http.get("/api/events", () =>
				HttpResponse.json({ events: [], total: 0 }),
			),
		);
		renderPage();
		const select = await screen.findByLabelText("Type");
		expect(
			within(select).getByRole("option", {
				name: "fleet.circuit_breaker_reset",
			}),
		).toBeInTheDocument();
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

	// Two page changes in quick succession leave two reads open at once. Applying
	// whichever lands last would put page 2's rows under a page-1 counter, so a
	// superseded response is dropped.
	it("ignores a superseded page response that lands last", async () => {
		let seen = 0;
		server.use(
			http.get("/api/events", async ({ request }) => {
				const offset = Number(new URL(request.url).searchParams.get("offset"));
				seen += 1;
				// Hold the second read (page 2) open past the third (back to page 1).
				if (seen === 2) await new Promise((r) => setTimeout(r, 60));
				const events = Array.from({ length: offset === 0 ? 25 : 5 }, (_, i) =>
					ev(`${offset + i}`),
				);
				return HttpResponse.json({ events, total: 30 });
			}),
		);
		renderPage();
		await screen.findByText("event 0");
		await userEvent.click(screen.getByRole("button", { name: /Next/i }));
		await userEvent.click(screen.getByRole("button", { name: /Previous/i }));
		await screen.findByText("event 0");
		// Long enough for the held page-2 read to land, which must change nothing.
		await new Promise((r) => setTimeout(r, 120));
		expect(screen.getByText("event 0")).toBeInTheDocument();
		expect(screen.queryByText("event 25")).toBeNull();
	});

	it("resets to the first page when filters are cleared", async () => {
		const offsets: number[] = [];
		server.use(
			http.get("/api/events", ({ request }) => {
				offsets.push(Number(new URL(request.url).searchParams.get("offset")));
				return HttpResponse.json({ events: [ev("0")], total: 60 });
			}),
		);
		renderPage();
		await screen.findByText("event 0");
		// Activate a filter (this also reveals the Clear Filters button).
		await userEvent.selectOptions(
			screen.getByLabelText(/Severity/i),
			"warning",
		);
		// Move to page 2 (offset 25).
		await userEvent.click(screen.getByRole("button", { name: /Next/i }));
		await waitFor(() => expect(offsets).toContain(25));
		// Clearing filters must return to the first page (offset 0), not stay at 25.
		await userEvent.click(screen.getByRole("button", { name: /Clear/i }));
		await waitFor(() => expect(offsets.at(-1)).toBe(0));
	});

	it("refetches on the first page when an SSE event arrives", async () => {
		let calls = 0;
		server.use(
			http.get("/api/events", () => {
				calls += 1;
				return HttpResponse.json(
					calls === 1
						? { events: [], total: 0 }
						: { events: [ev("1", { message: "fresh event" })], total: 1 },
				);
			}),
			sseEmitting([
				{
					id: "e1",
					type: "member.added",
					severity: "info",
					source: "frontdesk",
					message: "x",
					created_at: "",
				},
			]),
		);
		renderPage();
		// The first page load is empty; only the SSE-triggered refetch surfaces it.
		expect(await screen.findByText("fresh event")).toBeInTheDocument();
	});
});
