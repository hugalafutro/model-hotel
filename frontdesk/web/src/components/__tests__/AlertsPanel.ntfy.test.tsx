import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ntfyAppriseURL } from "@web-shared/ntfy";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import type { AlertEventDef, AlertStatus, Settings } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
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
		http.get("/api/alert/targets", () => HttpResponse.json({ targets: [] })),
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
	expect(ntfyAppriseURL("https://ntfy.example.com", "secret-1")).toBe(
		"ntfys://ntfy.example.com/secret-1",
	);
	expect(ntfyAppriseURL("http://ntfy.lan:8080", "fleet")).toBe(
		"ntfy://ntfy.lan:8080/fleet",
	);
	// Invalid pairs compose to nothing.
	expect(ntfyAppriseURL("not a url", "topic")).toBe("");
	expect(ntfyAppriseURL("ftp://ntfy.example.com", "topic")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.example.com", "")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.example.com", "has space")).toBe("");
	expect(ntfyAppriseURL("https://ntfy.example.com", "has/slash")).toBe("");
});

// No target is stored, so the operator says which ntfy server to use: the field
// starts empty rather than pointing at a public server nobody chose.
it("starts with an empty server field and composes what the operator types", async () => {
	renderPanel();
	const serverInput = (await screen.findByLabelText(
		"ntfy server",
	)) as HTMLInputElement;
	expect(serverInput.value).toBe("");
	expect(screen.queryByDisplayValue("https://ntfy.sh")).toBeNull();

	// Button is disabled until a valid server/topic pair exists.
	const useBtn = screen.getByRole("button", { name: "Set as target" });
	expect(useBtn).toBeDisabled();
	await userEvent.type(screen.getByLabelText("ntfy topic"), "my-secret-topic");
	expect(useBtn).toBeDisabled();
	await userEvent.type(serverInput, "https://ntfy.example.com");

	expect(
		screen.getByText("ntfys://ntfy.example.com/my-secret-topic"),
	).toBeInTheDocument();
	expect(useBtn).toBeEnabled();

	await userEvent.click(useBtn);
	expect(screen.getByLabelText("Notification target(s)")).toHaveValue(
		"ntfys://ntfy.example.com/my-secret-topic",
	);
	expect(screen.getByTestId("alert-ntfy-appended")).toBeInTheDocument();
});

// A second phone joins the ntfy server already in use, so the field is prefilled
// from the stored target and the composed URL is appended, not substituted.
it("prefills the server from a stored ntfy target and appends the new topic", async () => {
	server.use(
		http.get("/api/alert/targets", () =>
			HttpResponse.json({ targets: ["ntfys://ntfy.example.com/first"] }),
		),
	);
	renderPanel();
	const serverInput = (await screen.findByLabelText(
		"ntfy server",
	)) as HTMLInputElement;
	expect(serverInput.value).toBe("https://ntfy.example.com");

	await userEvent.type(screen.getByLabelText("ntfy topic"), "second");
	await userEvent.click(screen.getByRole("button", { name: "Set as target" }));
	expect(screen.getByLabelText("Notification target(s)")).toHaveValue(
		"ntfys://ntfy.example.com/first; ntfys://ntfy.example.com/second",
	);
	expect(screen.getByTestId("alert-ntfy-appended")).toBeInTheDocument();

	// The note is a reminder to save, so the next edit clears it.
	await userEvent.type(screen.getByLabelText("Notification target(s)"), "x");
	expect(screen.queryByTestId("alert-ntfy-appended")).toBeNull();
});

it("recomposes for a self-hosted plain-http server", async () => {
	renderPanel();
	const serverInput = await screen.findByLabelText("ntfy server");
	await userEvent.type(serverInput, "http://ntfy.lan:8080");
	await userEvent.type(screen.getByLabelText("ntfy topic"), "fleet");
	expect(screen.getByText("ntfy://ntfy.lan:8080/fleet")).toBeInTheDocument();
});
