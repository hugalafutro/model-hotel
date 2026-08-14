import { describe, expect, it } from "vitest";
import type { ModelClaim, ProviderClaims } from "../../api/types";
import {
	markDismissed,
	mergeClaims,
	providerHasNoPending,
	toSnapshot,
} from "../useDiscrepancies";

const claim = (
	model_id: string,
	over: Partial<ModelClaim> = {},
): ModelClaim => ({
	model_id,
	state: "gone",
	last_seen_at: "2026-07-01T00:00:00Z",
	missing_scans: 0,
	flap_window: 0,
	flap_since_review: 0,
	...over,
});

const provider = (over: Partial<ProviderClaims> = {}): ProviderClaims => ({
	provider_id: "p1",
	provider_name: "NanoGPT",
	gone: [],
	stale: [],
	suspect: [],
	retired: [],
	pinned: [],
	...over,
});

/** What a server predating the operator pin serves: no `pinned` key at all. */
const legacyProvider = (over: Partial<ProviderClaims> = {}): ProviderClaims => {
	const { pinned: _pinned, ...rest } = provider(over);
	return rest as ProviderClaims;
};

describe("toSnapshot", () => {
	it("seeds every claim as pending, regardless of group", () => {
		const snapshot = toSnapshot([
			provider({
				gone: [claim("a")],
				stale: [claim("b", { state: "stale" })],
				suspect: [claim("c", { state: "suspect" })],
			}),
		]);
		expect(snapshot[0].gone[0].status).toBe("pending");
		expect(snapshot[0].stale[0].status).toBe("pending");
		expect(snapshot[0].suspect[0].status).toBe("pending");
	});

	it("seeds the pinned bucket like any other", () => {
		const snapshot = toSnapshot([
			provider({
				pinned: [
					claim("k", { state: "pinned", pinned_at: "2026-07-15T00:00:00Z" }),
				],
			}),
		]);
		expect(snapshot[0].pinned.map((c) => [c.model_id, c.status])).toEqual([
			["k", "pending"],
		]);
	});

	it("treats a missing pinned bucket as empty", () => {
		// A rolling deploy puts an older server behind a newer dashboard, and that
		// server omits the key rather than sending [].
		const snapshot = toSnapshot([legacyProvider({ gone: [claim("a")] })]);
		expect(snapshot[0].pinned).toEqual([]);
		expect(snapshot[0].gone[0].status).toBe("pending");
	});
});

