import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { DiscoveryChangeEntry, GroupClaim } from "../../api/types";
import type { MergedProvider } from "../../hooks/useDiscrepancies";
import { ModelDiscrepancyModal } from "../ModelDiscrepancyModal";

const prov = (over: Partial<MergedProvider> = {}): MergedProvider => ({
	provider_id: "p1",
	provider_name: "NanoGPT",
	gone: [],
	stale: [],
	suspect: [],
	...over,
});

const claimOf = (
	model_id: string,
	status: "pending" | "resolved" | "new",
	state: "gone" | "stale" | "suspect" = "gone",
	flaps: { window?: number; sinceReview?: number } = {},
) => ({
	model_id,
	state,
	status,
	last_seen_at: "2026-07-01T00:00:00Z",
	missing_scans: state === "suspect" ? 1 : 0,
	flap_window: flaps.window ?? 0,
	flap_since_review: flaps.sinceReview ?? 0,
});

const group = (over: Partial<GroupClaim> = {}): GroupClaim => ({
	display_model: "gpt-oss-120b",
	member_count: 3,
	routable_count: 1,
	disabled_at: "2026-07-20T00:00:00Z",
	...over,
});

const baseProps = {
	groupClaims: [] as GroupClaim[],
	informational: [],
	onClose: vi.fn(),
	onRetest: vi.fn(),
	onRetestAll: vi.fn(),
	onDismiss: vi.fn(),
	isRetesting: false,
	errors: {},
	onExpandInformational: vi.fn(),
	readOnly: false,
};

const infoEntry: DiscoveryChangeEntry = {
	provider_id: "p1",
	provider_name: "NanoGPT",
	source: "background",
	detected_at: "2026-07-25T00:00:00Z",
	diff: { added: [{ model_id: "brand-new", reason: "new model" }] },
};

