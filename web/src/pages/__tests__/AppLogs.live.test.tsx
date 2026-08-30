import { act, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import i18n from "../../i18n";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { AppLogs } from "../AppLogs";

// App logs have no SSE feed, so scroll mode keeps itself current with a 5s
// poll plus a refresh when the tab regains visibility. Both are gated on
// document.hidden: a background tab left open for hours would otherwise keep
// hitting the log API for a view nobody is looking at.

let cursorCalls = 0;

function serveCursorLogs() {
	cursorCalls = 0;
	server.use(
		http.get("/api/logs/app/cursor", () => {
			cursorCalls++;
			return HttpResponse.json({
				entries: [
					{
						id: "log-1",
						created_at: "2026-06-10T10:00:00Z",
						timestamp: "2026-06-10T10:00:00Z",
						level: "info",
						source: "proxy",
						message: "an entry",
					},
				],
				total: 1,
				has_before: false,
				has_after: false,
			});
		}),
		http.get("/api/logs/app", () =>
			HttpResponse.json({
				entries: [],
				total: 0,
				page: 1,
				per_page: 20,
				level_counts: { info: 0, warning: 0, error: 0 },
				source_counts: {},
			}),
		),
	);
}

/** Replaces document.hidden for the duration of the test. */
function setTabHidden(hidden: boolean) {
	Object.defineProperty(document, "hidden", {
		configurable: true,
		get: () => hidden,
	});
}

async function advance(ms: number) {
	await act(async () => {
		await vi.advanceTimersByTimeAsync(ms);
	});
}

describe("AppLogs live polling", () => {
	beforeEach(() => {
		serveCursorLogs();
		setTabHidden(false);
		// Installed before render so the poll interval is a fake one from the
		// moment the effect creates it. shouldAdvanceTime keeps MSW and react-
		// query progressing on their own, so waitFor still resolves.
		vi.useFakeTimers({ shouldAdvanceTime: true });
	});

	afterEach(() => {
		vi.useRealTimers();
		setTabHidden(false);
	});

	it("polls for newer entries while the tab is visible", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(cursorCalls).toBeGreaterThan(0);
		});
		const afterLoad = cursorCalls;

		await advance(5000);
		expect(cursorCalls).toBeGreaterThan(afterLoad);
	});

	it("does not poll a backgrounded tab", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(cursorCalls).toBeGreaterThan(0);
		});
		const afterLoad = cursorCalls;

		setTabHidden(true);
		await advance(20000); // four poll ticks
		expect(cursorCalls).toBe(afterLoad);
	});

	it("refreshes when the tab comes back to the foreground, not when it leaves", async () => {
		renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(cursorCalls).toBeGreaterThan(0);
		});
		const beforeHiding = cursorCalls;

		// The event fires on the way out too. That one must do nothing: the
		// whole point of the guard is that a tab nobody is looking at stops
		// asking for logs.
		setTabHidden(true);
		await act(async () => {
			document.dispatchEvent(new Event("visibilitychange"));
		});
		expect(cursorCalls).toBe(beforeHiding);

		setTabHidden(false);
		await act(async () => {
			document.dispatchEvent(new Event("visibilitychange"));
		});
		await waitFor(() => {
			expect(cursorCalls).toBeGreaterThan(beforeHiding);
		});
	});

	it("stops polling once live updates are switched off", async () => {
		const { user } = renderWithProviders(<AppLogs />);
		await waitFor(() => {
			expect(cursorCalls).toBeGreaterThan(0);
		});

		await user.click(
			screen.getByRole("button", {
				name: i18n.t("components.logs.liveToggle.live"),
			}),
		);

		const afterToggle = cursorCalls;
		await advance(20000);
		expect(cursorCalls).toBe(afterToggle);
	});
});
