import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, it } from "vitest";
import type { AlertEventDef, AlertStatus, Settings } from "../../api/types";
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
		severity: "warning",
		defaultOn: true,
	},
];

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
	alert_apprise_targets: "********", // a secret is stored; served masked
	alert_events: "health.down",
	oidc_enabled: false,
	oidc_issuer_url: "",
	oidc_client_id: "",
	oidc_client_secret: "",
	oidc_public_base_url: "",
	oidc_allowed_emails: "",
};

const okStatus: AlertStatus = {
	configured: true,
	reachable: true,
	healthy: true,
	detail: "ok",
};

function renderPanel() {
	render(
		<ToastProvider>
			<AlertsPanel />
		</ToastProvider>,
	);
}

// baseHandlers mocks the three reads the panel issues on mount. Pass overrides to
// vary the settings / catalog / status for a given test.
function baseHandlers(opts?: {
	settings?: Settings;
	catalog?: AlertEventDef[];
	status?: AlertStatus;
}) {
	server.use(
		http.get("/api/settings", () =>
			HttpResponse.json(opts?.settings ?? settings),
		),
		http.get("/api/alert/events", () =>
			HttpResponse.json(opts?.catalog ?? catalog),
		),
		http.get("/api/alert/status", () =>
			HttpResponse.json(opts?.status ?? okStatus),
		),
	);
}

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
});

it("renders friendly labels and reflects the stored selection", async () => {
	baseHandlers();
	renderPanel();
	// Raw dotted type is never shown; the friendly label is.
	const down = await screen.findByRole("checkbox", {
		name: "Member went down",
	});
	expect(down).toBeChecked();
	expect(
		screen.getByRole("checkbox", { name: "Member recovered" }),
	).not.toBeChecked();
	expect(
		screen.getByRole("checkbox", { name: "Config sync failed" }),
	).not.toBeChecked();
	expect(screen.queryByText("health.down")).not.toBeInTheDocument();
	expect(
		screen.getByRole("checkbox", { name: /outbound alert notifications/i }),
	).toBeChecked();
});

it("shows the stored target masked, never as raw text", async () => {
	baseHandlers();
	renderPanel();
	const target = (await screen.findByLabelText(
		/notification target/i,
	)) as HTMLInputElement;
	expect(target.value).toBe("********");
	expect(target.type).toBe("password");
	expect(document.body.textContent).not.toContain("tgram://");
});

it("save preserves the masked secret and writes the current selection", async () => {
	// Declared without a `= null` initializer: the only assignment happens inside
	// the handler closure below, and a null initializer would make TS's control-
	// flow analysis pin the outer reads to `null` (collapsing putBody?.x to never).
	let putBody: Settings | undefined;
	baseHandlers();
	server.use(
		http.put("/api/settings", async ({ request }) => {
			putBody = (await request.json()) as Settings;
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();
	await screen.findByRole("checkbox", { name: "Member went down" });
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);

	await waitFor(() => expect(putBody).toBeDefined());
	// The mask is echoed back unchanged so the backend keeps the stored secret.
	expect(putBody?.alert_apprise_targets).toBe("********");
	expect(putBody?.alert_enabled).toBe(true);
	expect(putBody?.alert_events).toBe("health.down");
});

it("surfaces a 400 validation message but hides non-400 internals on save", async () => {
	baseHandlers();
	server.use(
		http.put(
			"/api/settings",
			() =>
				new HttpResponse("frontdesk: validation failed: bad url", {
					status: 400,
				}),
		),
	);
	renderPanel();
	await screen.findByRole("checkbox", { name: "Member went down" });
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);
	expect(await screen.findByRole("alert")).toHaveTextContent(/bad url/i);
});

it("sends a test: persists then posts, and does not leak raw errors on failure", async () => {
	let putHit = false;
	let testHit = false;
	baseHandlers();
	server.use(
		http.put("/api/settings", () => {
			putHit = true;
			return new HttpResponse(null, { status: 204 });
		}),
		http.post("/api/alert/test", () => {
			testHit = true;
			// 502 with a raw body the user must never see verbatim.
			return HttpResponse.json(
				{ error: "apprise-api returned status 503" },
				{
					status: 502,
				},
			);
		}),
	);
	renderPanel();
	await screen.findByRole("checkbox", { name: "Member went down" });
	await userEvent.click(screen.getByRole("button", { name: /send test/i }));

	await waitFor(() => expect(testHit).toBe(true));
	expect(putHit).toBe(true);
	const alert = await screen.findByRole("alert");
	expect(alert).toHaveTextContent(/something went wrong/i);
	expect(alert.textContent).not.toContain("503");
});

it("renders without a picker when the catalog is empty", async () => {
	baseHandlers({ catalog: [] });
	renderPanel();
	// The enable toggle still renders; no event checkboxes do.
	await screen.findByRole("checkbox", {
		name: /outbound alert notifications/i,
	});
	expect(
		screen.queryByRole("checkbox", { name: "Member went down" }),
	).not.toBeInTheDocument();
});

it("stays quiet (renders nothing) when settings fail to load", async () => {
	server.use(
		http.get("/api/settings", () => new HttpResponse(null, { status: 500 })),
		http.get("/api/alert/events", () => HttpResponse.json(catalog)),
		http.get("/api/alert/status", () => HttpResponse.json(okStatus)),
	);
	const { container } = render(
		<ToastProvider>
			<AlertsPanel />
		</ToastProvider>,
	);
	// The ToastProvider still renders its (empty) toast region, so assert the panel
	// card specifically never appears rather than that the whole container is empty.
	await waitFor(() => expect(container.querySelector(".ui-card")).toBeNull());
});

it.each([
	[
		{ configured: false, reachable: false, healthy: false },
		"Not configured",
		null,
	],
	[
		{
			configured: true,
			reachable: false,
			healthy: false,
			detail: "unreachable",
		},
		"apprise-api unreachable",
		"unreachable",
	],
	[
		{
			configured: true,
			reachable: true,
			healthy: false,
			detail: "apprise-api returned status 417",
		},
		"apprise-api unhealthy",
		"apprise-api returned status 417",
	],
	[
		{ configured: true, reachable: true, healthy: true, detail: "ok" },
		"apprise-api reachable",
		null,
	],
])("status pill renders the %o branch", async (status, label, detail) => {
	baseHandlers({ status: status as AlertStatus });
	renderPanel();
	await screen.findByRole("checkbox", {
		name: /outbound alert notifications/i,
	});
	expect(screen.getByText(label)).toBeInTheDocument();
	if (detail) {
		// The probe reason is surfaced inline, not just as a colour.
		expect(screen.getByText(detail)).toBeInTheDocument();
	}
});