describe("ModelDiscrepancyModal", () => {
	it("disables every retest button while any retest is in flight", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({ gone: [claimOf("a", "pending")] }),
					prov({
						provider_id: "p2",
						provider_name: "OpenRouter",
						gone: [claimOf("b", "pending")],
					}),
				]}
				retestingProviderId="p1"
				isRetesting
			/>,
		);
		for (const btn of screen.getAllByTestId("discrepancy-retest")) {
			expect(btn).toBeDisabled();
		}
		expect(screen.getByTestId("discrepancy-retest-all")).toBeDisabled();
	});

	it("keeps a resolved provider in place, drops its retest, and shows the resolved state", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({ gone: [claimOf("a", "resolved")] }),
					prov({
						provider_id: "p2",
						provider_name: "OpenRouter",
						gone: [claimOf("b", "pending")],
					}),
				]}
			/>,
		);
		const sections = screen.getAllByTestId("discrepancy-provider");
		// Position is the point: the old modal dropped the row entirely, and a
		// resolved section that merely survives at the BOTTOM of the list is the
		// same defect in a milder form. getAllByTestId returns document order.
		expect(
			sections.map((s) => s.getAttribute("data-provider-id")),
		).toStrictEqual(["p1", "p2"]);
		expect(
			sections[0].querySelector("[data-testid='discrepancy-resolved']"),
		).not.toBeNull();
		expect(
			sections[0].querySelector("[data-testid='discrepancy-retest']"),
		).toBeNull();
		expect(
			sections[1].querySelector("[data-testid='discrepancy-retest']"),
		).not.toBeNull();
	});

	it("keeps a resolved claim struck through in its slot and takes only its dismiss away", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({ gone: [claimOf("a", "resolved"), claimOf("b", "pending")] }),
				]}
			/>,
		);
		const rows = screen.getAllByTestId("discrepancy-claim");
		// The row-level form of the headline bug: a claim that clears during a
		// retest must stay in its slot, greyed, not vanish mid-list.
		expect(rows.map((r) => r.getAttribute("data-model-id"))).toStrictEqual([
			"a",
			"b",
		]);
		expect(rows[0]).toHaveAttribute("data-status", "resolved");
		// A struck-through row has nothing left to dismiss.
		expect(
			rows[0].querySelector("[data-testid='discrepancy-dismiss']"),
		).toBeNull();
		expect(
			rows[1].querySelector("[data-testid='discrepancy-dismiss']"),
		).not.toBeNull();
	});

	it("offers no dismiss control for stale or suspect claims", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({
						stale: [claimOf("old", "pending", "stale")],
						suspect: [claimOf("wobbly", "pending", "suspect")],
					}),
				]}
			/>,
		);
		expect(screen.queryByTestId("discrepancy-dismiss")).toBeNull();
		// Stale still keeps the provider's Retest: a long-gone model is exactly
		// what you would re-probe on demand.
		expect(screen.getByTestId("discrepancy-retest")).toBeInTheDocument();
	});

	it("shows the empty state only when no group anywhere has content", () => {
		const { rerender } = render(
			<ModelDiscrepancyModal {...baseProps} providers={[]} />,
		);
		expect(screen.getByTestId("discrepancy-empty")).toBeInTheDocument();

		rerender(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ stale: [claimOf("old", "pending", "stale")] })]}
			/>,
		);
		expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
	});

	it("disables dismiss and retest in read-only mode instead of hiding them", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
				readOnly
			/>,
		);
		expect(screen.getByTestId("discrepancy-dismiss")).toBeDisabled();
		// A retest is a real discovery run, which the backend rejects with 403 in
		// read-only mode; an enabled button that always fails is the same lie.
		expect(screen.getByTestId("discrepancy-retest")).toBeDisabled();
		expect(screen.getByTestId("discrepancy-retest-all")).toBeDisabled();
	});

	it("renders a per-provider error banner without dropping that provider's claims", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
				errors={{ p1: "upstream timeout" }}
			/>,
		);
		expect(screen.getByTestId("discrepancy-error")).toBeInTheDocument();
		expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
			"data-model-id",
			"a",
		);
	});

	it("hands the provider id and model id to the dismiss and retest callbacks", async () => {
		const user = userEvent.setup();
		const onDismiss = vi.fn();
		const onRetest = vi.fn();
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				onDismiss={onDismiss}
				onRetest={onRetest}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
			/>,
		);
		await user.click(screen.getByTestId("discrepancy-dismiss"));
		expect(onDismiss).toHaveBeenCalledWith("p1", "a");
		await user.click(screen.getByTestId("discrepancy-retest"));
		expect(onRetest).toHaveBeenCalledWith("p1", "NanoGPT");
	});

	it("shows the flap chip on a model that has moved, and not on one that has not", () => {
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({
						gone: [
							claimOf("since-review", "pending", "gone", { sinceReview: 2 }),
							claimOf("window-only", "pending", "gone", { window: 2 }),
							claimOf("steady", "pending", "gone", { window: 1 }),
						],
					}),
				]}
			/>,
		);
		const flagged = screen
			.getAllByTestId("discrepancy-claim")
			.filter((row) => row.querySelector("[data-testid='discrepancy-flap']"));
		expect(
			flagged.map((row) => row.getAttribute("data-model-id")),
		).toStrictEqual(["since-review", "window-only"]);
	});

	describe("failover group claims", () => {
		it("renders one row per disabled group", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[]}
					groupClaims={[group(), group({ display_model: "llama-4-70b" })]}
				/>,
			);
			expect(
				screen.getByTestId("discrepancy-group-claims"),
			).toBeInTheDocument();
			expect(
				screen
					.getAllByTestId("discrepancy-group-claim")
					.map((row) => row.getAttribute("data-display-model")),
			).toStrictEqual(["gpt-oss-120b", "llama-4-70b"]);
		});

		it("counts as content, so a group-only claim is not the empty state", () => {
			// These already count toward the badge; an empty modal here would be a
			// badge pointing at nothing, the exact defect this rework removes.
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[]}
					groupClaims={[group()]}
				/>,
			);
			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
		});

		it("carries no retest or dismiss controls", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					groupClaims={[group()]}
				/>,
			);
			const section = screen.getByTestId("discrepancy-group-claims");
			// Anchored by the provider section, which does have both: this proves
			// the controls are absent from the group rows, not from the whole modal.
			expect(screen.getByTestId("discrepancy-retest")).toBeInTheDocument();
			expect(screen.getByTestId("discrepancy-dismiss")).toBeInTheDocument();
			expect(
				section.querySelector("[data-testid='discrepancy-retest']"),
			).toBeNull();
			expect(
				section.querySelector("[data-testid='discrepancy-dismiss']"),
			).toBeNull();
		});
	});

	describe("informational zone", () => {
		it("starts expanded and reports the expansion once when there are no claims", () => {
			const onExpandInformational = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[]}
					informational={[infoEntry]}
					onExpandInformational={onExpandInformational}
				/>,
			);
			expect(
				screen.getByTestId("discrepancy-informational-toggle"),
			).toHaveAttribute("aria-expanded", "true");
			expect(onExpandInformational).toHaveBeenCalledTimes(1);
		});

		it("ignores the empty render before the fetch lands and seeds off the data", () => {
			const onExpandInformational = vi.fn();
			// useDiscrepancies keys its query per open, so the modal always renders
			// once with an empty snapshot. Seeding off that render would latch the
			// journal open while claims exist, and would do it without acking, so
			// the dot would never clear.
			const { rerender } = render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[]}
					informational={[]}
					onExpandInformational={onExpandInformational}
				/>,
			);
			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[infoEntry]}
					onExpandInformational={onExpandInformational}
				/>,
			);
			expect(
				screen.getByTestId("discrepancy-informational-toggle"),
			).toHaveAttribute("aria-expanded", "false");
			expect(onExpandInformational).not.toHaveBeenCalled();
		});

		it("starts collapsed when there are claims and reports only the first expand", async () => {
			const user = userEvent.setup();
			const onExpandInformational = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[infoEntry]}
					onExpandInformational={onExpandInformational}
				/>,
			);
			const toggle = screen.getByTestId("discrepancy-informational-toggle");
			expect(toggle).toHaveAttribute("aria-expanded", "false");
			expect(onExpandInformational).not.toHaveBeenCalled();

			await user.click(toggle);
			expect(toggle).toHaveAttribute("aria-expanded", "true");
			expect(onExpandInformational).toHaveBeenCalledTimes(1);

			// Collapsing and re-expanding must not re-ack the journal.
			await user.click(toggle);
			await user.click(toggle);
			expect(onExpandInformational).toHaveBeenCalledTimes(1);
		});
	});
});
