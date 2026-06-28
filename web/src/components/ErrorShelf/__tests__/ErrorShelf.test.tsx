import { act, screen, waitFor, within } from "@testing-library/react";
import { delay, HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ErrorShelf } from "../ErrorShelf";
import { isHaSource } from "../useErrorShelf";

describe("isHaSource", () => {
	it("flags the fleet config-sync sources", () => {
		expect(isHaSource("configsync")).toBe(true);
		expect(isHaSource("fleet")).toBe(true);
	});

	it("rejects non-fleet and missing sources", () => {
		expect(isHaSource("server")).toBe(false);
		expect(isHaSource("proxy")).toBe(false);
		expect(isHaSource(undefined)).toBe(false);
		expect(isHaSource("")).toBe(false);
	});
});

/** Seed the request-log (5xx) endpoint with one error row. */
function seedRequestError(message: string, createdAt: string) {
	server.use(
		http.get("/api/logs", () =>
			HttpResponse.json({
				entries: [
					{
						id: "req-1",
						provider_id: "p1",
						provider_name: "Prov",
						model_id: "m1",
						status_code: 502,
						error_message: message,
						error_kind: "upstream_5xx",
						created_at: createdAt,
					},
				],
				total: 1,
				page: 1,
				per_page: 15,
			}),
		),
	);
}

/** Seed the app-log history endpoint with one error row. The source defaults
 * to "server"; pass a fleet source ("configsync"/"fleet") to exercise the HA
 * sub-category. */
function seedAppError(message: string, timestamp: string, source = "server") {
	server.use(
		http.get("/api/logs/app", ({ request }) => {
			if (new URL(request.url).searchParams.get("history") !== "true") {
				return HttpResponse.json([]);
			}
			return HttpResponse.json({
				entries: [{ id: "app-1", timestamp, level: "error", source, message }],
				total: 1,
				page: 1,
				per_page: 15,
			});
		}),
	);
}

async function expand() {
	const header = await screen.findByRole("button", { expanded: false });
	await act(async () => {
		header.click();
	});
	return screen.findByRole("button", { expanded: true });
}