describe("mergeClaims", () => {
	it("keeps a claim that is still present as pending", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a")] })]);
		const merged = mergeClaims(snapshot, [provider({ gone: [claim("a")] })]);
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
		]);
	});

	it("marks a claim absent from the refetch as resolved without moving it", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a"), claim("b")] })]);
		const merged = mergeClaims(snapshot, [provider({ gone: [claim("b")] })]);
		// This is the defect the rework exists to fix: a claim that clears must
		// stay in position as `resolved`, never silently disappear.
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "resolved"],
			["b", "pending"],
		]);
	});

	it("appends a newly appeared claim", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a")] })]);
		const merged = mergeClaims(snapshot, [
			provider({ gone: [claim("a"), claim("c")] }),
		]);
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
			["c", "new"],
		]);
	});

	it("resolves every claim of a provider that drops out entirely, keeping its position", () => {
		const snapshot = toSnapshot([
			provider({ gone: [claim("a")] }),
			provider({
				provider_id: "p2",
				provider_name: "OpenRouter",
				gone: [claim("z")],
			}),
		]);
		const merged = mergeClaims(snapshot, [
			provider({
				provider_id: "p2",
				provider_name: "OpenRouter",
				gone: [claim("z")],
			}),
		]);
		expect(merged[0].provider_name).toBe("NanoGPT");
		expect(merged[0].gone[0].status).toBe("resolved");
		expect(merged[1].gone[0].status).toBe("pending");
	});

	it("moves a claim that changed bucket instead of listing it twice", () => {
		// A model degrading suspect -> gone is present in BOTH fetches, so it is
		// still pending. Reconciling each bucket on its own read it as two
		// unrelated facts: resolved under Suspect, new under Gone. The operator
		// then sees one model twice, once struck through as fixed, while it has
		// actually got worse.
		const snapshot = toSnapshot([
			provider({ suspect: [claim("a", { state: "suspect" })] }),
		]);
		const merged = mergeClaims(snapshot, [provider({ gone: [claim("a")] })]);
		expect(merged[0].suspect).toEqual([]);
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
		]);
	});

	it("moves a claim from gone to stale without resolving it", () => {
		// The other real transition: an unresolved claim ages past ClaimWindow.
		const snapshot = toSnapshot([provider({ gone: [claim("a")] })]);
		const merged = mergeClaims(snapshot, [
			provider({ stale: [claim("a", { state: "stale" })] }),
		]);
		expect(merged[0].gone).toEqual([]);
		expect(merged[0].stale.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
		]);
	});

	it("lands a moved claim after the destination's existing rows, shuffling none of them", () => {
		// Ordering contract: rows already in the destination keep their exact
		// index, the mover appends after them, and the refetch's own ordering
		// (which puts the mover first here) is deliberately ignored.
		const snapshot = toSnapshot([
			provider({
				gone: [claim("g1"), claim("g2")],
				suspect: [claim("s1", { state: "suspect" })],
			}),
		]);
		const merged = mergeClaims(snapshot, [
			provider({ gone: [claim("s1"), claim("g1"), claim("g2")] }),
		]);
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["g1", "pending"],
			["g2", "pending"],
			["s1", "pending"],
		]);
		expect(merged[0].suspect).toEqual([]);
	});

	it("keeps a resolved claim in its original bucket while another claim moves out of it", () => {
		// The invariant the whole rework exists for must survive cross-bucket
		// reconciliation: `s1` clears and stays struck through where it sat, even
		// though its neighbour `s2` leaves the bucket in the same merge.
		const snapshot = toSnapshot([
			provider({
				suspect: [
					claim("s1", { state: "suspect" }),
					claim("s2", { state: "suspect" }),
				],
			}),
		]);
		const merged = mergeClaims(snapshot, [provider({ gone: [claim("s2")] })]);
		expect(merged[0].suspect.map((c) => [c.model_id, c.status])).toEqual([
			["s1", "resolved"],
		]);
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["s2", "pending"],
		]);
	});

	it("appends genuinely new claims after movers", () => {
		const snapshot = toSnapshot([
			provider({ suspect: [claim("a", { state: "suspect" })] }),
		]);
		const merged = mergeClaims(snapshot, [
			provider({ gone: [claim("fresh"), claim("a")] }),
		]);
		// `fresh` is first in the refetch, but a mover the operator has already
		// seen outranks a row they have not.
		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
			["fresh", "new"],
		]);
	});

	it("appends a provider that appears only in the refetch", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a")] })]);
		const merged = mergeClaims(snapshot, [
			provider({ gone: [claim("a")] }),
			provider({
				provider_id: "p9",
				provider_name: "DeepInfra",
				suspect: [claim("s", { state: "suspect" })],
			}),
		]);
		expect(merged).toHaveLength(2);
		expect(merged[1].suspect[0].status).toBe("new");
	});

	it("merges a refetch that carries no pinned bucket", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a")] })]);
		const merged = mergeClaims(snapshot, [
			legacyProvider({ gone: [claim("a")] }),
		]);
		expect(merged[0].pinned).toEqual([]);
		expect(merged[0].gone[0].status).toBe("pending");
	});

	it("keeps an unpinned row cleared once the refetch stops reporting it", () => {
		// An unpinned model leaves the claims payload entirely: the pin is gone and
		// the miss streak is reset, so nothing is left to claim. Reading that
		// absence as "the provider is listing it again" would be false, which is
		// why the unpin path marks the row the same way a dismissal does.
		const snapshot = markDismissed(
			toSnapshot([
				provider({
					pinned: [
						claim("k", { state: "pinned", pinned_at: "2026-07-15T00:00:00Z" }),
					],
				}),
			]),
			"p1",
			new Set(["k"]),
		);
		const merged = mergeClaims(snapshot, [provider()]);
		expect(merged[0].pinned.map((c) => [c.model_id, c.status])).toEqual([
			["k", "dismissed"],
		]);
	});
});

