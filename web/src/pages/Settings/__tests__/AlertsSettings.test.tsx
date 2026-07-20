import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
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
	});

	it("shows inputs and a configured target when enabled", async () => {
		mockSettings({
			alert_enabled: "true",
			alert_apprise_api_url: "http://apprise:8000",
			alert_apprise_targets: "********",
		});
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		const target = await screen.findByTestId("alert-target-input");
		expect(target).toHaveAttribute(
			"placeholder",
			"Configured (type to replace)",
		);
		expect(screen.getByTestId("alert-api-url-input")).toHaveValue(
			"http://apprise:8000",
		);
		// Configured + enabled => test button is enabled.
		expect(screen.getByTestId("alert-test-button")).toBeEnabled();
		// A clear button appears for the configured secret.
		expect(screen.getByTestId("alert-target-clear")).toBeInTheDocument();
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
					{ error: "apprise-api unreachable" },
					{ status: 502 },
				),
			),
		);
		const user = userEvent.setup();
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);

		await user.click(await screen.findByTestId("alert-test-button"));
		await waitFor(() =>
			expect(screen.getByText(/Test notification failed/i)).toBeInTheDocument(),
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
					detail: "unreachable",
				}),
			),
		);
		renderWithProviders(
			<AlertsSettings collapsed={false} onToggle={() => {}} />,
		);
		await waitFor(() =>
			expect(screen.getByText(/apprise-api unreachable/i)).toBeInTheDocument(),
		);
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
