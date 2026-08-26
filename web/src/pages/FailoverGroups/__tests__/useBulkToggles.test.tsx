import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { useBulkToggles } from "../useBulkToggles";

function group(id: string, provider: string, enabled = true): FailoverGroup {
	return {
		id,
		display_model: id,
		group_enabled: true,
		auto_created: true,
		entries: [
			{
				model_uuid: `${id}-1`,
				provider_name: provider,
				enabled,
				model_enabled: true,
				provider_enabled: true,
			},
			{
				model_uuid: `${id}-2`,
				provider_name: "other",
				enabled: true,
				model_enabled: true,
				provider_enabled: true,
			},
		],
	} as unknown as FailoverGroup;
}

function Harness({
	allGroups,
	providerFilter,
	selected,
	refreshGroups,
}: {
	allGroups: FailoverGroup[] | undefined;
	providerFilter: string;
	selected: string[];
	refreshGroups: () => void;
}) {
	const {
		handleBulkModelToggle,
		handleBulkProviderToggle,
		handleProviderToggle,
		isProviderToggling,
	} = useBulkToggles({
		allGroups,
		providerFilter,
		selectedGroupIds: new Set(selected),
		clearSelection: () => {},
		refreshGroups,
	});
	return (
		<div>
			<button type="button" onClick={() => handleBulkModelToggle(false)}>
				bulk-model
			</button>
			<button type="button" onClick={() => handleBulkProviderToggle(false)}>
				bulk-provider
			</button>
			<button
				type="button"
				onClick={() => handleProviderToggle("ghost", false)}
			>
				provider-modal
			</button>
			<span data-testid="toggling">{String(isProviderToggling)}</span>
		</div>
	);
}

describe("useBulkToggles", () => {
	it("sends nothing when no group matches the selection, the provider filter or the modal's provider", async () => {
		const update = vi.fn();
		server.use(
			http.put("/api/failover-groups/:id", () => {
				update();
				return HttpResponse.json({});
			}),
		);
		const refreshGroups = vi.fn();
		const { user } = renderWithProviders(
			<Harness
				allGroups={[group("g1", "openai")]}
				providerFilter="nobody"
				selected={["missing"]}
				refreshGroups={refreshGroups}
			/>,
		);
		await user.click(screen.getByText("bulk-model"));
		await user.click(screen.getByText("bulk-provider"));
		await user.click(screen.getByText("provider-modal"));
		// The modal path explains itself with a toast; the other two are silent no-ops.
		await waitFor(() => expect(screen.getAllByTestId("toast")).toHaveLength(1));
		expect(update).not.toHaveBeenCalled();
		expect(refreshGroups).not.toHaveBeenCalled();
	});

	it("re-reads the groups and reports the failure when a bulk write is rejected", async () => {
		server.use(
			http.put("/api/failover-groups/:id", () =>
				HttpResponse.json({ error: "nope" }, { status: 500 }),
			),
		);
		const refreshGroups = vi.fn();
		const { user } = renderWithProviders(
			<Harness
				allGroups={[group("g1", "openai"), group("g2", "openai")]}
				providerFilter="openai"
				selected={["g1"]}
				refreshGroups={refreshGroups}
			/>,
		);
		await user.click(screen.getByText("bulk-model"));
		await waitFor(() => expect(refreshGroups).toHaveBeenCalledTimes(1));
		await user.click(screen.getByText("bulk-provider"));
		await waitFor(() => expect(refreshGroups).toHaveBeenCalledTimes(2));
		expect(screen.getAllByTestId("toast").length).toBeGreaterThanOrEqual(2);
	});
});
