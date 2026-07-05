import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { expect, it, vi } from "vitest";
import { server } from "../../test/server";
import { ObservabilityPanel } from "../ObservabilityPanel";

function mockStatus(json: boolean, otel: boolean, metrics = false) {
	server.use(
		http.get("/api/observability", () =>
			HttpResponse.json({
				log_export_json: json,
				log_export_otel: otel,
				log_export_metrics: metrics,
			}),
		),
	);
}

it("shows a loading state and no badges until status resolves", async () => {
	let resolve: (() => void) | undefined;
	const gate = new Promise<void>((r) => {
		resolve = r;
	});
	server.use(
		http.get("/api/observability", async () => {
			await gate;
			return HttpResponse.json({
				log_export_json: true,
				log_export_otel: false,
			});
		}),
	);
	render(<ObservabilityPanel />);

	// Before the response lands, no status badge is rendered (so an enabled
	// exporter can't flash as "Disabled").
	expect(
		screen.queryByTestId("observability-status-json"),
	).not.toBeInTheDocument();

	resolve?.();
	await waitFor(() =>
		expect(screen.getByTestId("observability-status-json")).toHaveAttribute(
			"data-enabled",
			"true",
		),
	);
});

it("shows enabled badges and no enable instructions when all are on", async () => {
	mockStatus(true, true, true);
	render(<ObservabilityPanel />);

	await waitFor(() =>
		expect(screen.getByTestId("observability-status-json")).toHaveAttribute(
			"data-enabled",
			"true",
		),
	);
	expect(screen.getByTestId("observability-status-otel")).toHaveAttribute(
		"data-enabled",
		"true",
	);
	expect(screen.getByTestId("observability-status-metrics")).toHaveAttribute(
		"data-enabled",
		"true",
	);
	// Enabled exporters hide the env-var instructions.
	expect(
		screen.queryByTestId("observability-instructions-json"),
	).not.toBeInTheDocument();
	expect(
		screen.queryByTestId("observability-instructions-otel"),
	).not.toBeInTheDocument();
	expect(
		screen.queryByTestId("observability-instructions-metrics"),
	).not.toBeInTheDocument();
});

it("shows disabled badges with copyable env vars when both are off", async () => {
	mockStatus(false, false);
	render(<ObservabilityPanel />);

	await waitFor(() =>
		expect(screen.getByTestId("observability-status-json")).toHaveAttribute(
			"data-enabled",
			"false",
		),
	);
	expect(
		screen.getByTestId("observability-instructions-json"),
	).toHaveTextContent("LOG_FORMAT=json");
	expect(
		screen.getByTestId("observability-instructions-otel"),
	).toHaveTextContent("OTEL_EXPORTER_OTLP_ENDPOINT=<collector-url>");
	expect(
		screen.getByTestId("observability-instructions-metrics"),
	).toHaveTextContent("FRONTDESK_METRICS_TOKEN=<token>");
});

it("copies the env var to the clipboard when the pill is clicked", async () => {
	const writeText = vi.fn().mockResolvedValue(undefined);
	Object.assign(navigator, { clipboard: { writeText } });

	mockStatus(false, false);
	render(<ObservabilityPanel />);

	const pill = await screen.findByText("LOG_FORMAT=json");
	await userEvent.click(pill);
	expect(writeText).toHaveBeenCalledWith("LOG_FORMAT=json");
});

it("renders a graceful error when the status request fails", async () => {
	server.use(
		http.get(
			"/api/observability",
			() => new HttpResponse(null, { status: 500 }),
		),
	);
	render(<ObservabilityPanel />);

	await waitFor(() =>
		expect(
			screen.getByText(/could not load observability status/i),
		).toBeInTheDocument(),
	);
});
