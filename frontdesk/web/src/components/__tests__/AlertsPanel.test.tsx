import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "i18next";
import { HttpResponse, http } from "msw";
import { expect, it } from "vitest";
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
	// Settings serves the stored targets masked; the card reads the plaintext
	// list from /api/alert/targets instead, so this value must never reach the UI.
	alert_apprise_targets: "********",
	alert_events: "health.down",
	oidc_enabled: false,
	oidc_issuer_url: "",
	oidc_client_id: "",
	oidc_client_secret: "",
	oidc_public_base_url: "",
	oidc_allowed_emails: "",
};

// The plaintext destinations /api/alert/targets serves, which is what both the
// destinations list and the manual field render.
const storedTargets = ["ntfys://ntfy.example.com/secret1"];

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
	targets?: string[];
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
		http.get("/api/alert/targets", () =>
			HttpResponse.json({ targets: opts?.targets ?? storedTargets }),
		),
	);
}

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

it("shows the stored targets as readable rows and in the manual text field", async () => {
	baseHandlers();
	renderPanel();
	const rows = await screen.findAllByTestId("alert-destination-row");
	expect(rows).toHaveLength(1);
	expect(rows[0]).toHaveTextContent("ntfy.example.com");
	expect(rows[0]).toHaveTextContent("secret1");
	// The manual field mirrors the same plaintext list, never the served mask.
	const target = screen.getByLabelText(
		/notification target/i,
	) as HTMLInputElement;
	expect(target.type).toBe("text");
	expect(target.value).toBe("ntfys://ntfy.example.com/secret1");
	expect(screen.queryByText("********")).toBeNull();
});

it("reports unreadable stored destinations instead of an empty list", async () => {
	baseHandlers();
	server.use(
		http.get("/api/alert/targets", () =>
			HttpResponse.json(
				{ code: "undecryptable", error: "decrypt failed" },
				{ status: 500 },
			),
		),
	);
	renderPanel();
	expect(
		await screen.findByTestId("alert-destinations-error"),
	).toHaveTextContent(/master key/i);
	expect(screen.queryByTestId("alert-destination-row")).toBeNull();
	expect(screen.getByTestId("alert-destinations-empty")).toBeInTheDocument();
});

// Any other read failure is generic: the reason is not something the operator
// can act on, and the card must still render the rest of the configuration.
it("falls back to a generic message when the destinations read fails", async () => {
	baseHandlers();
	server.use(
		http.get(
			"/api/alert/targets",
			() => new HttpResponse(null, { status: 500 }),
		),
	);
	renderPanel();
	expect(
		await screen.findByTestId("alert-destinations-error"),
	).toHaveTextContent(/something went wrong/i);
	expect(screen.getByTestId("alert-destinations-empty")).toBeInTheDocument();
});

