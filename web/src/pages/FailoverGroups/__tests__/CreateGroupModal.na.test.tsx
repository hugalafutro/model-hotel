import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import type { CandidateModel, FailoverGroup } from "../../../api/types";
import i18n from "../../../i18n";
import { server } from "../../../test/mocks/server";
import { armDeleteConfirm, renderWithProviders } from "../../../test/utils";
import { CreateGroupModal } from "../CreateGroupModal";

// CreateGroupModal's duplicate-candidate dedupe and the N/A actions' failure
// and cancel paths. UI text is looked up through its i18n key.

const candidate = (
	uuid: string,
	provider: string,
	modelId: string,
	display: string,
): CandidateModel => ({
	model_uuid: uuid,
	model_id: modelId,
	provider_id: `pid-${provider}`,
	provider_name: provider,
	display_name: display,
	context_length: 8192,
	owned_by: "x",
});

const entry = (
	uuid: string,
	provider: string,
	modelId: string,
	over: Partial<FailoverGroup["entries"][0]> = {},
): FailoverGroup["entries"][0] => ({
	model_uuid: uuid,
	model_id: modelId,
	provider_id: `pid-${provider}`,
	provider_name: provider,
	display_name: modelId,
	enabled: true,
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
	context_length: 8192,
	owned_by: "x",
	...over,
});

// One healthy member plus two retryable N/A members (auto-disabled model on a
// live provider): Delete N/A would leave one, so it asks to delete the group.
const group: FailoverGroup = {
	id: "fg-1",
	display_model: "grp",
	display_name: "",
	description: "",
	group_enabled: true,
	auto_created: false,
	auto_disabled: false,
	entries: [
		entry("u-ok", "Live", "ok-model"),
		entry("u-na1", "Flaky", "na-throws", { model_enabled: false }),
		entry("u-na2", "Flaky", "na-fails", { model_enabled: false }),
	],
	total_tokens: 0,
	created_at: "2026-04-01T10:00:00Z",
	updated_at: "2026-04-01T10:00:00Z",
};

function renderEdit() {
	return renderWithProviders(
		<CreateGroupModal
			candidates={[]}
			refreshGroups={vi.fn()}
			group={group}
			onClose={vi.fn()}
			onUpdated={vi.fn()}
		/>,
	);
}

describe("CreateGroupModal N/A and dedupe paths", () => {
	it("shows a candidate listed twice as a single pill", () => {
		renderWithProviders(
			<CreateGroupModal
				candidates={[
					candidate("u-1", "Prov", "m", "Model M"),
					candidate("u-2", "Prov", "m", "Model M"),
					candidate("u-3", "Other", "n", "Model N"),
				]}
				refreshGroups={vi.fn()}
				onClose={vi.fn()}
				onCreated={vi.fn()}
			/>,
		);
		expect(screen.getAllByText("Model M")).toHaveLength(1);
		expect(screen.getByText("Model N")).toBeInTheDocument();
	});

	it("keeps a member that fails or cannot be probed in the failed column, then closes the summary", async () => {
		let patched = false;
		server.use(
			http.post("/api/models/:id/test", ({ params }) =>
				params.id === "u-na1"
					? HttpResponse.json({ error: "down" }, { status: 502 })
					: HttpResponse.json({
							success: false,
							streaming: false,
							ttft_ms: 0,
							duration_ms: 0,
							response: "",
						}),
			),
			http.patch("/api/models/:id", () => {
				patched = true;
				return HttpResponse.json({});
			}),
		);
		const { user } = renderEdit();
		await user.click(
			screen.getByRole("button", {
				name: i18n.t("failoverGroups.entry.retryNa"),
			}),
		);
		const summary = await screen.findByTestId("discovery-summary");
		const failed = within(summary).getByTestId("discovery-summary-disabled");
		expect(failed).toHaveTextContent("na-throws");
		expect(failed).toHaveTextContent("na-fails");
		// Neither answered, so neither was re-enabled.
		expect(patched).toBe(false);

		await user.click(
			within(summary.closest("[role='dialog']") as HTMLElement).getByRole(
				"button",
				{ name: i18n.t("common.close") },
			),
		);
		await waitFor(() =>
			expect(screen.queryByTestId("discovery-summary")).toBeNull(),
		);
	});

	it("cancelling the delete-group confirmation deletes nothing", async () => {
		const del = vi.spyOn(api.failoverGroups, "delete");
		const { user } = renderEdit();
		await user.click(
			screen.getByRole("button", {
				name: i18n.t("failoverGroups.entry.deleteNa"),
			}),
		);
		const title = await screen.findByText(
			i18n.t("failoverGroups.entry.deleteNaConfirmTitle"),
		);
		await user.click(
			within(title.closest("[role='dialog']") as HTMLElement).getByRole(
				"button",
				{ name: i18n.t("common.cancel") },
			),
		);
		await waitFor(() =>
			expect(
				screen.queryByText(i18n.t("failoverGroups.entry.deleteNaConfirmTitle")),
			).toBeNull(),
		);
		expect(del).not.toHaveBeenCalled();
		del.mockRestore();
	});

	it("toasts the failure when deleting the group is rejected", async () => {
		const del = vi
			.spyOn(api.failoverGroups, "delete")
			.mockRejectedValue(new Error("in use"));
		const { user } = renderEdit();
		await user.click(
			screen.getByRole("button", {
				name: i18n.t("failoverGroups.entry.deleteNa"),
			}),
		);
		await user.click(await armDeleteConfirm(user, "hotel/grp"));
		expect(
			await screen.findByText(
				i18n.t("failover.toast_delete_failed", { message: "in use" }),
			),
		).toBeInTheDocument();
		del.mockRestore();
	});
});