describe("ErrorShelf", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.removeItem("ackedErrorKeys");
	});

	it("renders nothing when there are no errors", async () => {
		renderWithProviders(<ErrorShelf />);
		// Give the polls a tick; default handlers return empty.
		await waitFor(() => {
			expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
		});
	});

	it("stays hidden until both error sources finish their first load", async () => {
		// app-logs hangs; the request query resolves with an unacked error.
		// Without the both-loaded gate, that lone resolution would flash the
		// shelf before the app query lands.
		server.use(
			http.get("/api/logs/app", async ({ request }) => {
				if (new URL(request.url).searchParams.get("history") === "true") {
					await delay("infinite");
				}
				return HttpResponse.json([]);
			}),
		);
		seedRequestError("req boom", "2024-02-01T12:00:00Z");
		renderWithProviders(<ErrorShelf />);

		// Let the request query settle; the shelf must remain hidden because the
		// app-logs query is still loading.
		await new Promise((r) => setTimeout(r, 60));
		expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
	});

	it("shows the shelf with an unacked count when errors exist", async () => {
		seedRequestError("upstream refused", "2024-02-01T10:00:00Z");
		renderWithProviders(<ErrorShelf />);
		const count = await screen.findByTestId("error-shelf-count");
		expect(count).toHaveTextContent("1");
	});

	it("interleaves app + request errors newest-first when expanded", async () => {
		// request error is newer than the app error
		seedRequestError("req boom", "2024-02-01T12:00:00Z");
		seedAppError("app boom", "2024-02-01T09:00:00Z");
		renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();

		const rows = await screen.findAllByTestId("error-shelf-row");
		expect(rows).toHaveLength(2);
		// newest (request) first
		expect(
			within(rows[0]).getByTestId("error-shelf-chip-request"),
		).toBeTruthy();
		expect(within(rows[0]).getByText("req boom")).toBeTruthy();
		expect(within(rows[1]).getByTestId("error-shelf-chip-app")).toBeTruthy();
		expect(within(rows[1]).getByText("app boom")).toBeTruthy();
	});

	it("tags fleet config-sync app errors with the HA chip", async () => {
		seedAppError("apply import failed", "2024-02-01T09:00:00Z", "configsync");
		renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();

		const rows = await screen.findAllByTestId("error-shelf-row");
		expect(rows).toHaveLength(1);
		// An HA-sourced app error gets the HA chip, not the generic app chip.
		expect(within(rows[0]).getByTestId("error-shelf-chip-ha")).toBeTruthy();
		expect(within(rows[0]).queryByTestId("error-shelf-chip-app")).toBeNull();
		expect(within(rows[0]).getByText("apply import failed")).toBeTruthy();
	});

	it("opens an HA error in the app log-detail modal", async () => {
		// HA stays kind="app", so "View details" must route to the app modal,
		// which surfaces the source ("configsync") and message.
		seedAppError("apply import failed", "2024-02-01T09:00:00Z", "configsync");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		const row = await screen.findByTestId("error-shelf-row");
		await user.click(within(row).getByTitle("View details"));

		const dialog = await screen.findByRole("dialog");
		expect(within(dialog).getByText("configsync")).toBeTruthy();
		expect(within(dialog).getByText("apply import failed")).toBeTruthy();
	});

	it("acknowledges a single row, persists it, and hides when none remain", async () => {
		seedRequestError("solo error", "2024-02-01T10:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		await user.click(screen.getByTestId("error-shelf-ack"));

		await waitFor(() => {
			expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
		});
		const acked = JSON.parse(localStorage.getItem("ackedErrorKeys") ?? "[]");
		expect(acked).toHaveLength(1);
		expect(acked[0]).toContain("solo error");
	});

	it("arms on the first Clear all click without clearing anything", async () => {
		seedRequestError("req boom", "2024-02-01T12:00:00Z");
		seedAppError("app boom", "2024-02-01T09:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		await user.click(screen.getByTestId("error-shelf-clear-all"));

		// Confirm hint shows; nothing dismissed yet; shelf still present.
		expect(screen.getByTestId("error-shelf-clear-confirm")).toBeInTheDocument();
		expect(screen.getByTestId("error-shelf")).toBeInTheDocument();
		expect(localStorage.getItem("ackedErrorKeys")).toBeNull();
	});

	it("clears all visible errors only on the second Clear all click", async () => {
		seedRequestError("req boom", "2024-02-01T12:00:00Z");
		seedAppError("app boom", "2024-02-01T09:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		await user.click(screen.getByTestId("error-shelf-clear-all")); // arm
		await user.click(screen.getByTestId("error-shelf-clear-all")); // confirm

		await waitFor(() => {
			expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
		});
		const acked = JSON.parse(localStorage.getItem("ackedErrorKeys") ?? "[]");
		expect(acked).toHaveLength(2);
	});

	it("disarms Clear all when the shelf is collapsed", async () => {
		seedRequestError("req boom", "2024-02-01T12:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		await user.click(screen.getByTestId("error-shelf-clear-all")); // arm
		expect(screen.getByTestId("error-shelf-clear-confirm")).toBeInTheDocument();

		// Collapse then re-expand: the arm should have reset.
		await user.click(await screen.findByRole("button", { expanded: true }));
		await expand();
		expect(
			screen.queryByTestId("error-shelf-clear-confirm"),
		).not.toBeInTheDocument();
		expect(localStorage.getItem("ackedErrorKeys")).toBeNull();
	});

	it("copies an error message to the clipboard", async () => {
		seedRequestError("copy me", "2024-02-01T10:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		const row = await screen.findByTestId("error-shelf-row");
		// Spy on whatever clipboard impl is live (userEvent.setup installs its own).
		const writeSpy = vi.spyOn(navigator.clipboard, "writeText");
		await user.click(within(row).getByTitle("Copy error"));

		expect(writeSpy).toHaveBeenCalledWith("copy me");
	});

	it("toasts an error when the clipboard write fails", async () => {
		seedRequestError("copy me", "2024-02-01T10:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		const row = await screen.findByTestId("error-shelf-row");
		vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(
			new Error("denied"),
		);
		await user.click(within(row).getByTitle("Copy error"));

		expect(await screen.findByText("Failed to copy")).toBeInTheDocument();
	});

	it("toasts an error when the Clipboard API is unavailable", async () => {
		seedRequestError("copy me", "2024-02-01T10:00:00Z");
		const { user } = renderWithProviders(<ErrorShelf />);

		await screen.findByTestId("error-shelf-count");
		await expand();
		const row = await screen.findByTestId("error-shelf-row");
		// A missing Clipboard API throws synchronously on access; the handler
		// must still route that to the failure toast.
		vi.spyOn(navigator.clipboard, "writeText").mockImplementationOnce(() => {
			throw new TypeError("clipboard unavailable");
		});
		await user.click(within(row).getByTitle("Copy error"));

		expect(await screen.findByText("Failed to copy")).toBeInTheDocument();
	});

	it("stays hidden for an already-acknowledged error", async () => {
		const ts = "2024-02-01T10:00:00Z";
		const msg = "already seen";
		// Pre-seed the acked set with this error's key.
		localStorage.setItem(
			"ackedErrorKeys",
			JSON.stringify([`request:${ts}:${msg.slice(0, 50)}`]),
		);
		seedRequestError(msg, ts);
		renderWithProviders(<ErrorShelf />);

		await waitFor(() => {
			expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
		});
	});

	it("un-acks everything on the dismissedErrorsReset event", async () => {
		const ts = "2024-02-01T10:00:00Z";
		const msg = "comes back";
		localStorage.setItem(
			"ackedErrorKeys",
			JSON.stringify([`request:${ts}:${msg.slice(0, 50)}`]),
		);
		seedRequestError(msg, ts);
		renderWithProviders(<ErrorShelf />);

		// Hidden because acked.
		await waitFor(() => {
			expect(screen.queryByTestId("error-shelf")).not.toBeInTheDocument();
		});

		act(() => {
			window.dispatchEvent(new Event("dismissedErrorsReset"));
		});

		// Reappears once acks are cleared.
		expect(await screen.findByTestId("error-shelf")).toBeInTheDocument();
	});
});
