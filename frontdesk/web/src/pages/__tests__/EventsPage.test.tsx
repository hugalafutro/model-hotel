import {
	act,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FdEvent } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import i18n from "../../i18n";
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
		// The page-2 read is held open by a promise the test releases, and it
		// reports back once its body has been handed to the client. Both halves
		// are explicit signals: a wall-clock wait could expire before the held
		// response existed, and the assertions below would then hold vacuously.
		let release!: () => void;
		const held = new Promise<void>((r) => {
			release = r;
		});
		let served!: () => void;
		const landed = new Promise<void>((r) => {
			served = r;
		});
		server.use(
			http.get("/api/events", async ({ request }) => {
				const offset = Number(new URL(request.url).searchParams.get("offset"));
				seen += 1;
				const mine = seen;
				// Hold the second read (page 2) open past the third (back to page 1).
				if (mine === 2) await held;
				const events = Array.from({ length: offset === 0 ? 25 : 5 }, (_, i) =>
					ev(`${offset + i}`),
				);
				if (mine === 2) setTimeout(served, 0);
				return HttpResponse.json({ events, total: 30 });
			}),
		);
		renderPage();
		await screen.findByText("event 0");
		await userEvent.click(screen.getByRole("button", { name: /Next/i }));
		await userEvent.click(screen.getByRole("button", { name: /Previous/i }));
		await screen.findByText("event 0");
		release();
		await landed;
		// One more turn of the loop, so the superseded response has been through
		// the client before the rows are read.
		await act(async () => {
			await new Promise((r) => setTimeout(r, 0));
		});
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
		// The first read is empty; the event goes out only once that read has
		// been answered, and the refetch it triggers sees the logged event.
		let calls = 0;
		let firstRead!: () => void;
		const firstReadDone = new Promise<void>((r) => {
			firstRead = r;
		});
		server.use(
			http.get("/api/events", () => {
				calls += 1;
				if (calls === 1) {
					firstRead();
					return HttpResponse.json({ events: [], total: 0 });
				}
				return HttpResponse.json({
					events: [ev("1", { message: "fresh event" })],
					total: 1,
				});
			}),
			sseEmitting(
				[
					{
						id: "e1",
						type: "member.added",
						severity: "info",
						source: "frontdesk",
						message: "x",
						created_at: "",
					},
				],
				firstReadDone,
			),
		);
		renderPage();
		// Only the refetch the event triggered can surface it.
		expect(await screen.findByText("fresh event")).toBeInTheDocument();
		expect(calls).toBe(2);
	});
});

describe("EventsPage filters and rows", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("gives each severity its own badge class", async () => {
		server.use(
			http.get("/api/events", () =>
				HttpResponse.json({
					events: [
						ev("1", { severity: "success", message: "m-success" }),
						ev("2", { severity: "warning", message: "m-warning" }),
						ev("3", { severity: "error", message: "m-error" }),
						ev("4", { severity: "info", message: "m-info" }),
					],
					total: 4,
				}),
			),
		);
		renderPage();
		const badge = async (sev: string) => {
			const row = (await screen.findByText(`m-${sev}`)).closest(
				"tr",
			) as HTMLElement;
			return within(row).getByText(
				i18n.t(`events.sev${sev[0].toUpperCase()}${sev.slice(1)}`),
			);
		};
		expect(await badge("success")).toHaveClass("ui-badge-ok");
		expect(await badge("warning")).toHaveClass("ui-badge-warn");
		expect(await badge("error")).toHaveClass("ui-badge-danger");
		expect(await badge("info")).toHaveClass("ui-badge-info");
	});

	it("shows the network error when the read fails", async () => {
		server.use(
			http.get("/api/events", () =>
				HttpResponse.json({ error: "down" }, { status: 500 }),
			),
		);
		renderPage();
		expect(
			await screen.findByText(i18n.t("errors.network")),
		).toBeInTheDocument();
	});

	it("names a known member, falls back to the raw id, and filters by member and type", async () => {
		const seen: URLSearchParams[] = [];
		server.use(
			http.get("/api/members", () =>
				HttpResponse.json([
					{
						id: "m1",
						name: "hotel-one",
						url: "https://one.example.com",
						state: "active",
						has_token: true,
						created_at: "",
						updated_at: "",
					},
				]),
			),
			http.get("/api/events", ({ request }) => {
				seen.push(new URL(request.url).searchParams);
				return HttpResponse.json({
					events: [
						ev("1", { member_id: "m1", message: "known" }),
						ev("2", { member_id: "gone", message: "orphan" }),
					],
					total: 2,
				});
			}),
		);
		renderPage();
		const known = (await screen.findByText("known")).closest("td");
		await waitFor(() => expect(known).toHaveTextContent("· hotel-one"));
		expect((await screen.findByText("orphan")).closest("td")).toHaveTextContent(
			"· gone",
		);

		await userEvent.selectOptions(
			screen.getByLabelText(i18n.t("events.filterMember")),
			"m1",
		);
		await waitFor(() => expect(seen.at(-1)?.get("member_id")).toBe("m1"));
		await userEvent.selectOptions(
			screen.getByLabelText(i18n.t("events.filterType")),
			"member.added",
		);
		await waitFor(() => expect(seen.at(-1)?.get("type")).toBe("member.added"));
		expect(seen.at(-1)?.get("member_id")).toBe("m1");
	});

	it("pages with the horizontal wheel over the table", async () => {
		let clock = 0;
		// Each wheel event reads as a fresh gesture (gap over the idle threshold).
		vi.spyOn(Date, "now").mockImplementation(() => {
			clock += 1000;
			return clock;
		});
		const offsets: number[] = [];
		server.use(
			http.get("/api/events", ({ request }) => {
				const offset = Number(new URL(request.url).searchParams.get("offset"));
				offsets.push(offset);
				return HttpResponse.json({ events: [ev(`${offset}`)], total: 30 });
			}),
		);
		renderPage();
		await screen.findByText("event 0");
		const scroller = screen.getByRole("table").parentElement as HTMLElement;
		fireEvent.wheel(scroller, { deltaX: 120, deltaY: 0 });
		expect(await screen.findByText("event 25")).toBeInTheDocument();
		fireEvent.wheel(scroller, { deltaX: -120, deltaY: 0 });
		expect(await screen.findByText("event 0")).toBeInTheDocument();
		expect(offsets).toEqual([0, 25, 0]);
	});
});
