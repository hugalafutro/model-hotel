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
	health_fail_threshold: 3,
	session_idle_timeout_minutes: 60,
	alert_enabled: false,
	alert_apprise_api_url: "",
	alert_apprise_targets: "",
	alert_events: "",
	oidc_enabled: false,
	oidc_issuer_url: "",
	oidc_client_id: "",
	oidc_client_secret: "",
	oidc_public_base_url: "",
	oidc_allowed_emails: "",
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
		// The Alerts panel loads its catalog + reachability on mount.
		http.get("/api/alert/events", () => HttpResponse.json([])),
		http.get("/api/alert/status", () =>
			HttpResponse.json({
				configured: false,
				reachable: false,
				healthy: false,
			}),
		),
		// The OIDC panel polls SSO status on mount.
		http.get("/api/auth/oidc/status", () =>
			HttpResponse.json({ enabled: false }),
		),
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

	it("does not revert alert settings when the polling form is saved (B1)", async () => {
		// Stateful settings: PUT is a partial merge onto the stored row (mirrors the
		// server), so each panel writes only its own fields and cannot clobber the
		// other's. The test would fail if a panel sent a full row from stale state.
		let current: Settings = { ...defaults, alert_enabled: true };
		server.use(
			http.get("/api/settings", () => HttpResponse.json(current)),
			http.put("/api/settings", async ({ request }) => {
				const patch = (await request.json()) as Partial<Settings>;
				current = { ...current, ...patch };
				return new HttpResponse(null, { status: 204 });
			}),
			http.get("/api/alert/events", () =>
				HttpResponse.json([
					{
						type: "health.down",
						category: "Health",
						severity: "error",
						defaultOn: true,
					},
				]),
			),
		);
		renderPage();

		// Turn alerts OFF in the Alerts panel and save it.
		const enable = await screen.findByRole("checkbox", {
			name: /outbound alert notifications/i,
		});
		expect(enable).toBeChecked();
		await userEvent.click(enable);
		await userEvent.click(
			screen.getByRole("button", { name: /save alert settings/i }),
		);
		await waitFor(() => expect(current.alert_enabled).toBe(false));

		// Now save an unrelated polling field. Pre-fix this PUT carried the panel's
		// stale alert_enabled:true and reverted the change.
		const stale = (await screen.findByLabelText(
			/staleness warning/i,
		)) as HTMLInputElement;
		await userEvent.clear(stale);
		await userEvent.type(stale, "42");
		await userEvent.click(screen.getByRole("button", { name: /^Save$/i }));

		await waitFor(() => expect(current.traefik_stale_secs).toBe(42));
		expect(current.alert_enabled).toBe(false);
	});
});
