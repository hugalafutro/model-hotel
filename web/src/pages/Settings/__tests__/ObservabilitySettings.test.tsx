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

// The status is a read-only badge (not a toggle); data-enabled carries the state.
function cardStatus(id: string): HTMLElement {
	return screen.getByTestId(`observability-status-${id}`);
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

	it("shows an enabled status and hides instructions when an exporter is on", async () => {
		seedSettings({
			log_export_json: "true",
			log_export_metrics: "true",
			log_export_otel: "true",
		});
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(cardStatus("json")).toHaveAttribute("data-enabled", "true");
		});
		for (const id of ["json", "metrics", "otel"]) {
			expect(cardStatus(id)).toHaveAttribute("data-enabled", "true");
			expect(
				screen.queryByTestId(`observability-instructions-${id}`),
			).not.toBeInTheDocument();
		}
	});

	it("shows enable instructions with a copyable env var only for disabled exporters", async () => {
		seedSettings({
			log_export_json: "true",
			log_export_metrics: "false",
			log_export_otel: "false",
		});
		renderWithProviders(
			<ObservabilitySettings collapsed={false} onToggle={onToggle} />,
		);

		await waitFor(() => {
			expect(cardStatus("json")).toHaveAttribute("data-enabled", "true");
		});

		// Enabled: no instructions.
		expect(
			screen.queryByTestId("observability-instructions-json"),
		).not.toBeInTheDocument();

		// Disabled: instructions present with the copyable env var to set.
		const metricsHint = screen.getByTestId(
			"observability-instructions-metrics",
		);
		expect(within(metricsHint).getByText(/METRICS_TOKEN=/)).toBeInTheDocument();

		const otelHint = screen.getByTestId("observability-instructions-otel");
		expect(
			within(otelHint).getByText(/OTEL_EXPORTER_OTLP_ENDPOINT=/),
		).toBeInTheDocument();

		expect(cardStatus("metrics")).toHaveAttribute("data-enabled", "false");
		expect(cardStatus("otel")).toHaveAttribute("data-enabled", "false");
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
			expect(cardStatus(id)).toHaveAttribute("data-enabled", "false");
			expect(
				screen.getByTestId(`observability-instructions-${id}`),
			).toBeInTheDocument();
		}
	});
});
