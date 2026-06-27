import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { Settings } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { SettingsPage } from "../SettingsPage";

const defaults: Settings = {
	health_poll_secs: 5,
	traefik_poll_secs: 5,
	traefik_stale_secs: 30,
	event_retention_days: 90,
	retry_attempts: 2,
	sticky_enabled: true,
};

function renderPage() {
	return render(
		<ToastProvider>
			<SettingsPage />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	// Settings embeds the fleet sync wizard, which loads the member list
	// and an SSE stream.
	server.use(
		sseHandler(),
		http.get("/api/members", () => HttpResponse.json([])),
	);
});

describe("SettingsPage", () => {
	it("loads and saves edited settings", async () => {
		let saved: Settings | null = null;
		server.use(
			http.get("/api/settings", () => HttpResponse.json(defaults)),
			http.put("/api/settings", async ({ request }) => {
				saved = (await request.json()) as Settings;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		const stale = (await screen.findByLabelText(
			/staleness warning/i,
		)) as HTMLInputElement;
		expect(stale.value).toBe("30");
		await userEvent.clear(stale);
		await userEvent.type(stale, "45");
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));
		await waitFor(() => expect(saved?.traefik_stale_secs).toBe(45));
	});

	it("coerces a cleared numeric field to its minimum (never NaN) on save", async () => {
		let saved: Settings | null = null;
		server.use(
			http.get("/api/settings", () => HttpResponse.json(defaults)),
			http.put("/api/settings", async ({ request }) => {
				saved = (await request.json()) as Settings;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		const stale = (await screen.findByLabelText(
			/staleness warning/i,
		)) as HTMLInputElement;
		await userEvent.clear(stale);
		await userEvent.tab(); // blur coerces the empty field to its minimum (1)
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));
		// The saved value is the minimum (1), not NaN — a cleared field is coerced.
		await waitFor(() => expect(saved?.traefik_stale_secs).toBe(1));
	});

	it("toggles sticky sessions", async () => {
		let saved: Settings | null = null;
		server.use(
			http.get("/api/settings", () => HttpResponse.json(defaults)),
			http.put("/api/settings", async ({ request }) => {
				saved = (await request.json()) as Settings;
				return new HttpResponse(null, { status: 204 });
			}),
		);
		renderPage();
		const sticky = (await screen.findByRole("checkbox")) as HTMLInputElement;
		expect(sticky.checked).toBe(true);
		await userEvent.click(sticky);
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));
		await waitFor(() => expect(saved?.sticky_enabled).toBe(false));
	});

	it("surfaces a validation error from the API", async () => {
		server.use(
			http.get("/api/settings", () => HttpResponse.json(defaults)),
			http.put(
				"/api/settings",
				() =>
					new HttpResponse(
						"frontdesk: validation failed: poll/stale intervals must be at least 1 second",
						{ status: 400 },
					),
			),
		);
		renderPage();
		await screen.findByRole("button", { name: /^Save$/i });
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/at least 1 second/i,
		);
	});
});
