import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import i18n from "../../../i18n";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AlertsSettings } from "../AlertsSettings";

function mockSettings(values: Record<string, string>) {
	server.use(
		http.get("/api/settings", ({ request }) => {
			if (!request.headers.get("Cookie")?.includes("mh_csrf=")) {
				return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
			}
			return HttpResponse.json(values);
		}),
	);
}

// mockTargets serves the decrypted destination list GET /api/alert/targets
// returns; the settings row keeps its "********" mask so a test that asserts
// plaintext proves the card reads the list, not the mask.
function mockTargets(targets: string[]) {
	server.use(
		http.get("/api/alert/targets", () => HttpResponse.json({ targets })),
	);
}

// toastText is the message of the toast currently on screen. Tests compare it
// with i18n.t(...) rather than a literal so they hold in any locale.
function toastText() {
	return screen.getByTestId("toast-message").textContent;
}

// failTargets makes the destination read fail the way an unreadable stored
// value does after a master-key rotation.
function failTargets(code?: string) {
	server.use(
		http.get("/api/alert/targets", () =>
			HttpResponse.json({ code, error: "boom" }, { status: 500 }),
		),
	);
}

describe("AlertsSettings", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("renders the section header", () => {
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		expect(screen.getByText("Alerts")).toBeInTheDocument();
		expect(
			screen.getByText(/Push notifications for noteworthy events/i),
		).toBeInTheDocument();
	});

	it("hides config inputs when alerting is disabled", async () => {
		mockSettings({ alert_enabled: "false" });
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Enable alerting")).toBeInTheDocument();
		});
		expect(screen.queryByTestId("alert-api-url-input")).not.toBeInTheDocument();
		expect(screen.queryByTestId("alert-test-button")).not.toBeInTheDocument();
		// The guided run switches nothing on by itself: it is offered once the
		// toggle is, so a switched-off card has no way into it.
		expect(screen.queryByTestId("alert-wizard-open")).not.toBeInTheDocument();
		expect(screen.queryByTestId("alert-wizard-add")).not.toBeInTheDocument();
	});

	it("shows inputs and the stored destinations in clear when enabled", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat", "ntfys://ntfy.example.com/topic1"]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		// The manual field mirrors the decrypted list, so what is on screen is
		// exactly what is stored; the settings row's mask never reaches the UI.
		await waitFor(() =>
			expect(screen.getByTestId("alert-target-input")).toHaveValue(
				"tgram://tok/chat; ntfys://ntfy.example.com/topic1",
			),
		);
		expect(screen.queryByText("********")).toBeNull();
		expect(screen.getByTestId("alert-api-url-input")).toHaveValue(
			"http://apprise:8000",
		);
		// Configured + enabled => test button is enabled.
		expect(screen.getByTestId("alert-test-button")).toBeEnabled();
		// A clear button appears for the configured secret.
		expect(screen.getByTestId("alert-target-clear")).toBeInTheDocument();
	});

	it("renders one readable row per stored destination", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat", "ntfys://ntfy.example.com/topic1"]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const rows = await screen.findAllByTestId("alert-destination-row");
		expect(rows).toHaveLength(2);
		expect(rows[0]).toHaveTextContent("api.telegram.org");
		expect(rows[1]).toHaveTextContent("topic1");
	});

	it("writes the remaining list when a destination row is removed", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat", "ntfys://ntfy.example.com/topic1"]);
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const rows = await screen.findAllByTestId("alert-destination-row");
		await user.click(within(rows[0]).getByTestId("alert-destination-remove"));
		await user.click(screen.getByTestId("alert-destination-remove-confirm"));

		await waitFor(() =>
			expect(put.body).toEqual({
				alert_apprise_targets: "ntfys://ntfy.example.com/topic1",
			}),
		);
	});

	it("clears the setting when the last destination row is removed", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat"]);
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-destination-remove"));
		await user.click(screen.getByTestId("alert-destination-remove-confirm"));

		await waitFor(() =>
			expect(put.body).toEqual({ alert_apprise_targets: "" }),
		);
	});

	it("tests one destination on its own", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat", "ntfys://ntfy.example.com/topic1"]);
		const sent: { body: unknown } = { body: null };
		server.use(
			http.post("/api/alert/test", async ({ request }) => {
				sent.body = await request.json();
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const rows = await screen.findAllByTestId("alert-destination-row");
		await user.click(within(rows[1]).getByTestId("alert-destination-test"));

		await waitFor(() =>
			expect(sent.body).toEqual({
				targets: ["ntfys://ntfy.example.com/topic1"],
			}),
		);
		expect(toastText()).toBe(i18n.t("settings.alerts.testSent"));
	});

	it("explains a row test failure with the reported reason", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat"]);
		server.use(
			http.post("/api/alert/test", () =>
				HttpResponse.json(
					{ code: "deliver_failed", error: "apprise-api could not deliver" },
					{ status: 502 },
				),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-destination-test"));
		await waitFor(() =>
			expect(toastText()).toBe(
				i18n.t("settings.alerts.testFailed", {
					message: i18n.t("settings.alerts.reason.deliver_failed"),
				}),
			),
		);
	});

	it("offers the full guided run when nothing is stored", async () => {
		mockSettings({ alert_enabled: "true" });
		mockTargets([]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-destinations-empty");
		expect(screen.getByTestId("alert-wizard-open")).toBeInTheDocument();
		expect(screen.queryByTestId("alert-wizard-add")).not.toBeInTheDocument();
	});

	it("offers adding a destination once one is stored", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat"]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-wizard-add");
		expect(screen.queryByTestId("alert-wizard-open")).not.toBeInTheDocument();
	});

	it("blocks the guided run and says why when the stored list cannot be read", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		failTargets("undecryptable");
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const line = await screen.findByTestId("alert-destinations-error");
		// Themed by what the box is, not by a palette utility, so it renders the
		// same way under all three UI styles.
		expect(line).toHaveClass("ui-callout", "ui-callout-warning");
		const button = screen.getByTestId("alert-wizard-open");
		expect(button).toBeDisabled();
		expect(button).toHaveAttribute("title", line.textContent as string);
		// The wizard would have to show a list it cannot read, so it does not open.
		await userEvent.setup().click(button);
		expect(screen.queryByTestId("wiz-step-1")).not.toBeInTheDocument();
	});

	// The guided run seeds its recommended events from the catalog once, when it
	// mounts, so it must not start before the catalog is there to seed from.
	it("holds the guided run back until the event list has loaded", async () => {
		let release = () => {};
		const gate = new Promise<void>((resolve) => {
			release = resolve;
		});
		mockSettings({ alert_enabled: "true" });
		mockTargets([]);
		server.use(
			http.get("/api/alert/events", async () => {
				await gate;
				return HttpResponse.json([
					{
						type: "circuit_breaker.open",
						category: "Failover",
						severity: "warning",
						defaultOn: true,
					},
				]);
			}),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const button = await screen.findByTestId("alert-wizard-open");
		expect(button).toBeDisabled();
		expect(button).toHaveAttribute(
			"title",
			i18n.t("settings.alerts.wizard.catalogLoading"),
		);

		release();
		await waitFor(() => expect(button).toBeEnabled());
	});

	it("says why the guided run is unavailable when the event list cannot be read", async () => {
		mockSettings({ alert_enabled: "true" });
		mockTargets([]);
		server.use(
			http.get(
				"/api/alert/events",
				() => new HttpResponse(null, { status: 500 }),
			),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const button = await screen.findByTestId("alert-wizard-open");
		await waitFor(() =>
			expect(button).toHaveAttribute(
				"title",
				i18n.t("settings.alerts.wizard.catalogUnavailable"),
			),
		);
		// Starting without it would offer an empty recommended preset, so the run
		// waits for a reload rather than writing a selection nobody made.
		expect(button).toBeDisabled();
	});

	it("opens the guided run and writes nothing when it is cancelled", async () => {
		mockSettings({ alert_enabled: "true" });
		mockTargets([]);
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const open = await screen.findByTestId("alert-wizard-open");
		await waitFor(() => expect(open).toBeEnabled());
		await user.click(open);
		// Nothing is stored, so the run starts at the apprise address.
		await screen.findByTestId("wiz-step-1");

		await user.click(screen.getByTestId("wiz-cancel"));
		expect(screen.queryByTestId("wiz-step-1")).not.toBeInTheDocument();
		expect(put.body).toBeNull();
	});

	it("starts the add-destination run at the destination step", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat"]);
		server.use(
			http.post("/api/alert/probe", () =>
				HttpResponse.json({ configured: true, reachable: true, healthy: true }),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const add = await screen.findByTestId("alert-wizard-add");
		await waitFor(() => expect(add).toBeEnabled());
		await user.click(add);
		await screen.findByTestId("wiz-step-2");
	});

	it("re-reads its own state and says so when the guided run finishes", async () => {
		let settingsReads = 0;
		server.use(
			http.get("/api/settings", () => {
				settingsReads += 1;
				return HttpResponse.json({ alert_enabled: "true" });
			}),
			http.post("/api/alert/probe", () =>
				HttpResponse.json({ configured: true, reachable: true, healthy: true }),
			),
			http.post("/api/alert/test", () => HttpResponse.json({ ok: true })),
		);
		const reads = countTargetReads();
		capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const open = await screen.findByTestId("alert-wizard-open");
		await waitFor(() => expect(open).toBeEnabled());
		await user.click(open);
		await screen.findByTestId("wiz-step-1");
		await user.click(screen.getByTestId("wiz-api-check"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await user.click(screen.getByTestId("wiz-next")); // -> 2
		await user.click(screen.getByTestId("wiz-kind-other"));
		await user.click(screen.getByTestId("wiz-next")); // -> 3
		await user.type(screen.getByTestId("wiz-field-url"), "tgram://tok/chat");
		await user.click(screen.getByTestId("wiz-next")); // -> 4
		await user.click(screen.getByTestId("wiz-send-test"));
		await waitFor(() => expect(screen.getByTestId("wiz-next")).toBeEnabled());
		await user.click(screen.getByTestId("wiz-next")); // -> 5
		await user.click(screen.getByTestId("wiz-next")); // -> 6
		await screen.findByTestId("alert-event-picker");
		await user.click(screen.getByTestId("wiz-next")); // -> 7

		const settledSettings = settingsReads;
		const settledTargets = reads.n;
		await user.click(screen.getByTestId("wiz-finish"));
		await screen.findByTestId("wiz-done");
		await user.click(screen.getByTestId("wiz-close"));

		// The dialog is gone, the card's own copy of both reads is dropped, and
		// the write it made is reported the way every other save on this page is.
		expect(screen.queryByTestId("wiz-done")).not.toBeInTheDocument();
		expect(toastText()).toBe(i18n.t("settings.common.settingsSaved"));
		await waitFor(() => {
			expect(settingsReads).toBeGreaterThan(settledSettings);
			expect(reads.n).toBeGreaterThan(settledTargets + 1);
		});
	});

	it("reports an uncoded destination read failure generically", async () => {
		mockSettings({ alert_enabled: "true" });
		failTargets();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		expect(
			(await screen.findByTestId("alert-destinations-error")).textContent,
		).toBe(i18n.t("settings.alerts.destinations.readFailed"));
	});

	it("hides the destination list, not just the callout, when the read fails", async () => {
		// A 500 leaves `targets` at its empty-array fallback, which would
		// otherwise render the list's own "nothing configured" empty state
		// alongside the callout that already explains the read failed.
		mockSettings({ alert_enabled: "true" });
		failTargets();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-destinations-error");
		expect(
			screen.queryByTestId("alert-destinations-empty"),
		).not.toBeInTheDocument();
	});

	it("shows the destination read failure even when alerting is off", async () => {
		// A rotated master key is the one thing on this card the toggle does
		// nothing about, so it is reported in both states; the guided entry point
		// itself waits for the toggle.
		mockSettings({ alert_enabled: "false" });
		failTargets("undecryptable");
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-destinations-error");
		expect(screen.queryByTestId("alert-wizard-open")).not.toBeInTheDocument();
	});

	it("keeps the manual configuration behind the advanced disclosure", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const manual = within(await screen.findByTestId("alert-manual"));
		expect(manual.getByTestId("alert-api-url-input")).toBeInTheDocument();
		expect(manual.getByTestId("alert-target-input")).toBeInTheDocument();
		expect(manual.getByTestId("alert-test-button")).toBeInTheDocument();
		expect(manual.getByTestId("alert-snippets")).toBeInTheDocument();
		// The readable list stays outside it: it is the primary view.
		expect(
			manual.queryByTestId("alert-destinations-empty"),
		).not.toBeInTheDocument();
	});

	it("points at the guided run when no apprise address is set", async () => {
		mockSettings({ alert_enabled: "true" });
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		expect(await screen.findByTestId("alert-status-hint")).toBeInTheDocument();
	});

	// countTargetReads serves the destination list and counts how often it is
	// asked for, so a test can prove a write dropped the cached copy.
	function countTargetReads() {
		const reads = { n: 0 };
		server.use(
			http.get("/api/alert/targets", () => {
				reads.n += 1;
				return HttpResponse.json({ targets: [] });
			}),
		);
		return reads;
	}

	it("re-reads the destinations after a settings write", async () => {
		mockSettings({ alert_enabled: "true" });
		const reads = countTargetReads();
		capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => expect(reads.n).toBe(1));
		const input = await screen.findByTestId("alert-api-url-input");
		await user.type(input, "http://apprise:8000");
		await user.tab();

		await waitFor(() => expect(reads.n).toBeGreaterThan(1));
	});

	it("re-reads the destinations after a setting is reset", async () => {
		// A reset clears the Apprise address and targets just as an edit can, so
		// the decrypted list it produced is stale either way.
		mockSettings({ alert_enabled: "true" });
		const reads = countTargetReads();
		server.use(http.delete("/api/settings", () => HttpResponse.json({})));
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await waitFor(() => expect(reads.n).toBe(1));
		await user.click(
			screen.getAllByRole("button", {
				name: i18n.t("settings.common.resetSetting"),
			})[0],
		);

		await waitFor(() => expect(reads.n).toBeGreaterThan(1));
	});

	it("keeps the destinations and the guided run usable on a managed member", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		mockTargets(["tgram://tok/chat"]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} managed />,
		);

		// The Apprise address and its destinations are instance-local, so config
		// sync does not replicate them and they stay editable here.
		await screen.findByTestId("alert-wizard-add");
		expect(screen.getByTestId("alert-destination-remove")).toBeEnabled();
		expect(screen.getByTestId("alert-target-input")).toBeEnabled();
		expect(screen.getByTestId("alert-api-url-input")).toBeEnabled();
	});

	it("keeps the delivery settings reachable on a managed member with alerting off", async () => {
		// Alerting on/off is decided fleet-wide, so a managed member cannot
		// switch it on to reach its own delivery settings. Hiding them behind
		// the toggle the way an unmanaged card does would leave this member no
		// way to configure the address it is expected to deliver through.
		mockSettings({ alert_enabled: "false" });
		mockTargets([]);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} managed />,
		);

		// Enabled only once the destination read lands: until then the run would
		// snapshot an empty stored list.
		await waitFor(() =>
			expect(screen.getByTestId("alert-wizard-open")).toBeEnabled(),
		);
		expect(screen.getByTestId("alert-manual")).toBeInTheDocument();
		expect(screen.getByTestId("alert-api-url-input")).toBeEnabled();
		// The fleet-owned half stays locked whatever the toggle says.
		expect(
			screen.getByRole("switch", { name: "Enable alerting" }),
		).toBeDisabled();
	});

	it("reveals the event picker from the catalog API on toggle", async () => {
		mockSettings({ alert_enabled: "true" });
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const toggle = await screen.findByTestId("alert-picker-toggle");
		expect(screen.queryByTestId("alert-event-picker")).not.toBeInTheDocument();

		await user.click(toggle);

		await waitFor(() => {
			expect(screen.getByTestId("alert-event-picker")).toBeInTheDocument();
		});
		// Events come from the mocked /api/alert/events catalog.
		expect(
			screen.getByTestId("alert-event-circuit_breaker.open"),
		).toBeInTheDocument();
	});

	it("disables the test button until fully configured", async () => {
		mockSettings({ alert_enabled: "true" }); // no URL/target
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		const btn = await screen.findByTestId("alert-test-button");
		expect(btn).toBeDisabled();
	});

	// capturePut records the body of the next PUT /api/settings and returns a
	// getter for it.
	function capturePut() {
		const captured: { body: Record<string, string> | null } = { body: null };
		server.use(
			http.put("/api/settings", async ({ request }) => {
				captured.body = (await request.json()) as Record<string, string>;
				return HttpResponse.json(captured.body);
			}),
		);
		return captured;
	}

	it("saves the apprise-api URL on blur", async () => {
		mockSettings({ alert_enabled: "true" });
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const input = await screen.findByTestId("alert-api-url-input");
		await user.clear(input);
		await user.type(input, "http://apprise:8000");
		await user.tab(); // blur commits

		await waitFor(() =>
			expect(put.body).toEqual({
				alert_apprise_api_url: "http://apprise:8000",
			}),
		);
	});

	it("encrypts a new target on blur", async () => {
		mockSettings({ alert_enabled: "true" });
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const input = await screen.findByTestId("alert-target-input");
		await user.type(input, "tgram://tok/chat");
		await user.tab();

		await waitFor(() =>
			expect(put.body).toEqual({ alert_apprise_targets: "tgram://tok/chat" }),
		);
	});

	it("clears a configured target", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-target-clear"));
		await waitFor(() =>
			expect(put.body).toEqual({ alert_apprise_targets: "" }),
		);
	});

	it("sends a test notification and toasts success", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-test-button"));
		await waitFor(() =>
			expect(screen.getByText("Test notification sent.")).toBeInTheDocument(),
		);
	});

	it("writes the picker selection when an event is toggled off", async () => {
		mockSettings({ alert_enabled: "true" }); // unset alert_events => catalog defaults
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-picker-toggle"));
		const row = await screen.findByTestId("alert-event-circuit_breaker.open");
		await user.click(row.querySelector("input") as HTMLElement);

		// Default-on set is {open, closed}; turning open off leaves just closed.
		await waitFor(() =>
			expect(put.body).toEqual({ alert_events: "circuit_breaker.closed" }),
		);
	});

	it("reflects a stored event selection in the picker", async () => {
		// value-defined branch: only circuit_breaker.open selected.
		mockSettings({
			alert_enabled: "true",
			alert_events: "circuit_breaker.open",
		});
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-picker-toggle"));
		const open = (
			await screen.findByTestId("alert-event-circuit_breaker.open")
		).querySelector("input") as HTMLInputElement;
		const closed = screen
			.getByTestId("alert-event-circuit_breaker.closed")
			.querySelector("input") as HTMLInputElement;
		expect(open.checked).toBe(true);
		expect(closed.checked).toBe(false);
	});

	it("toasts an error when the test notification fails", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		server.use(
			http.post("/api/alert/test", () =>
				HttpResponse.json(
					{ code: "unreachable", error: "apprise-api unreachable" },
					{ status: 502 },
				),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-test-button"));
		// The decoded {code, error} body's code picks the translated, actionable
		// sentence; the server's own English text never reaches the toast.
		await waitFor(() =>
			expect(toastText()).toBe(
				i18n.t("settings.alerts.testFailed", {
					message: i18n.t("settings.alerts.reason.unreachable"),
				}),
			),
		);
	});

	it("toggles a whole category with select-all/none", async () => {
		mockSettings({ alert_enabled: "true" });
		const put = capturePut();
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-picker-toggle"));
		await screen.findByTestId("alert-event-picker");
		// Failover group starts all-on (open+closed default on); its select-all
		// icon toggles them all off. The single-event Discovery group has no toggle.
		expect(
			screen.queryByTestId("alert-group-toggle-Discovery"),
		).not.toBeInTheDocument();
		await user.click(screen.getByTestId("alert-group-toggle-Failover"));
		await waitFor(() => expect(put.body).not.toBeNull());
		// Every Failover event removed; discovery.provider_failed was already off.
		expect(put.body?.alert_events).toBe("");
	});

	it("shows the apprise-api reachable status", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		await waitFor(() =>
			expect(screen.getByText("apprise-api reachable")).toBeInTheDocument(),
		);
	});

	it("shows a reachable-but-unhealthy status", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
		});
		server.use(
			http.get("/api/alert/status", () =>
				HttpResponse.json({
					configured: true,
					reachable: true,
					healthy: false,
					detail: "apprise-api returned status 417",
				}),
			),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		await waitFor(() =>
			expect(
				screen.getByText(/reachable but reporting issues/i),
			).toBeInTheDocument(),
		);
	});

	it("shows an unreachable status when the probe fails", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
		});
		server.use(
			http.get("/api/alert/status", () =>
				HttpResponse.json({
					configured: true,
					reachable: false,
					healthy: false,
					reason: "unreachable",
					detail: "dial tcp 10.0.0.9:8000: connect: no route to host",
				}),
			),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		const row = await screen.findByTestId("alert-status");
		await waitFor(() =>
			expect(row).toHaveTextContent(
				i18n.t("settings.alerts.status.unreachable"),
			),
		);
		// The reason code is the translated, actionable half; the raw server text
		// stays a tooltip so it never becomes the message.
		expect(screen.getByTestId("alert-status-note").textContent).toBe(
			i18n.t("settings.alerts.reason.unreachable"),
		);
		expect(screen.queryByText(/no route to host/)).toBeNull();
	});

	// --- outstanding-discrepancy threshold -----------------------------------
	//
	// discovery_claim_window_days is served read-only by the backend
	// (ClaimWindow in days); the control's ceiling is one day below it, because
	// a discrepancy stops counting once it is older than the window and a
	// threshold at or above the window could therefore never fire.
	const CLAIM_WINDOW_DAYS = "30";
	const CEILING = Number(CLAIM_WINDOW_DAYS) - 1;

	// claimAgeInputs returns the control's number field and its slider, scoped
	// by testid so neither is found by translated label text.
	function claimAgeInputs() {
		const box = within(screen.getByTestId("alert-claim-age"));
		return {
			number: box.getByRole("spinbutton") as HTMLInputElement,
			slider: box.getByRole("slider") as HTMLInputElement,
		};
	}

	it("renders the stored threshold and a ceiling below the claim window", async () => {
		mockSettings({
			alert_enabled: "true",
			discovery_claim_window_days: CLAIM_WINDOW_DAYS,
			discovery_claim_alert_days: "12",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-claim-age");
		const { number, slider } = claimAgeInputs();
		expect(number).toHaveValue(12);
		expect(slider).toHaveAttribute("max", String(CEILING));
	});

	it("persists a new threshold", async () => {
		mockSettings({
			alert_enabled: "true",
			discovery_claim_window_days: CLAIM_WINDOW_DAYS,
			discovery_claim_alert_days: "7",
		});
		const put = capturePut();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-claim-age");
		const { number } = claimAgeInputs();
		fireEvent.change(number, { target: { value: "20" } });
		fireEvent.blur(number);

		await waitFor(() =>
			expect(put.body).toEqual({ discovery_claim_alert_days: "20" }),
		);
	});

	it("refuses a threshold above the ceiling and saves the ceiling instead", async () => {
		mockSettings({
			alert_enabled: "true",
			discovery_claim_window_days: CLAIM_WINDOW_DAYS,
			discovery_claim_alert_days: "7",
		});
		const put = capturePut();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-claim-age");
		const { number } = claimAgeInputs();
		fireEvent.change(number, { target: { value: "45" } });
		fireEvent.blur(number);

		await waitFor(() =>
			expect(put.body).toEqual({
				discovery_claim_alert_days: String(CEILING),
			}),
		);
		// The field must show what was actually saved, not what was typed.
		expect(claimAgeInputs().number).toHaveValue(CEILING);
	});

	it("shows a stored out-of-range threshold clamped to the ceiling", async () => {
		// A value the API validator would reject can still reach the client:
		// a restored backup or a config-sync import writes settings directly.
		// It must render as the number the backend will actually act on.
		mockSettings({
			alert_enabled: "true",
			discovery_claim_window_days: CLAIM_WINDOW_DAYS,
			discovery_claim_alert_days: "45",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-claim-age");
		const { number, slider } = claimAgeInputs();
		expect(number).toHaveValue(CEILING);
		expect(slider).toHaveValue(String(CEILING));
	});

	it("falls back to the default threshold, not the minimum, for a stored empty string", async () => {
		// A restored backup or a hand-edited settings row can carry an empty
		// string. `??` only substitutes for null/undefined, so this used to
		// reach `Number("")` (0) and render the slider's minimum of 1 while the
		// backend fell back to its own default of 7 — a visible/effective
		// disagreement, exactly what this control exists to prevent.
		mockSettings({
			alert_enabled: "true",
			discovery_claim_window_days: CLAIM_WINDOW_DAYS,
			discovery_claim_alert_days: "",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await screen.findByTestId("alert-claim-age");
		const { number, slider } = claimAgeInputs();
		expect(number).toHaveValue(7);
		expect(slider).toHaveValue("7");
	});

	it("surfaces a failed status check instead of hiding it", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
		});
		server.use(
			http.get("/api/alert/status", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		await waitFor(() =>
			expect(screen.getByText("Status check failed")).toBeInTheDocument(),
		);
	});
});
