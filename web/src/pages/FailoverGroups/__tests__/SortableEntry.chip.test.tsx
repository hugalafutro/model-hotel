import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { SortableEntry } from "../SortableEntry";

const entry: FailoverGroup["entries"][0] = {
	model_uuid: "m-uuid",
	model_id: "glm-5.3",
	provider_id: "p1",
	provider_name: "Neuralwatt",
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
} as FailoverGroup["entries"][0];

// Locale-independent: the chip is found by testid and its state by data-chip.
describe("SortableEntry circuit chip", () => {
	it("shows the entry's chip with the last verdict in its tooltip", () => {
		renderWithProviders(
			<SortableEntry
				entry={entry}
				groupEnabled
				onToggle={vi.fn()}
				circuitView={{
					chip: "busy",
					lastCause: "upstream status 429 (saturated)",
					lastStatus: 429,
					lastAt: "2026-08-31T14:47:30Z",
				}}
			/>,
		);
		const chip = screen.getByTestId("failover-entry-chip");
		expect(chip).toHaveAttribute("data-chip", "busy");
		expect(chip.getAttribute("title")).toContain(
			"upstream status 429 (saturated)",
		);
		expect(chip.getAttribute("title")).toContain("429");
	});

	it("renders no chip without a view, or on a disabled entry", () => {
		renderWithProviders(
			<SortableEntry entry={entry} groupEnabled onToggle={vi.fn()} />,
		);
		expect(screen.queryByTestId("failover-entry-chip")).not.toBeInTheDocument();
		renderWithProviders(
			<SortableEntry
				entry={{ ...entry, enabled: false }}
				groupEnabled
				onToggle={vi.fn()}
				circuitView={{ chip: "live" }}
			/>,
		);
		expect(screen.queryByTestId("failover-entry-chip")).not.toBeInTheDocument();
	});
});
