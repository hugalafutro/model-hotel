import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { FailoverGroup } from "../../api/types";
import {
	mockFailoverGroup,
	mockProvider,
	mockSystemStats,
} from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

const entry: FailoverGroup["entries"][0] = {
	model_uuid: "model-001",
	model_id: "test-model-v1",
	provider_id: mockProvider.id,
	provider_name: mockProvider.name,
	display_name: "Test Model",
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "test-provider",
};

// Custom (non-auto) group with one entry so the card exposes edit/delete, the
// group on/off control, and a per-entry toggle that managed mode must lock.
const group: FailoverGroup = {
	...mockFailoverGroup,
	auto_created: false,
	group_enabled: true,
	entries: [entry],
};

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

function useSystem(state: "primary" | "member") {
	server.use(
		http.get("/api/system", () => HttpResponse.json(systemWithFleet(state))),
		http.get("/api/failover-groups", () =>
			HttpResponse.json({ groups: [group], last_synced_at: null }),
		),
	);
}

async function findCard(): Promise<HTMLElement> {
	const name = await screen.findByText(`hotel/${group.display_model}`);
	return name.closest(".ui-card") as HTMLElement;
}

describe("FailoverGroups managed (fleet member) mode", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("locks every synced write and shows the banner for a member", async () => {
		useSystem("member");
		renderWithProviders(<FailoverGroups />);

		expect(await screen.findByTestId("managed-banner")).toBeInTheDocument();
		const card = await findCard();
		await waitFor(() => {
			// Card-level config writes are hidden.
			expect(
				within(card).queryByRole("button", { name: "Edit" }),
			).not.toBeInTheDocument();
			expect(
				within(card).queryByRole("button", { name: "Delete" }),
			).not.toBeInTheDocument();
			// The group on/off flag becomes a static badge, not a toggle button.
			expect(
				within(card).queryByRole("button", { name: "ON" }),
			).not.toBeInTheDocument();
			// The per-entry toggle (entry.enabled) is locked.
			expect(within(card).getByRole("switch")).toBeDisabled();
		});
		// Page-level create and bulk-selection affordances are gone.
		expect(
			screen.queryByRole("button", { name: "New Group" }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Select all" }),
		).not.toBeInTheDocument();
	});

	it("keeps every control and no banner when this instance is the primary", async () => {
		useSystem("primary");
		renderWithProviders(<FailoverGroups />);

		const card = await findCard();
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
		expect(
			within(card).getByRole("button", { name: "Edit" }),
		).toBeInTheDocument();
		expect(
			within(card).getByRole("button", { name: "ON" }),
		).toBeInTheDocument();
		expect(within(card).getByRole("switch")).toBeEnabled();
		expect(
			await screen.findByRole("button", { name: "New Group" }),
		).toBeInTheDocument();
	});
});
