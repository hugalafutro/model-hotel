import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
	status: "pending" | "resolved" | "new" | "dismissed",
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
	onCancelRetestAll: vi.fn(),
	onDismiss: vi.fn(),
	onDismissAll: vi.fn(),
	onDismissEverything: vi.fn(),
	isRetesting: false,
	errors: {},
	onExpandInformational: vi.fn(),
	readOnly: false,
};

/**
 * Opens a provider pill and then one of its bucket lines, which is what the
 * accordion now requires before any model row is mounted.
 *
 * Providers render collapsed and their bucket lines render collapsed inside them,
 * so a test that wants rows has to ask for them. `nth` picks the provider when a
 * case renders more than one.
 */
async function openBucket(
	user: ReturnType<typeof userEvent.setup>,
	bucket: "gone" | "stale" | "suspect" = "gone",
	nth = 0,
) {
	const section = screen.getAllByTestId("discrepancy-provider")[nth];
	await user.click(
		section.querySelector(
			"[data-testid='discrepancy-provider-pill']",
		) as HTMLElement,
	);
	// Scoped to the section: every provider has its own line per bucket, so a
	// document-wide query is ambiguous the moment a case renders two providers.
	await user.click(
		section.querySelector(
			`[data-testid='discrepancy-group-${bucket}-toggle']`,
		) as HTMLElement,
	);
}

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

	it("keeps per-provider retest disabled between two providers of a walk", () => {
		// `isRetesting` deliberately false: between providers the walk is refreshing
		// status, not discovering, so no mutation is pending. The walk's own lock
		// would still refuse the click, silently. An enabled button that does
		// nothing is the complaint that started this rework.
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
				isRetesting={false}
				retestAllProgress={{ done: 1, total: 2 }}
			/>,
		);
		expect(screen.getByTestId("discrepancy-retest")).toBeDisabled();
		// Cancel is the one control the block must not reach.
		expect(
			screen.getByTestId("discrepancy-retest-all-cancel"),
		).not.toBeDisabled();
	});

	it("keeps a resolved provider in place, drops its retest, and shows the resolved state", async () => {
		const user = userEvent.setup();
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
		let sections = screen.getAllByTestId("discrepancy-provider");
		// Position is the point: the old modal dropped the row entirely, and a
		// resolved section that merely survives at the BOTTOM of the list is the
		// same defect in a milder form. getAllByTestId returns document order.
		expect(
			sections.map((s) => s.getAttribute("data-provider-id")),
		).toStrictEqual(["p1", "p2"]);
		// The cleared summary lives in the provider BODY, which is unmounted while
		// the pill is collapsed, so it has to be unrolled to be asserted on.
		await user.click(
			sections[0].querySelector(
				"[data-testid='discrepancy-provider-pill']",
			) as HTMLElement,
		);
		sections = screen.getAllByTestId("discrepancy-provider");
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

	it("keeps a resolved claim struck through in its slot and takes only its dismiss away", async () => {
		const user = userEvent.setup();
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({ gone: [claimOf("a", "resolved"), claimOf("b", "pending")] }),
				]}
			/>,
		);
		await openBucket(user, "gone");
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

	it("disables dismiss and retest in read-only mode instead of hiding them", async () => {
		const user = userEvent.setup();
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
				readOnly
			/>,
		);
		await openBucket(user, "gone");
		expect(screen.getByTestId("discrepancy-dismiss")).toBeDisabled();
		// A retest is a real discovery run, which the backend rejects with 403 in
		// read-only mode; an enabled button that always fails is the same lie.
		expect(screen.getByTestId("discrepancy-retest")).toBeDisabled();
		expect(screen.getByTestId("discrepancy-retest-all")).toBeDisabled();
	});

	it("renders a per-provider error banner without dropping that provider's claims", async () => {
		const user = userEvent.setup();
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[prov({ gone: [claimOf("a", "pending")] })]}
				errors={{ p1: "upstream timeout" }}
			/>,
		);
		// The banner sits OUTSIDE the collapsible body on purpose: it must be
		// visible on a collapsed pill, which is where Retest was clicked.
		expect(screen.getByTestId("discrepancy-error")).toBeInTheDocument();
		await openBucket(user, "gone");
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
		await openBucket(user, "gone");
		await user.click(screen.getByTestId("discrepancy-dismiss"));
		expect(onDismiss).toHaveBeenCalledWith("p1", "a");
		await user.click(screen.getByTestId("discrepancy-retest"));
		expect(onRetest).toHaveBeenCalledWith("p1", "NanoGPT");
	});

	it("shows the flap chip on a model that has moved, and not on one that has not", async () => {
		const user = userEvent.setup();
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
		await openBucket(user, "gone");
		const flagged = screen
			.getAllByTestId("discrepancy-claim")
			.filter((row) => row.querySelector("[data-testid='discrepancy-flap']"));
		expect(
			flagged.map((row) => row.getAttribute("data-model-id")),
		).toStrictEqual(["since-review", "window-only"]);
	});

	it("keeps the flap chip's tooltip count in agreement with its visible count in the window-only case", async () => {
		const user = userEvent.setup();
		render(
			<ModelDiscrepancyModal
				{...baseProps}
				providers={[
					prov({
						gone: [claimOf("window-only", "pending", "gone", { window: 4 })],
					}),
				]}
			/>,
		);
		await openBucket(user, "gone");
		const chip = screen.getByTestId("discrepancy-flap");
		const visibleCount = chip.textContent?.match(/\d+/)?.[0];
		const tooltipCount = chip.getAttribute("title")?.match(/\d+/)?.[0];
		expect(visibleCount).toBe("4");
		expect(tooltipCount).toBe(visibleCount);
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

		it("carries no retest or dismiss controls", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					groupClaims={[group()]}
				/>,
			);
			// Unroll the provider so its own Dismiss exists; without it the assertion
			// below would pass because the control is absent everywhere.
			await openBucket(user, "gone");
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

	describe("accessibility", () => {
		// Resolve a toggle's aria-controls the way assistive tech does, and prove
		// it lands on the region that actually holds the content.
		const regionOf = (toggle: HTMLElement, contained: HTMLElement) => {
			const id = toggle.getAttribute("aria-controls");
			expect(id).toBeTruthy();
			const region = document.getElementById(id as string);
			expect(region).not.toBeNull();
			expect(region).toContainElement(contained);
			return region as HTMLElement;
		};

		it("takes the collapsed journal out of the accessibility tree", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[infoEntry]}
				/>,
			);
			const toggle = screen.getByTestId("discrepancy-informational-toggle");
			const region = regionOf(
				toggle,
				screen.getByTestId("discrepancy-informational-entry"),
			);

			// grid-rows-[0fr] + overflow-hidden hides the entries visually only, so
			// without `inert` a screen reader reads all of them out while this very
			// toggle claims the zone is collapsed.
			expect(toggle).toHaveAttribute("aria-expanded", "false");
			expect(region.firstElementChild).toHaveAttribute("inert");

			await user.click(toggle);
			expect(toggle).toHaveAttribute("aria-expanded", "true");
			expect(region.firstElementChild).not.toHaveAttribute("inert");
		});

		it("takes the collapsed stale rows out of the accessibility tree", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ stale: [claimOf("old", "pending", "stale")] })]}
				/>,
			);
			// Collapsed rows are UNMOUNTED, not merely inert. That is strictly better
			// for a screen reader (a row that does not exist cannot be announced under
			// an aria-expanded="false" toggle) and it is what keeps unrolling cheap:
			// the modal holds one bucket's rows, not every bucket's.
			await user.click(screen.getByTestId("discrepancy-provider-pill"));
			const toggle = screen.getByTestId("discrepancy-group-stale-toggle");

			expect(toggle).toHaveAttribute("aria-expanded", "false");
			expect(screen.queryByTestId("discrepancy-claim")).toBeNull();

			await user.click(toggle);

			expect(toggle).toHaveAttribute("aria-expanded", "true");
			expect(screen.getByTestId("discrepancy-claim")).toBeInTheDocument();
			// aria-controls must still resolve, so the region wrapper is always there.
			const regionId = toggle.getAttribute("aria-controls");
			expect(regionId).toBeTruthy();
			expect(document.getElementById(regionId as string)).not.toBeNull();
		});

		it("makes the read-only reason reachable from the controls it disables", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					readOnly
				/>,
			);
			const note = screen.getByTestId("discrepancy-readonly-note");
			expect(note.id).toBeTruthy();
			expect(note.textContent?.trim()).not.toBe("");
			// The per-row Dismiss only exists once its bucket is unrolled.
			await openBucket(user, "gone");

			for (const testId of [
				"discrepancy-dismiss",
				"discrepancy-retest",
				"discrepancy-retest-all",
			]) {
				const button = screen.getByTestId(testId);
				// Disabled buttons are not focusable, so their `title` never reaches
				// a keyboard or screen-reader user. The description has to.
				expect(button).toBeDisabled();
				expect(button).toHaveAttribute("aria-describedby", note.id);
				// The two must not drift apart: whatever the tooltip says on hover is
				// what assistive tech gets. Compared, never spelled out, so this holds
				// in every locale.
				expect(button.getAttribute("title")).toBe(note.textContent);
			}
		});

		it("leaves no dangling description when the modal is writable", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);
			expect(screen.queryByTestId("discrepancy-readonly-note")).toBeNull();
			await openBucket(user, "gone");
			expect(screen.getByTestId("discrepancy-dismiss")).not.toHaveAttribute(
				"aria-describedby",
			);
		});
	});

	describe("provider accordion", () => {
		// Collapsed does NOT mean unmounted here: the rows stay in the DOM inside an
		// `inert`, zero-height grid row so the open/close can animate. "Is it open?"
		// is therefore aria-expanded on the toggle, never presence of the rows.
		// `nth` matters once a case renders two providers: each has its own pill and
		// its own line per bucket, so a document-wide query is ambiguous.
		const isOpen = (testId: string, nth = 0) =>
			screen.getAllByTestId(testId)[nth].getAttribute("aria-expanded") ===
			"true";

		it("renders every provider collapsed, with nothing below the pill mounted", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "pending")],
							stale: [claimOf("old", "pending", "stale")],
						}),
					]}
				/>,
			);

			expect(isOpen("discrepancy-provider-pill")).toBe(false);
			// A collapsed provider mounts NEITHER its bucket lines nor their rows.
			// That is what keeps the modal cheap with eight providers on screen: it
			// holds eight pills, not eight providers' worth of lists.
			expect(screen.queryByTestId("discrepancy-group-gone-toggle")).toBeNull();
			expect(screen.queryByTestId("discrepancy-group-stale-toggle")).toBeNull();
			expect(screen.queryByTestId("discrepancy-claim")).toBeNull();
		});

		it("unrolls a provider without opening any of its bucket lines", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			// Two clicks to reach a model, by design: the pill answers "which
			// provider", the line answers "which kind of problem".
			expect(isOpen("discrepancy-provider-pill")).toBe(true);
			expect(isOpen("discrepancy-group-gone-toggle")).toBe(false);
		});

		it("collapses the open provider and its line when a second provider opens", async () => {
			const user = userEvent.setup();
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
				/>,
			);
			await openBucket(user, "gone", 0);
			expect(isOpen("discrepancy-group-gone-toggle")).toBe(true);

			await user.click(screen.getAllByTestId("discrepancy-provider-pill")[1]);

			const pills = screen.getAllByTestId("discrepancy-provider-pill");
			expect(pills[0]).toHaveAttribute("aria-expanded", "false");
			expect(pills[1]).toHaveAttribute("aria-expanded", "true");
			// The line the first provider had open went with it: one atom holds both
			// halves of the path, so switching provider cannot leave a stale line.
			// Exactly ONE bucket line is mounted, the newly opened provider's, and it
			// is closed.
			const toggles = screen.getAllByTestId("discrepancy-group-gone-toggle");
			expect(toggles).toHaveLength(1);
			expect(toggles[0]).toHaveAttribute("aria-expanded", "false");
			expect(screen.queryByTestId("discrepancy-claim")).toBeNull();
		});

		it("collapses the open bucket line when a sibling line opens", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "pending")],
							stale: [claimOf("old", "pending", "stale")],
						}),
					]}
				/>,
			);
			await openBucket(user, "gone");
			expect(isOpen("discrepancy-group-gone-toggle")).toBe(true);

			await user.click(screen.getByTestId("discrepancy-group-stale-toggle"));

			expect(isOpen("discrepancy-group-gone-toggle")).toBe(false);
			expect(isOpen("discrepancy-group-stale-toggle")).toBe(true);
		});

		it("closes an open provider when its own pill is clicked again", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);
			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			expect(isOpen("discrepancy-provider-pill")).toBe(false);
		});

		it("leaves the open provider alone when the journal is toggled", async () => {
			// The journal is a separate zone, not a third level of the accordion:
			// reading the background-discovery log must not cost the operator their
			// place in a provider.
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[infoEntry]}
				/>,
			);
			await openBucket(user, "gone");

			await user.click(screen.getByTestId("discrepancy-informational-toggle"));

			expect(isOpen("discrepancy-provider-pill")).toBe(true);
			expect(isOpen("discrepancy-group-gone-toggle")).toBe(true);
		});
	});

	describe("provider pill controls", () => {
		it("offers Retest all and Dismiss all while rows are actionable", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			expect(screen.getByTestId("discrepancy-retest")).toBeEnabled();
			expect(screen.getByTestId("discrepancy-dismiss-all")).toBeEnabled();
			expect(screen.queryByTestId("discrepancy-clean")).toBeNull();
		});

		it("disables Dismiss all when every actionable row is suspect", () => {
			// setModelsDismissed only touches enabled = false rows and a suspect model
			// is still enabled, so the server would refuse it. Disabled rather than
			// hidden, so it does not read as a missing feature.
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ suspect: [claimOf("s", "pending", "suspect")] })]}
				/>,
			);

			expect(screen.getByTestId("discrepancy-dismiss-all")).toBeDisabled();
			expect(screen.getByTestId("discrepancy-retest")).toBeEnabled();
		});

		it("replaces both controls with Clean once nothing is actionable", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "dismissed")] })]}
				/>,
			);

			expect(screen.getByTestId("discrepancy-clean")).toBeInTheDocument();
			expect(screen.queryByTestId("discrepancy-retest")).toBeNull();
			expect(screen.queryByTestId("discrepancy-dismiss-all")).toBeNull();
		});

		it("keeps a cleared provider unrollable, with its rows struck through", async () => {
			// The pill is the log of what the operator did until they Clean it away.
			// Dropping the buckets on clear would be the #583 vanishing-rows bug one
			// level up.
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "dismissed"), claimOf("b", "dismissed")],
						}),
					]}
				/>,
			);

			await openBucket(user, "gone");

			const rows = screen.getAllByTestId("discrepancy-claim");
			expect(rows).toHaveLength(2);
			for (const row of rows) {
				expect(row).toHaveAttribute("data-status", "dismissed");
			}
			expect(
				screen.getByTestId("discrepancy-dismissed-summary"),
			).toBeInTheDocument();
		});

		it("counts actionable rows in the chips, cleared rows once dealt with", () => {
			const { rerender } = render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({ gone: [claimOf("a", "pending"), claimOf("b", "pending")] }),
					]}
				/>,
			);
			expect(screen.getByTestId("discrepancy-chip-gone")).toHaveTextContent(
				"2",
			);

			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "dismissed"), claimOf("b", "dismissed")],
						}),
					]}
				/>,
			);

			// A chip reading "× 2 Gone" over two rows the operator already dismissed
			// would advertise two live problems that no longer exist.
			expect(screen.queryByTestId("discrepancy-chip-gone")).toBeNull();
			expect(
				screen.getByTestId("discrepancy-chip-dismissed"),
			).toHaveTextContent("2");
		});

		it("drops a cleaned provider from the list without calling anything", async () => {
			const user = userEvent.setup();
			const onDismissAll = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissAll={onDismissAll}
					providers={[prov({ gone: [claimOf("a", "dismissed")] })]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-clean"));

			expect(screen.queryByTestId("discrepancy-provider")).toBeNull();
			// View-only: every row is already persisted as dismissed or is healthy.
			expect(onDismissAll).not.toHaveBeenCalled();
		});

		it("brings a cleaned provider back when a refresh gives it a new claim", async () => {
			// Hiding on membership alone hid the provider for the life of the modal, so
			// a model that flapped healthy -> gone again after the Clean click vanished
			// silently. That is the false reassurance this whole rework exists to
			// remove, and the disappearance-only test above did not catch it.
			const user = userEvent.setup();
			const cleared = prov({ gone: [claimOf("done", "dismissed")] });
			const { rerender } = render(
				<ModelDiscrepancyModal {...baseProps} providers={[cleared]} />,
			);
			await user.click(screen.getByTestId("discrepancy-clean"));
			expect(screen.queryByTestId("discrepancy-provider")).toBeNull();

			// A refresh reports the same provider with a genuinely new gone model.
			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("done", "dismissed"), claimOf("fresh", "new")],
						}),
					]}
				/>,
			);

			expect(screen.getByTestId("discrepancy-provider")).toBeInTheDocument();
			// And it carries both: the new claim, plus the struck-through log above it.
			expect(screen.getByTestId("discrepancy-chip-gone")).toHaveTextContent(
				"1",
			);
			await openBucket(user, "gone");
			expect(
				screen
					.getAllByTestId("discrepancy-claim")
					.map((r) => [
						r.getAttribute("data-model-id"),
						r.getAttribute("data-status"),
					]),
			).toStrictEqual([
				["done", "dismissed"],
				["fresh", "new"],
			]);
		});

		it("hides it again once the new claim is dealt with, without a second Clean", async () => {
			// The auto-re-hide is deliberate: by this point the operator has seen the
			// new row and acted on it, so nothing is hidden that they have not dealt
			// with, and it saves re-clicking Clean on a provider they already retired.
			const user = userEvent.setup();
			const { rerender } = render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("done", "dismissed")] })]}
				/>,
			);
			await user.click(screen.getByTestId("discrepancy-clean"));

			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("done", "dismissed"), claimOf("fresh", "new")],
						}),
					]}
				/>,
			);
			expect(screen.getByTestId("discrepancy-provider")).toBeInTheDocument();

			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [
								claimOf("done", "dismissed"),
								claimOf("fresh", "dismissed"),
							],
						}),
					]}
				/>,
			);

			expect(screen.queryByTestId("discrepancy-provider")).toBeNull();
		});
	});

	describe("dismiss all", () => {
		it("asks before sending, and sends nothing when cancelled", async () => {
			const user = userEvent.setup();
			const onDismissAll = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissAll={onDismissAll}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-all"));
			expect(onDismissAll).not.toHaveBeenCalled();

			await user.click(screen.getByTestId("confirm-dialog-cancel"));

			expect(onDismissAll).not.toHaveBeenCalled();
		});

		it("sends the gone and stale ids on confirm, never a suspect id", async () => {
			const user = userEvent.setup();
			const onDismissAll = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissAll={onDismissAll}
					providers={[
						prov({
							gone: [claimOf("g1", "pending")],
							stale: [claimOf("s1", "pending", "stale")],
							suspect: [claimOf("q1", "pending", "suspect")],
						}),
					]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-all"));
			await user.click(screen.getByTestId("discrepancy-dismiss-all-confirm"));

			// ConfirmDialog fires onConfirm from Modal's onClose, i.e. after the fade
			// finishes, so this is asynchronous however synchronous the click looks.
			await waitFor(() => expect(onDismissAll).toHaveBeenCalledTimes(1));
			expect(onDismissAll).toHaveBeenCalledWith("p1", ["g1", "s1"]);
		});

		it("excludes rows that are already cleared from the batch", async () => {
			const user = userEvent.setup();
			const onDismissAll = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissAll={onDismissAll}
					providers={[
						prov({
							gone: [
								claimOf("live", "pending"),
								claimOf("already", "dismissed"),
								claimOf("back", "resolved"),
							],
						}),
					]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-all"));
			await user.click(screen.getByTestId("discrepancy-dismiss-all-confirm"));

			await waitFor(() =>
				expect(onDismissAll).toHaveBeenCalledWith("p1", ["live"]),
			);
		});
	});

	describe("modal-wide dismiss all", () => {
		it("batches every provider's dismissible ids, one batch per provider", async () => {
			// The endpoint is provider-scoped, so a modal-wide dismiss is N requests
			// however it is presented. The modal's job is to hand over the batches;
			// Layout turns them into one confirm, one refresh and one toast.
			const user = userEvent.setup();
			const onDismissEverything = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissEverything={onDismissEverything}
					providers={[
						prov({
							gone: [claimOf("g1", "pending")],
							stale: [claimOf("s1", "pending", "stale")],
							suspect: [claimOf("q1", "pending", "suspect")],
						}),
						prov({
							provider_id: "p2",
							provider_name: "OpenRouter",
							gone: [claimOf("g2", "pending")],
						}),
					]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-everything"));
			await user.click(
				screen.getByTestId("discrepancy-dismiss-everything-confirm"),
			);

			await waitFor(() => expect(onDismissEverything).toHaveBeenCalledTimes(1));
			// Suspect is excluded here for the same reason as on the pill: the server
			// refuses a still-enabled model.
			expect(onDismissEverything).toHaveBeenCalledWith([
				{ providerID: "p1", modelIDs: ["g1", "s1"] },
				{ providerID: "p2", modelIDs: ["g2"] },
			]);
		});

		it("asks first and sends nothing when cancelled", async () => {
			const user = userEvent.setup();
			const onDismissEverything = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissEverything={onDismissEverything}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-everything"));
			expect(onDismissEverything).not.toHaveBeenCalled();

			await user.click(screen.getByTestId("confirm-dialog-cancel"));

			expect(onDismissEverything).not.toHaveBeenCalled();
		});

		it("omits providers with nothing dismissible from the batches", async () => {
			const user = userEvent.setup();
			const onDismissEverything = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissEverything={onDismissEverything}
					providers={[
						prov({ suspect: [claimOf("q1", "pending", "suspect")] }),
						prov({
							provider_id: "p2",
							provider_name: "OpenRouter",
							gone: [claimOf("g2", "pending")],
						}),
					]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-dismiss-everything"));
			await user.click(
				screen.getByTestId("discrepancy-dismiss-everything-confirm"),
			);

			await waitFor(() =>
				expect(onDismissEverything).toHaveBeenCalledWith([
					{ providerID: "p2", modelIDs: ["g2"] },
				]),
			);
		});

		it("is absent when nothing anywhere is dismissible", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ suspect: [claimOf("q1", "pending", "suspect")] })]}
				/>,
			);

			// Hidden rather than disabled, matching Retest all beside it.
			expect(screen.queryByTestId("discrepancy-dismiss-everything")).toBeNull();
		});

		it("excludes a cleaned provider from the batches", async () => {
			// Clean is view-only, so a cleaned provider must not be swept back in by
			// the modal-wide action either.
			const user = userEvent.setup();
			const onDismissEverything = vi.fn();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					onDismissEverything={onDismissEverything}
					providers={[
						prov({ gone: [claimOf("done", "dismissed")] }),
						prov({
							provider_id: "p2",
							provider_name: "OpenRouter",
							gone: [claimOf("g2", "pending")],
						}),
					]}
				/>,
			);
			await user.click(screen.getByTestId("discrepancy-clean"));

			await user.click(screen.getByTestId("discrepancy-dismiss-everything"));
			await user.click(
				screen.getByTestId("discrepancy-dismiss-everything-confirm"),
			);

			await waitFor(() =>
				expect(onDismissEverything).toHaveBeenCalledWith([
					{ providerID: "p2", modelIDs: ["g2"] },
				]),
			);
		});
	});

	describe("chip tooltips", () => {
		it("explains what each bucket means", () => {
			// "Gone" on its own does not say gone from where.
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "pending")],
							stale: [claimOf("old", "pending", "stale")],
							suspect: [claimOf("q", "pending", "suspect")],
						}),
					]}
				/>,
			);

			for (const key of ["gone", "stale", "suspect"]) {
				const chip = screen.getByTestId(`discrepancy-chip-${key}`);
				const tip = chip.getAttribute("title");
				// Compared against the label rather than spelled out, so this holds in
				// every locale: a tooltip that just repeats the chip explains nothing.
				expect(tip).toBeTruthy();
				expect(tip).not.toBe(chip.textContent);
				expect((tip as string).length).toBeGreaterThan(20);
			}
		});

		it("explains the cleared chips too", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[
						prov({
							gone: [claimOf("a", "dismissed"), claimOf("b", "resolved")],
						}),
					]}
				/>,
			);

			for (const key of ["dismissed", "resolved"]) {
				expect(screen.getByTestId(`discrepancy-chip-${key}`)).toHaveAttribute(
					"title",
				);
			}
		});
	});

	describe("journal summary", () => {
		it("summarises the collapsed journal and hides the line when expanded", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					// A claim keeps the journal collapsed on open, which is where the
					// operator actually meets it.
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[
						infoEntry,
						{ ...infoEntry, detected_at: "2026-07-04T00:00:00Z" },
					]}
				/>,
			);
			expect(
				screen.getByTestId("discrepancy-journal-summary"),
			).toBeInTheDocument();

			await user.click(screen.getByTestId("discrepancy-informational-toggle"));

			expect(screen.queryByTestId("discrepancy-journal-summary")).toBeNull();
		});

		it("uses different copy for a single entry than for a range", () => {
			// One entry takes the journalSummaryOne branch, which interpolates a single
			// timestamp; two or more take journalSummary, which interpolates newest and
			// oldest. Comparing the two rendered strings pins that the branch exists
			// without asserting on any translated wording.
			const { rerender } = render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[infoEntry]}
				/>,
			);
			const single = screen.getByTestId(
				"discrepancy-journal-summary",
			).textContent;

			rerender(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
					informational={[
						infoEntry,
						{ ...infoEntry, detected_at: "2026-07-04T00:00:00Z" },
					]}
				/>,
			);
			const range = screen.getByTestId(
				"discrepancy-journal-summary",
			).textContent;

			expect(single).toBeTruthy();
			expect(range).toBeTruthy();
			expect(single).not.toBe(range);
		});
	});

	describe("return to top", () => {
		// jsdom has no layout, so the shared setup's IntersectionObserver stub never
		// reports an intersection. These cases replace it with one that hands the
		// callback back, which is the only way to exercise the observed behaviour.
		let fire: ((entries: IntersectionObserverEntry[]) => void) | null = null;
		let observed: Element | null = null;

		beforeEach(() => {
			fire = null;
			observed = null;
			Element.prototype.scrollIntoView = vi.fn();
			vi.stubGlobal(
				"IntersectionObserver",
				class {
					readonly root = null;
					readonly rootMargin = "";
					readonly thresholds: number[] = [];
					constructor(cb: (entries: IntersectionObserverEntry[]) => void) {
						fire = cb;
					}
					observe(target: Element) {
						observed = target;
					}
					unobserve() {}
					disconnect() {}
					takeRecords(): IntersectionObserverEntry[] {
						return [];
					}
				},
			);
		});

		afterEach(() => {
			vi.unstubAllGlobals();
		});

		const offscreen = async () => {
			await act(async () => {
				fire?.([{ isIntersecting: false } as IntersectionObserverEntry]);
			});
		};

		it("is absent while no provider is open", () => {
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			// Nothing to return to: the observer is not even attached.
			expect(screen.queryByTestId("discrepancy-return-to-top")).toBeNull();
		});

		it("watches the pill row, not the whole provider section", async () => {
			// The bug this pins cost a real debugging round: observing the <section>
			// meant an unrolled provider stayed "intersecting" for as long as its open
			// bucket filled the viewport, so the control never appeared however far the
			// header scrolled away. The observed node must contain the pill and NOT the
			// model rows.
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			await openBucket(user, "gone");

			expect(observed).not.toBeNull();
			const target = observed as unknown as HTMLElement;
			expect(
				target.querySelector("[data-testid='discrepancy-provider-pill']"),
			).not.toBeNull();
			expect(
				target.querySelector("[data-testid='discrepancy-claim']"),
			).toBeNull();
		});

		it("stays hidden while the open provider's header is in view", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);

			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			expect(screen.queryByTestId("discrepancy-return-to-top")).toBeNull();
		});

		it("appears once the open provider's header scrolls out of view", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);
			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			await offscreen();

			expect(
				screen.getByTestId("discrepancy-return-to-top"),
			).toBeInTheDocument();
		});

		it("scrolls without collapsing what the operator is reading", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);
			await openBucket(user, "gone");
			await offscreen();

			await user.click(screen.getByTestId("discrepancy-return-to-top"));

			// Collapsing here would defeat the whole point of the control.
			expect(screen.getByTestId("discrepancy-provider-pill")).toHaveAttribute(
				"aria-expanded",
				"true",
			);
			expect(
				screen.getByTestId("discrepancy-group-gone-toggle"),
			).toHaveAttribute("aria-expanded", "true");
			expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
		});

		it("goes away when the provider it belongs to is closed", async () => {
			const user = userEvent.setup();
			render(
				<ModelDiscrepancyModal
					{...baseProps}
					providers={[prov({ gone: [claimOf("a", "pending")] })]}
				/>,
			);
			await user.click(screen.getByTestId("discrepancy-provider-pill"));
			await offscreen();
			expect(
				screen.getByTestId("discrepancy-return-to-top"),
			).toBeInTheDocument();

			await user.click(screen.getByTestId("discrepancy-provider-pill"));

			expect(screen.queryByTestId("discrepancy-return-to-top")).toBeNull();
		});
	});
});
