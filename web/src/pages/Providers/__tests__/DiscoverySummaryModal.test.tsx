import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { DiscoveryDiff } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { DiscoverySummaryModal } from "../DiscoverySummaryModal";

const fullDiff: DiscoveryDiff = {
	added: [{ model_id: "model-new", reason: "new_model" }],
	reenabled: [{ model_id: "model-back", reason: "reappeared" }],
	disabled: [{ model_id: "model-gone", reason: "not_listed" }],
	failover_deleted_groups: [
		{
			display_model: "deleted-group",
			reason: "only 1 enabled provider (need 2+ for failover)",
			provider_count: 1,
			provider_names: ["Provider A"],
		},
	],
	failover_updated_groups: [
		{
			display_model: "updated-group",
			removed_model_ids: ["uuid-removed"],
			added_model_ids: ["uuid-added-1", "uuid-added-2"],
		},
	],
};

describe("DiscoverySummaryModal", () => {
	it("renders all sections from a full diff", () => {
		renderWithProviders(
			<DiscoverySummaryModal
				results={[{ providerName: "Test Provider", diff: fullDiff }]}
				onClose={vi.fn()}
			/>,
		);

		expect(screen.getByTestId("discovery-summary")).toBeInTheDocument();

		const added = screen.getByTestId("discovery-summary-added");
		expect(added).toHaveTextContent("model-new");

		const reenabled = screen.getByTestId("discovery-summary-reenabled");
		expect(reenabled).toHaveTextContent("model-back");

		const disabled = screen.getByTestId("discovery-summary-disabled");
		expect(disabled).toHaveTextContent("model-gone");

		const deleted = screen.getByTestId("discovery-summary-failover-deleted");
		expect(deleted).toHaveTextContent("deleted-group");

		const updated = screen.getByTestId("discovery-summary-failover-updated");
		expect(updated).toHaveTextContent("updated-group");
		expect(updated).toHaveTextContent("1");
		expect(updated).toHaveTextContent("2");

		expect(
			screen.queryByTestId("discovery-summary-no-changes"),
		).not.toBeInTheDocument();
	});

	it("renders the no-changes state for an empty diff", () => {
		renderWithProviders(
			<DiscoverySummaryModal
				results={[{ providerName: "Quiet Provider", diff: {} }]}
				onClose={vi.fn()}
			/>,
		);

		expect(
			screen.getByTestId("discovery-summary-no-changes"),
		).toBeInTheDocument();
		expect(
			screen.queryByTestId("discovery-summary-added"),
		).not.toBeInTheDocument();
	});

	it("renders the no-changes state when the diff is missing", () => {
		renderWithProviders(
			<DiscoverySummaryModal
				results={[{ providerName: "No Diff Provider" }]}
				onClose={vi.fn()}
			/>,
		);

		expect(
			screen.getByTestId("discovery-summary-no-changes"),
		).toBeInTheDocument();
	});

	it("renders provider error rows", () => {
		renderWithProviders(
			<DiscoverySummaryModal
				results={[
					{ providerName: "Broken Provider", error: "connection refused" },
					{ providerName: "Working Provider", diff: fullDiff },
				]}
				onClose={vi.fn()}
			/>,
		);

		const error = screen.getByTestId("discovery-summary-error");
		expect(error).toHaveTextContent("connection refused");

		// Provider names render as section headers in multi-provider mode.
		expect(screen.getByText("Broken Provider")).toBeInTheDocument();
		expect(screen.getByText("Working Provider")).toBeInTheDocument();
		expect(screen.getByTestId("discovery-summary-added")).toBeInTheDocument();
	});

	it("hides the provider header for a single-provider summary", () => {
		renderWithProviders(
			<DiscoverySummaryModal
				results={[{ providerName: "Solo Provider", diff: {} }]}
				onClose={vi.fn()}
			/>,
		);

		expect(screen.queryByText("Solo Provider")).not.toBeInTheDocument();
	});

	it("calls onClose when the modal close button is clicked", async () => {
		const onClose = vi.fn();
		const { user } = renderWithProviders(
			<DiscoverySummaryModal
				results={[{ providerName: "Test Provider", diff: {} }]}
				onClose={onClose}
			/>,
		);

		await user.click(screen.getByRole("button", { name: "Close" }));

		// The Modal fades out before invoking onClose.
		await waitFor(() => {
			expect(onClose).toHaveBeenCalled();
		});
	});
});
