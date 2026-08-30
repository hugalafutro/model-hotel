import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	CircuitBreakerProviderStatus,
	FailoverGroup,
} from "../../../api/types";
import { mockFailoverGroup } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { FailoverGroupCard } from "../FailoverGroupCard";

const PROVIDER_ID = "provider-uuid-1";

function member(modelId: string): FailoverGroup["entries"][0] {
	return {
		model_uuid: `uuid-${modelId}`,
		model_id: modelId,
		provider_id: PROVIDER_ID,
		provider_name: "TestProvider",
		display_name: modelId,
		enabled: true,
		model_enabled: true,
		provider_enabled: true,
		disabled_manually: false,
		context_length: 8192,
		owned_by: "test",
	};
}

// Both members sit behind the same provider, which is the only arrangement in
// which a provider-keyed and a model-keyed breaker disagree.
const group: FailoverGroup = {
	...mockFailoverGroup,
	group_enabled: true,
	entries: [member("dark-model"), member("healthy-model")],
};

const baseProps = {
	group,
	selected: false,
	onToggleSelect: vi.fn(),
	onToggleGroup: vi.fn(),
	onToggleEntry: vi.fn(),
	onReorder: vi.fn(),
	onDelete: vi.fn(),
	onResetCircuit: vi.fn(),
	cbProviderMap: new Map<string, CircuitBreakerProviderStatus>(),
};

function row(modelId: string): HTMLElement {
	return screen.getByText(modelId).closest(".failover-entry") as HTMLElement;
}

function hasOutline(modelId: string): boolean {
	const el = row(modelId);
	return (
		el.querySelector("[data-testid^='fuse-outline-']") !== null ||
		el.getAttribute("style")?.includes("overflow: hidden") === true
	);
}

function render(status: CircuitBreakerProviderStatus) {
	renderWithProviders(
		<FailoverGroupCard
			{...baseProps}
			cbProviderMap={new Map([[PROVIDER_ID, status]])}
		/>,
	);
}

describe("FailoverGroupCard model-keyed circuit breaker", () => {
	it("marks only the member whose own model the breaker is blocking", () => {
		// The row reports the provider's most degraded circuit, so it says "open"
		// while the provider itself is still serving every other model. Painting
		// both members from it is the provider-wide darkening the per-model keying
		// exists to end.
		render({
			provider_id: PROVIDER_ID,
			provider_name: "TestProvider",
			state: "open",
			consecutive_fails: 5,
			provider_open: false,
			open_models: ["dark-model"],
		});

		expect(hasOutline("dark-model")).toBe(true);
		expect(hasOutline("healthy-model")).toBe(false);
	});

	it("offers the reset control only on the member that is actually blocked", () => {
		render({
			provider_id: PROVIDER_ID,
			provider_name: "TestProvider",
			state: "open",
			consecutive_fails: 5,
			provider_open: false,
			open_models: ["dark-model"],
		});

		expect(screen.getAllByTestId("failover-entry-reset-circuit")).toHaveLength(
			1,
		);
	});

	it("marks every member once the provider verdict itself is open", () => {
		// Two models corroborate at the default span, so the breaker skips the
		// provider outright and the member with no circuit of its own is skipped
		// with it.
		render({
			provider_id: PROVIDER_ID,
			provider_name: "TestProvider",
			state: "open",
			consecutive_fails: 5,
			provider_open: true,
			open_models: ["dark-model", "other-model"],
		});

		expect(hasOutline("dark-model")).toBe(true);
		expect(hasOutline("healthy-model")).toBe(true);
	});
});
