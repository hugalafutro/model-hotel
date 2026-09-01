import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	CircuitBreakerProviderStatus,
	FailoverGroup,
} from "../../../api/types";
import { mockFailoverGroup } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { FailoverGroupCard } from "../FailoverGroupCard";

function member(
	providerId: string,
	modelId: string,
): FailoverGroup["entries"][0] {
	return {
		...mockFailoverGroup.entries[0],
		model_uuid: `${providerId}-${modelId}`,
		model_id: modelId,
		provider_id: providerId,
		provider_name: providerId,
		enabled: true,
		model_enabled: true,
		provider_enabled: true,
	};
}

const group: FailoverGroup = {
	...mockFailoverGroup,
	group_enabled: true,
	entries: [
		member("zai", "glm-5.3"),
		member("neuralwatt", "glm-5.3"),
		member("ollama", "glm-5.3"),
	],
};

const baseProps = {
	group,
	selected: false,
	onToggleSelect: vi.fn(),
	onToggleGroup: vi.fn(),
	onToggleEntry: vi.fn(),
	onReorder: vi.fn(),
	onDelete: vi.fn(),
};

const openRow = (id: string, model: string): CircuitBreakerProviderStatus => ({
	provider_id: id,
	state: "open",
	consecutive_fails: 5,
	provider_open: false,
	open_models: [model],
	next_retry_at: "2026-08-31T15:00:00Z",
	circuits: [
		{
			model,
			state: "open",
			consecutive_fails: 5,
			next_retry_at: "2026-08-31T15:00:00Z",
		},
	],
});

// Locale-independent: the header is found by testid; counts are digits.
describe("FailoverGroupCard circuit summary", () => {
	it("counts the live entries against the routable ones", () => {
		const cb = new Map<string, CircuitBreakerProviderStatus>([
			["zai", openRow("zai", "glm-5.3")],
		]);
		renderWithProviders(
			<FailoverGroupCard {...baseProps} cbProviderMap={cb} />,
		);
		const live = screen.getByTestId("failover-card-live-count");
		expect(live.textContent).toContain("2");
		expect(live.textContent).toContain("3");
		expect(
			screen.queryByTestId("failover-card-all-dark"),
		).not.toBeInTheDocument();
		expect(screen.getAllByTestId("failover-entry-chip")).toHaveLength(3);
	});

	it("says all entries are dark, in red, when every one is turned away", () => {
		const cb = new Map<string, CircuitBreakerProviderStatus>([
			["zai", openRow("zai", "glm-5.3")],
			["neuralwatt", openRow("neuralwatt", "glm-5.3")],
			["ollama", openRow("ollama", "glm-5.3")],
		]);
		renderWithProviders(
			<FailoverGroupCard {...baseProps} cbProviderMap={cb} />,
		);
		expect(screen.getByTestId("failover-card-all-dark")).toBeInTheDocument();
		expect(
			screen.queryByTestId("failover-card-live-count"),
		).not.toBeInTheDocument();
	});

	it("shows neither chips nor a summary on a disabled group", () => {
		const cb = new Map<string, CircuitBreakerProviderStatus>([
			["zai", openRow("zai", "glm-5.3")],
		]);
		renderWithProviders(
			<FailoverGroupCard
				{...baseProps}
				group={{ ...group, group_enabled: false }}
				cbProviderMap={cb}
			/>,
		);
		expect(screen.queryByTestId("failover-entry-chip")).not.toBeInTheDocument();
		expect(
			screen.queryByTestId("failover-card-live-count"),
		).not.toBeInTheDocument();
		expect(
			screen.queryByTestId("failover-card-all-dark"),
		).not.toBeInTheDocument();
	});
});
