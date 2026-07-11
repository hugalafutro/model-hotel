import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import type { AlertEventDef, AlertStatus, Settings } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { ntfyAppriseURL } from "../../utils/ntfy";
import { AlertsPanel } from "../AlertsPanel";

// The phone-push (ntfy) convenience block inside the Alerts panel: composes an
// Apprise ntfy URL from server + topic and sets it as the target field value.

const settings: Settings = {
	health_poll_secs: 5,
	traefik_poll_secs: 5,
	traefik_stale_secs: 30,
	event_retention_days: 90,
	retry_attempts: 2,
	health_fail_threshold: 3,
	session_idle_timeout_minutes: 60,
	alert_enabled: true,
	alert_apprise_api_url: "http://apprise:8000",
	alert_apprise_targets: "",
	alert_events: "",
	oidc_enabled: false,
	oidc_issuer_url: "",
	oidc_client_id: "",
	oidc_client_secret: "",
	oidc_public_base_url: "",
	oidc_allowed_emails: "",
};

const catalog: AlertEventDef[] = [
	{
		type: "health.down",
		category: "Health",
		severity: "error",
		defaultOn: true,
	},
];

const okStatus: AlertStatus = {
	configured: true,
	reachable: true,
	healthy: true,
	detail: "ok",
};

beforeEach(() => {
	server.resetHandlers();
	server.use(
		http.get("/api/settings", () => HttpResponse.json(settings)),
		http.get("/api/alert/events", () => HttpResponse.json(catalog)),
		http.get("/api/alert/status", () => HttpResponse.json(okStatus)),
	);
});

function renderPanel() {
	render(
		<ToastProvider>
			<AlertsPanel />
		</ToastProvider>,
	);
}

it("composes ntfy Apprise URLs from server scheme and topic", () => {
	expect(ntfyAppriseURL("https://ntfy.sh", "secret-1")).toBe(
		"ntfys://ntfy.sh/secret-1",
	);
	expect(ntfyAppriseURL("http://ntfy.lan:8080", "fleet")).toBe(
		"ntfy://ntfy.lan:8080/fleet",
	);
	// Invalid pairs compose to nothing.
	expect(ntfyAppriseURL("not a url", "topic")).toBe("");
	expect(ntfyAppriseURL("ftp://ntfy.sh", "topic")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.sh", "")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.sh", "has space")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.sh", "has/slash")).toBe("");
});

it("previews the composed URL and sets it as the notification target", async () => {
	renderPanel();
	const topic = await screen.findByLabelText("ntfy topic");

	// Button is disabled until a valid topic exists.
	const useBtn = screen.getByRole("button", { name: "Set as target" });
	expect(useBtn).toBeDisabled();

	await userEvent.type(topic, "my-secret-topic");
	expect(
		screen.getByText("ntfys://ntfy.sh/my-secret-topic"),
	).toBeInTheDocument();
	expect(useBtn).toBeEnabled();

	await userEvent.click(useBtn);
	expect(screen.getByLabelText("Notification target(s)")).toHaveValue(
		"ntfys://ntfy.sh/my-secret-topic",
	);
});

it("recomposes for a self-hosted plain-http server", async () => {
	renderPanel();
	const serverInput = await screen.findByLabelText("ntfy server");
	await userEvent.clear(serverInput);
	await userEvent.type(serverInput, "http://ntfy.lan:8080");
	await userEvent.type(screen.getByLabelText("ntfy topic"), "fleet");
	expect(screen.getByText("ntfy://ntfy.lan:8080/fleet")).toBeInTheDocument();
});
