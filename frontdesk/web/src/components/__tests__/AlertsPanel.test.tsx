import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import type { AlertEventDef, Settings } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { AlertsPanel } from "../AlertsPanel";

const catalog: AlertEventDef[] = [
	{
		type: "health.down",
		category: "Health",
		severity: "error",
		defaultOn: true,
	},
	{
		type: "health.up",
		category: "Health",
		severity: "success",
		defaultOn: true,
	},
	{
		type: "config.sync_failed",
		category: "Config Sync",
		severity: "error",
		defaultOn: true,
	},
];

const settings: Settings = {
	health_poll_secs: 5,
	traefik_poll_secs: 5,
	traefik_stale_secs: 30,
	event_retention_days: 90,
	retry_attempts: 2,
	alert_enabled: true,
	alert_apprise_api_url: "http://apprise:8000",
	alert_apprise_targets: "********", // a secret is stored; served masked
	alert_events: "health.down",
};

function renderPanel() {
	render(
		<ToastProvider>
			<AlertsPanel />
		</ToastProvider>,
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
	server.use(
		http.get("/api/settings", () => HttpResponse.json(settings)),
		http.get("/api/alert/events", () => HttpResponse.json(catalog)),
		http.get("/api/alert/status", () =>
			HttpResponse.json({ configured: true, reachable: true, healthy: true }),
		),
	);
});

it("renders the picker from the catalog and reflects the stored selection", async () => {
	renderPanel();
	const down = await screen.findByTestId("alert-event-health.down");
	expect(down).toBeChecked();
	expect(screen.getByTestId("alert-event-health.up")).not.toBeChecked();
	expect(
		screen.getByTestId("alert-event-config.sync_failed"),
	).not.toBeChecked();
	expect(screen.getByTestId("alert-enabled")).toBeChecked();
});

it("shows the stored target masked, never as raw text", async () => {
	renderPanel();
	const target = (await screen.findByLabelText(
		/notification target/i,
	)) as HTMLInputElement;
	expect(target.value).toBe("********");
	expect(target.type).toBe("password");
	// The plaintext secret is never sent to the client, so it must not appear.
	expect(document.body.textContent).not.toContain("tgram://");
});

it("sends a test: persists the config then posts to /api/alert/test", async () => {
	let putHit = false;
	let testHit = false;
	server.use(
		http.put("/api/settings", () => {
			putHit = true;
			return new HttpResponse(null, { status: 204 });
		}),
		http.post("/api/alert/test", () => {
			testHit = true;
			return new HttpResponse(null, { status: 204 });
		}),
	);

	renderPanel();
	const btn = await screen.findByTestId("alert-test");
	await userEvent.click(btn);

	await waitFor(() => expect(testHit).toBe(true));
	expect(putHit).toBe(true);
});
