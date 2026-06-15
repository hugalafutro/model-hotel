import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { ObservabilitySettings } from "../ObservabilitySettings";

function seedSettings(status: Record<string, string>) {
	server.use(
		http.get("/api/settings", () =>
			HttpResponse.json({ app_version: "test", ...status }),
		),
	);
}

function cardSwitch(id: string): HTMLElement {
	const card = screen.getByTestId(`observability-card-${id}`);
	const toggle = within(card).getByRole("switch");
	return toggle as HTMLElement;
}

describe("ObservabilitySettings", () => {
	const onToggle = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders all three exporter cards", async () => {
		seedSettings({
			log_export_json: "false",
			log_export_metrics: "false",
			log_export_otel: "false",
		});
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(screen.getByTestId("observability-card-json")).toBeInTheDocument();
		});
		expect(
			screen.getByTestId("observability-card-metrics"),
		).toBeInTheDocument();
		expect(screen.getByTestId("observability-card-otel")).toBeInTheDocument();
	});

	it("reflects enabled state and hides instructions when an exporter is on", async () => {
		seedSettings({
			log_export_json: "true",
			log_export_metrics: "true",
			log_export_otel: "true",
		});
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(cardSwitch("json")).toHaveAttribute("aria-checked", "true");
		});
		for (const id of ["json", "metrics", "otel"]) {
			expect(cardSwitch(id)).toHaveAttribute("aria-checked", "true");
			// The toggle is a read-only reflector, never interactive.
			expect(cardSwitch(id)).toBeDisabled();
			expect(
				screen.queryByTestId(`observability-instructions-${id}`),
			).not.toBeInTheDocument();
		}
	});

	it("shows enable instructions only for disabled exporters", async () => {
		seedSettings({
			log_export_json: "true",
			log_export_metrics: "false",
			log_export_otel: "false",
		});
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(cardSwitch("json")).toHaveAttribute("aria-checked", "true");
		});

		// Enabled: no instructions.
		expect(
			screen.queryByTestId("observability-instructions-json"),
		).not.toBeInTheDocument();

		// Disabled: instructions present with the env var to set.
		const metricsHint = screen.getByTestId(
			"observability-instructions-metrics",
		);
		expect(within(metricsHint).getByText(/METRICS_TOKEN=/)).toBeInTheDocument();

		const otelHint = screen.getByTestId("observability-instructions-otel");
		expect(
			within(otelHint).getByText(/OTEL_EXPORTER_OTLP_ENDPOINT=/),
		).toBeInTheDocument();

		expect(cardSwitch("metrics")).toHaveAttribute("aria-checked", "false");
		expect(cardSwitch("otel")).toHaveAttribute("aria-checked", "false");
	});

	it("treats missing status keys (e.g. while loading) as disabled", async () => {
		// Settings response without any log_export_* keys — mirrors the
		// undefined/loading state where settings?.log_export_x === "true" is false.
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({ app_version: "test" }),
			),
		);
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(screen.getByTestId("observability-card-json")).toBeInTheDocument();
		});
		for (const id of ["json", "metrics", "otel"]) {
			expect(cardSwitch(id)).toHaveAttribute("aria-checked", "false");
			expect(
				screen.getByTestId(`observability-instructions-${id}`),
			).toBeInTheDocument();
		}
	});
});