describe("dismissed survives a refetch", () => {
	it("keeps a dismissed row dismissed when the refetch no longer reports it", () => {
		// The headline fix. A dismissed model is absent from the refetch BECAUSE it
		// was dismissed: listClaimRows filters on discovery_dismissed_at IS NULL.
		// Reading that absence as "the provider listed it again" made the cleared
		// summary announce every dismissal with `resolvedPlain`.
		const snapshot = markDismissed(
			toSnapshot([provider({ gone: [claim("a")] })]),
			"p1",
			new Set(["a"]),
		);
		expect(snapshot[0].gone[0].status).toBe("dismissed");

		const merged = mergeClaims(snapshot, [provider()]);

		expect(merged[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "dismissed"],
		]);
	});

	it("still resolves a row that was never dismissed", () => {
		// The other half of the same branch: absence with no dismissal behind it
		// does mean the provider is listing the model again.
		const merged = mergeClaims(toSnapshot([provider({ gone: [claim("a")] })]), [
			provider(),
		]);

		expect(merged[0].gone[0].status).toBe("resolved");
	});

	it("rebuilds a still-reported row as pending when the server keeps reporting it", () => {
		// A model the server still reports is not dismissed, whatever the snapshot
		// says, so the refetch alone corrects the row. Nothing has to roll anything
		// back: the merge is the only thing that sets status from server truth.
		const fresh = [provider({ gone: [claim("a")] })];
		const snapshot = markDismissed(toSnapshot(fresh), "p1", new Set(["a"]));

		const merged = mergeClaims(snapshot, fresh);

		expect(merged[0].gone[0].status).toBe("pending");
	});
});

describe("bulk dismiss", () => {
	it("dismisses every id in the set across buckets and leaves the rest alone", () => {
		const snapshot = markDismissed(
			toSnapshot([
				provider({
					gone: [claim("a"), claim("b")],
					stale: [claim("c", { state: "stale" })],
					suspect: [claim("d", { state: "suspect" })],
				}),
			]),
			"p1",
			new Set(["a", "c"]),
		);

		expect(snapshot[0].gone.map((c) => c.status)).toEqual([
			"dismissed",
			"pending",
		]);
		expect(snapshot[0].stale[0].status).toBe("dismissed");
		expect(snapshot[0].suspect[0].status).toBe("pending");
	});

	it("is false while a suspect row is still pending", () => {
		// Suspect can never be dismissed (the server refuses a still-enabled
		// model), so it is the case that keeps a provider actionable.
		const [p] = toSnapshot([
			provider({ suspect: [claim("d", { state: "suspect" })] }),
		]);

		expect(providerHasNoPending(p)).toBe(false);
	});

	it("is true when rows are a mix of dismissed and resolved", () => {
		const base = toSnapshot([
			provider({
				gone: [claim("a")],
				stale: [claim("b", { state: "stale" })],
			}),
		]);
		const dismissed = markDismissed(base, "p1", new Set(["a"]));
		// `b` was never dismissed, so its absence resolves it.
		const merged = mergeClaims(dismissed, [provider()]);

		expect(merged[0].gone[0].status).toBe("dismissed");
		expect(merged[0].stale[0].status).toBe("resolved");
		expect(providerHasNoPending(merged[0])).toBe(true);
	});
});
