import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { AlertsSettings } from "../AlertsSettings";

function mockSettings(values: Record<string, string>) {
	server.use(
		http.get("/api/settings", ({ request }) => {
			if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
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
			"Configured — type to replace",
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
});