// The manual field mirrors the destination read, so a failed read leaves it
// empty. Saving must not write that emptiness over the stored ciphertext: the
// key is left out of the PUT and the server's partial merge keeps it.
it("omits the destinations from the PUT when the stored list could not be read", async () => {
	let putBody: Record<string, unknown> | undefined;
	baseHandlers();
	server.use(
		http.get(
			"/api/alert/targets",
			() => new HttpResponse(null, { status: 500 }),
		),
		http.put("/api/settings", async ({ request }) => {
			putBody = (await request.json()) as Record<string, unknown>;
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();
	await screen.findByTestId("alert-destinations-error");
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);

	await waitFor(() => expect(putBody).toBeDefined());
	expect(putBody).not.toHaveProperty("alert_apprise_targets");
	// Everything else the card owns is still written.
	expect(putBody?.alert_apprise_api_url).toBe("http://apprise:8000");
	expect(putBody?.alert_events).toBe("health.down");
});

// An unreadable stored list is also how a rotated master key looks, and the way
// out is to retype the destinations. What was typed is a deliberate rewrite, so
// it is written even though the read failed.
it("writes a retyped destination even when the stored list could not be read", async () => {
	let putBody: Record<string, unknown> | undefined;
	baseHandlers();
	server.use(
		http.get(
			"/api/alert/targets",
			() => new HttpResponse(null, { status: 500 }),
		),
		http.put("/api/settings", async ({ request }) => {
			putBody = (await request.json()) as Record<string, unknown>;
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();
	await screen.findByTestId("alert-destinations-error");
	await userEvent.type(
		screen.getByLabelText(/notification target/i),
		"ntfys://ntfy.example.com/x",
	);
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);

	await waitFor(() => expect(putBody).toBeDefined());
	expect(putBody?.alert_apprise_targets).toBe("ntfys://ntfy.example.com/x");
});

it("removing a row PUTs the remaining list", async () => {
	let putBody: Settings | undefined;
	baseHandlers();
	server.use(
		http.put("/api/settings", async ({ request }) => {
			putBody = (await request.json()) as Settings;
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();
	await userEvent.click(await screen.findByTestId("alert-destination-remove"));
	await userEvent.click(screen.getByTestId("alert-destination-remove-confirm"));

	await waitFor(() => expect(putBody).toBeDefined());
	// The only stored destination is gone, so the list persists as empty.
	expect(putBody?.alert_apprise_targets).toBe("");
});

it("surfaces a rejected removal instead of silently keeping the row", async () => {
	baseHandlers();
	server.use(
		http.put(
			"/api/settings",
			() =>
				new HttpResponse("frontdesk: validation failed: bad target", {
					status: 400,
				}),
		),
	);
	renderPanel();
	await userEvent.click(await screen.findByTestId("alert-destination-remove"));
	await userEvent.click(screen.getByTestId("alert-destination-remove-confirm"));

	expect(await screen.findByRole("alert")).toHaveTextContent(/bad target/i);
	expect(screen.getByTestId("alert-destination-row")).toBeInTheDocument();
});

// The rows describe what is stored, so an unsaved edit to the manual field puts
// them out of sync: both row actions wait until the edit is saved (or undone).
it("blocks the row actions while the manual targets field is dirty", async () => {
	baseHandlers();
	server.use(
		http.put("/api/settings", () => new HttpResponse(null, { status: 204 })),
	);
	renderPanel();
	await screen.findByTestId("alert-destination-row");
	expect(screen.getByTestId("alert-destination-remove")).toBeEnabled();
	expect(screen.queryByTestId("alert-destinations-dirty")).toBeNull();

	// Appending through the ntfy helper leaves the field ahead of the stored list.
	await userEvent.type(screen.getByLabelText(/ntfy topic/i), "second");
	await userEvent.click(screen.getByRole("button", { name: /set as target/i }));
	expect(screen.getByTestId("alert-destinations-dirty")).toBeInTheDocument();
	expect(screen.getByTestId("alert-destination-remove")).toBeDisabled();
	expect(screen.getByTestId("alert-destination-test")).toBeDisabled();

	// Saving re-reads the stored list, which puts the field back in sync.
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);
	await waitFor(() =>
		expect(screen.getByTestId("alert-destination-remove")).toBeEnabled(),
	);
	expect(screen.queryByTestId("alert-destinations-dirty")).toBeNull();
});

it("tests a single row without persisting the form", async () => {
	let testBody: { targets?: string[] } | undefined;
	let putHit = false;
	let statusReads = 0;
	baseHandlers();
	server.use(
		http.get("/api/alert/status", () => {
			statusReads += 1;
			return HttpResponse.json(okStatus);
		}),
		http.put("/api/settings", () => {
			putHit = true;
			return new HttpResponse(null, { status: 204 });
		}),
		http.post("/api/alert/test", async ({ request }) => {
			testBody = (await request.json()) as { targets?: string[] };
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();
	await userEvent.click(await screen.findByTestId("alert-destination-test"));

	await waitFor(() => expect(testBody).toBeDefined());
	expect(testBody?.targets).toEqual(["ntfys://ntfy.example.com/secret1"]);
	expect(putHit).toBe(false);
	// The probe result is re-read afterwards, so the pill reflects the attempt.
	await waitFor(() => expect(statusReads).toBeGreaterThan(1));
});

// A 502 from the test endpoint carries a machine-readable code; the card turns
// it into an actionable sentence rather than the generic fallback.
it("explains a coded delivery failure on a row test", async () => {
	baseHandlers();
	server.use(
		http.post("/api/alert/test", () =>
			HttpResponse.json(
				{ code: "deliver_failed", error: "apprise-api returned status 424" },
				{ status: 502 },
			),
		),
	);
	renderPanel();
	await userEvent.click(await screen.findByTestId("alert-destination-test"));

	const alert = await screen.findByRole("alert");
	expect(alert).toHaveTextContent(/could not deliver/i);
	expect(alert.textContent).not.toContain("424");
});

it("save writes the plaintext targets and the current selection", async () => {
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
	// The plaintext list round-trips; the served mask is never written back.
	expect(putBody?.alert_apprise_targets).toBe(
		"ntfys://ntfy.example.com/secret1",
	);
	expect(putBody?.alert_enabled).toBe(true);
	expect(putBody?.alert_events).toBe("health.down");
});

// Every editable field feeds the PUT: the URL and target inputs, the event
// checkboxes, and the ntfy helper composing a target from server + topic.
it("saves the edited URL, a composed ntfy target, and a toggled event", async () => {
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

	const url = screen.getByLabelText(/apprise api url/i);
	await userEvent.clear(url);
	await userEvent.type(url, "http://apprise:9000");

	// Typing a raw target replaces the loaded list...
	const target = screen.getByLabelText(/notification target/i);
	await userEvent.clear(target);
	await userEvent.type(target, "tgram://tok/chat");
	// ...and the ntfy helper appends the composed Apprise URL to it.
	await userEvent.type(screen.getByLabelText(/ntfy topic/i), "fleet-alerts");
	await userEvent.click(screen.getByRole("button", { name: /set as target/i }));
	expect(target).toHaveValue(
		"tgram://tok/chat; ntfys://ntfy.example.com/fleet-alerts",
	);

	await userEvent.click(
		screen.getByRole("checkbox", { name: "Member recovered" }),
	);
	await userEvent.click(
		screen.getByRole("checkbox", { name: "Member went down" }),
	);

	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);

	await waitFor(() => expect(putBody).toBeDefined());
	expect(putBody?.alert_apprise_api_url).toBe("http://apprise:9000");
	expect(putBody?.alert_apprise_targets).toBe(
		"tgram://tok/chat; ntfys://ntfy.example.com/fleet-alerts",
	);
	expect(putBody?.alert_events).toBe("health.up");
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

it("confirms a delivered test with a success toast and no error", async () => {
	baseHandlers();
	server.use(
		http.put("/api/settings", () => new HttpResponse(null, { status: 204 })),
		http.post("/api/alert/test", () => new HttpResponse(null, { status: 204 })),
	);
	renderPanel();
	await screen.findByRole("checkbox", { name: "Member went down" });
	await userEvent.click(screen.getByRole("button", { name: /send test/i }));

	// Compared against the resolved translation so the assertion pins which
	// toast fired without depending on the active locale.
	await waitFor(() =>
		expect(screen.getByRole("status")).toHaveTextContent(
			i18n.t("settings.alerts.testSent"),
		),
	);
	expect(screen.queryByRole("alert")).toBeNull();
});

// Like the SSO card, a switched-off Alerts card is just its toggle: the
// connection fields, the ntfy helper, the event picker and the test button only
// appear once outbound alerts are enabled, so a fleet that never turned them on
// is not looking at a tall form of blank inputs.
it("rolls the configuration up while alerts are disabled and unrolls on enable", async () => {
	let putBody: Settings | undefined;
	baseHandlers({ settings: { ...settings, alert_enabled: false } });
	server.use(
		http.put("/api/settings", async ({ request }) => {
			putBody = (await request.json()) as Settings;
			return new HttpResponse(null, { status: 204 });
		}),
	);
	renderPanel();

	const toggle = await screen.findByRole("checkbox", {
		name: /outbound alert notifications/i,
	});
	expect(toggle).not.toBeChecked();
	expect(screen.queryByLabelText(/apprise api url/i)).toBeNull();
	expect(
		screen.queryByRole("checkbox", { name: "Member went down" }),
	).toBeNull();
	expect(screen.queryByRole("button", { name: /send test/i })).toBeNull();
	// Save stays reachable so switching alerts off can be persisted, and a save
	// while rolled up still carries the stored URL, the stored destinations and
	// the event selection: hiding the fields must never blank what they held, or
	// toggling alerts off would silently wipe the operator's Apprise setup.
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);
	await waitFor(() => expect(putBody).toBeDefined());
	expect(putBody?.alert_enabled).toBe(false);
	expect(putBody?.alert_apprise_api_url).toBe(settings.alert_apprise_api_url);
	expect(putBody?.alert_apprise_targets).toBe(
		"ntfys://ntfy.example.com/secret1",
	);
	expect(putBody?.alert_events).toBe(settings.alert_events);

	await userEvent.click(toggle);
	expect(screen.getByLabelText(/apprise api url/i)).toBeInTheDocument();
	expect(
		screen.getByRole("checkbox", { name: "Member went down" }),
	).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: /send test/i }),
	).toBeInTheDocument();
});

// The card is the only way into the wizard, and the wizard is the only thing on
// the card that can write without a Save: cancelling it must leave the stored
// configuration exactly as it was.
it("opens the wizard from the card and cancels without writing", async () => {
	let putHit = false;
	baseHandlers();
	server.use(
		http.put("/api/settings", () => {
			putHit = true;
			return new HttpResponse(null, { status: 204 });
		}),
		http.post("/api/alert/probe", () => HttpResponse.json(okStatus)),
	);
	renderPanel();

	// A card with a destination stored offers only "Add destination", which
	// enters at step 2 and skips straight to picking a destination kind.
	const add = await screen.findByTestId("alert-wizard-add");
	expect(add).toHaveTextContent(
		i18n.t("settings.alerts.wizard.addDestination"),
	);
	expect(screen.queryByTestId("alert-wizard-open")).toBeNull();
	await userEvent.click(add);
	expect(screen.getByTestId("wiz-step-2")).toBeInTheDocument();
	await userEvent.click(screen.getByTestId("wiz-cancel"));
	expect(screen.queryByTestId("wiz-step-2")).toBeNull();
	expect(putHit).toBe(false);
});

it("offers only Set up alerts while nothing is stored", async () => {
	baseHandlers({
		settings: { ...settings, alert_apprise_api_url: "" },
		status: { configured: false, reachable: false, healthy: false },
		targets: [],
	});
	renderPanel();
	expect(await screen.findByTestId("alert-wizard-open")).toHaveTextContent(
		i18n.t("settings.alerts.wizard.open"),
	);
	// "Add destination" enters at step 2, which only makes sense once there is a
	// configuration to append a destination to.
	expect(screen.queryByTestId("alert-wizard-add")).toBeNull();
});

// The wizard writes on its own, so the card cannot mirror what it sent: it has
// to go and read what actually landed, or the rows and the manual field keep
// describing the configuration from before the run.
it("reloads the stored configuration after the wizard finishes", async () => {
	let written = "";
	baseHandlers();
	server.use(
		http.post("/api/alert/probe", () => HttpResponse.json(okStatus)),
		http.post("/api/alert/test", () => new HttpResponse(null, { status: 204 })),
		http.put("/api/settings", async ({ request }) => {
			written = ((await request.json()) as Settings).alert_apprise_targets;
			return new HttpResponse(null, { status: 204 });
		}),
		// The reads after the write serve what the wizard just stored.
		http.get("/api/alert/targets", () =>
			HttpResponse.json({
				targets: written === "" ? storedTargets : written.split("; "),
			}),
		),
	);
	renderPanel();
	await userEvent.click(await screen.findByTestId("alert-wizard-add"));

	await userEvent.click(await screen.findByTestId("wiz-kind-telegram"));
	await userEvent.click(screen.getByTestId("wiz-next"));
	await userEvent.type(screen.getByTestId("wiz-field-token"), "tok");
	await userEvent.type(screen.getByTestId("wiz-field-chat_id"), "chat");
	await userEvent.click(screen.getByTestId("wiz-next"));
	await userEvent.click(screen.getByTestId("wiz-send-test"));
	await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 5
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 6
	await userEvent.click(screen.getByTestId("wiz-next")); // -> 7
	await userEvent.click(screen.getByTestId("wiz-finish"));
	await userEvent.click(await screen.findByTestId("wiz-close"));

	expect(written).toBe("ntfys://ntfy.example.com/secret1; tgram://tok/chat");
	await waitFor(() =>
		expect(screen.getAllByTestId("alert-destination-row")).toHaveLength(2),
	);
	// The manual field is re-derived from the freshly read list, so it is not
	// left holding the pre-wizard value and reported as a pending edit.
	expect(screen.getByLabelText(/notification target/i)).toHaveValue(written);
	expect(screen.queryByTestId("alert-destinations-dirty")).toBeNull();
	expect(screen.getByRole("status")).toHaveTextContent(
		i18n.t("settings.alerts.saved"),
	);
});

// The wizard finishes by writing "the stored destinations plus what it added",
// so it must be handed a list that is both readable and current. An unreadable
// one would be saved as the additions alone, silently dropping what is stored.
it("refuses to open the wizard while the stored destinations cannot be read", async () => {
	baseHandlers();
	server.use(
		http.get("/api/alert/targets", () =>
			HttpResponse.json(
				{ code: "undecryptable", error: "decrypt failed" },
				{ status: 500 },
			),
		),
	);
	renderPanel();
	await screen.findByTestId("alert-destinations-error");
	// An unreadable list serves no destinations, so the card is back to its
	// first-run button, and that one is the one that has to be blocked.
	expect(screen.getByTestId("alert-wizard-open")).toBeDisabled();
	expect(screen.getByTestId("alert-wizard-open")).toHaveAttribute(
		"title",
		i18n.t("settings.alerts.destinationsError"),
	);
});

// Same reasoning for a pending manual edit: the wizard would carry the list
// from before the edit into its write, quietly undoing it.
it("refuses to open the wizard while the manual targets field is dirty", async () => {
	baseHandlers();
	server.use(
		http.put("/api/settings", () => new HttpResponse(null, { status: 204 })),
	);
	renderPanel();
	await screen.findByTestId("alert-destination-row");
	expect(screen.getByTestId("alert-wizard-add")).toBeEnabled();

	await userEvent.type(screen.getByLabelText(/ntfy topic/i), "second");
	await userEvent.click(screen.getByRole("button", { name: /set as target/i }));
	expect(screen.getByTestId("alert-wizard-add")).toBeDisabled();
	expect(screen.getByTestId("alert-wizard-add")).toHaveAttribute(
		"title",
		i18n.t("settings.alerts.destinationsDirty"),
	);

	// Saving puts the field back in sync, which unblocks the guided path again.
	await userEvent.click(
		screen.getByRole("button", { name: /save alert settings/i }),
	);
	await waitFor(() =>
		expect(screen.getByTestId("alert-wizard-add")).toBeEnabled(),
	);
});

it("points at set-up next to the Not configured pill", async () => {
	baseHandlers({
		status: { configured: false, reachable: false, healthy: false },
	});
	renderPanel();
	expect(await screen.findByTestId("alert-status-hint")).toBeInTheDocument();
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
		http.get("/api/alert/targets", () =>
			HttpResponse.json({ targets: storedTargets }),
		),
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

// The guided button is offered whether or not alerts are switched on, and an
// unreadable destination list is what greys it out. The reason therefore has to
// live outside the toggle's block, or a switched-off card shows a disabled
// button with no explanation anywhere on screen.
it("shows why the guided button is greyed out even with alerts switched off", async () => {
	baseHandlers({ settings: { ...settings, alert_enabled: false } });
	server.use(
		http.get(
			"/api/alert/targets",
			() => new HttpResponse(null, { status: 500 }),
		),
	);
	renderPanel();

	expect(
		await screen.findByTestId("alert-destinations-error"),
	).toHaveTextContent(/something went wrong/i);
	expect(screen.getByTestId("alert-wizard-open")).toBeDisabled();
	// The configuration itself is still folded away behind the toggle.
	expect(screen.queryByTestId("alert-destination-row")).toBeNull();
});

// The probe's reason code is the translated, actionable half of the result; the
// detail is raw server text. The note shows the reason and keeps the detail as
// the tooltip for whoever wants the literal answer.
it("shows the translated probe reason beside the pill, with the raw detail as its tooltip", async () => {
	baseHandlers({
		status: {
			configured: true,
			reachable: false,
			healthy: false,
			reason: "unreachable",
			detail: "dial tcp 172.18.0.5:8000: connect: connection refused",
		},
	});
	renderPanel();

	const note = await screen.findByTestId("alert-status-note");
	expect(note).toHaveTextContent(i18n.t("settings.alerts.reason.unreachable"));
	expect(
		screen.getByText(i18n.t("settings.alerts.statusUnreachable")),
	).toHaveAttribute(
		"title",
		"dial tcp 172.18.0.5:8000: connect: connection refused",
	);
});

// A reason Front Desk has no sentence for must not render its own translation
// key, so the raw detail is the fallback.
it("falls back to the raw detail when the probe reason has no translation", async () => {
	baseHandlers({
		status: {
			configured: true,
			reachable: true,
			healthy: false,
			reason: "something_new",
			detail: "apprise-api returned status 417",
		},
	});
	renderPanel();

	expect(await screen.findByTestId("alert-status-note")).toHaveTextContent(
		"apprise-api returned status 417",
	);
});
