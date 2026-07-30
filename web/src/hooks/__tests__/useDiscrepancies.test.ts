import { describe, expect, it } from "vitest";
import type { ModelClaim, ProviderClaims } from "../../api/types";
import {
	dismissOptimistically,
	mergeClaims,
	providerHasNoPending,
	revertDismissal,
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
	...over,
});

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
});

describe("dismiss rollback", () => {
	it("reverts a claim that still holds its own optimistic write", () => {
		const snapshot = toSnapshot([provider({ gone: [claim("a"), claim("b")] })]);
		const after = revertDismissal(
			dismissOptimistically(snapshot, "p1", new Set(["a"])),
			"p1",
			new Set(["a"]),
		);
		expect(after[0].gone.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
			["b", "pending"],
		]);
	});

	it("reverts to the status the row actually held, not a hardcoded pending", () => {
		// A claim discovered mid-session carries `new`. A failed dismiss must not
		// quietly erase that marker.
		const snapshot = mergeClaims(toSnapshot([provider()]), [
			provider({ gone: [claim("a")] }),
		]);
		expect(snapshot[0].gone[0].status).toBe("new");
		const after = revertDismissal(
			dismissOptimistically(snapshot, "p1", new Set(["a"])),
			"p1",
			new Set(["a"]),
		);
		expect(after[0].gone[0].status).toBe("new");
	});

	it("touches only the addressed provider's copy of a model id", () => {
		// `model_id` is unique WITHIN a provider, not across them: two providers
		// commonly serve the same model name. Both halves address a claim by
		// (provider_id, model_id), so the other provider's row must not move.
		const snapshot = toSnapshot([
			provider({ gone: [claim("a")] }),
			provider({ provider_id: "p2", provider_name: "Two", gone: [claim("a")] }),
		]);
		const optimistic = dismissOptimistically(snapshot, "p1", new Set(["a"]));
		expect(optimistic[1].gone[0].status).toBe("pending");
		const after = revertDismissal(optimistic, "p1", new Set(["a"]));
		expect(after[0].gone[0].status).toBe("pending");
		expect(after[1]).toBe(snapshot[1]);
	});

	it("leaves a claim alone once a refresh has confirmed the dismissal", () => {
		// The rollback must only undo its OWN write. A refresh landing while the
		// request is out is newer authority: here the server stops reporting `a`,
		// which for a dismissed row CONFIRMS the dismissal (listClaimRows filters
		// out dismissed models), so the merge owns the row and a later failure must
		// not replay the click-time `pending` over it.
		//
		// The status stays `dismissed` rather than becoming `resolved`: absence is
		// the dismissal taking effect, not the provider listing the model again. A
		// dismiss that actually FAILED is covered by "rebuilds a still-reported row
		// as pending" below, where the server keeps reporting the model.
		const optimistic = dismissOptimistically(
			toSnapshot([provider({ gone: [claim("a")] })]),
			"p1",
			new Set(["a"]),
		);
		const refreshed = mergeClaims(optimistic, [provider()]);
		expect(revertDismissal(refreshed, "p1", new Set(["a"]))).toEqual(refreshed);
		expect(refreshed[0].gone[0].status).toBe("dismissed");
		expect(refreshed[0].gone[0].optimisticFrom).toBeUndefined();
	});

	it("leaves a claim a refresh moved to another bucket where the refresh put it", () => {
		// Identity is (provider_id, model_id), so the rollback still FINDS a claim
		// that changed bucket mid-request — and must still decline to touch it. The
		// row belongs to the merge now: it is not reverted in its new bucket and
		// no copy is resurrected in the old one.
		//
		// `a` is seeded as `new` so the two outcomes are distinguishable: the merge
		// makes the moved row `pending`, and a revert that ignored the compare would
		// stamp the click-time `new` back over it.
		const discovered = mergeClaims(toSnapshot([provider()]), [
			provider({ gone: [claim("a")] }),
		]);
		expect(discovered[0].gone[0].status).toBe("new");
		const optimistic = dismissOptimistically(discovered, "p1", new Set(["a"]));
		const refreshed = mergeClaims(optimistic, [
			provider({ stale: [claim("a", { state: "stale" })] }),
		]);
		const after = revertDismissal(refreshed, "p1", new Set(["a"]));
		expect(after[0].gone).toEqual([]);
		expect(after[0].stale.map((c) => [c.model_id, c.status])).toEqual([
			["a", "pending"],
		]);
	});
});

describe("dismissed survives a refetch", () => {
	it("keeps a dismissed row dismissed when the refetch no longer reports it", () => {
		// The headline fix. A dismissed model is absent from the refetch BECAUSE it
		// was dismissed: listClaimRows filters on discovery_dismissed_at IS NULL.
		// Reading that absence as "the provider listed it again" made the cleared
		// summary announce every dismissal with `resolvedPlain`.
		const snapshot = dismissOptimistically(
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

	it("rebuilds a still-reported row as pending after a failed dismiss", () => {
		// A failed POST leaves the model undismissed, so the server keeps reporting
		// it and the refetch alone corrects the row. This is why stripping
		// optimisticFrom is safe: the rollback becoming a no-op costs nothing.
		const fresh = [provider({ gone: [claim("a")] })];
		const snapshot = dismissOptimistically(
			toSnapshot(fresh),
			"p1",
			new Set(["a"]),
		);

		const merged = mergeClaims(snapshot, fresh);

		expect(merged[0].gone[0].status).toBe("pending");
		expect(merged[0].gone[0].optimisticFrom).toBeUndefined();
	});
});

describe("bulk dismiss", () => {
	it("dismisses every id in the set across buckets and leaves the rest alone", () => {
		const snapshot = dismissOptimistically(
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

	it("reverts every id in the set to its prior status", () => {
		const base = toSnapshot([provider({ gone: [claim("a"), claim("b")] })]);
		const dismissed = dismissOptimistically(base, "p1", new Set(["a", "b"]));

		const reverted = revertDismissal(dismissed, "p1", new Set(["a", "b"]));

		expect(reverted[0].gone.map((c) => c.status)).toEqual([
			"pending",
			"pending",
		]);
		expect(reverted[0].gone[0].optimisticFrom).toBeUndefined();
	});
});

describe("providerHasNoPending", () => {
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
		const dismissed = dismissOptimistically(base, "p1", new Set(["a"]));
		// `b` was never dismissed, so its absence resolves it.
		const merged = mergeClaims(dismissed, [provider()]);

		expect(merged[0].gone[0].status).toBe("dismissed");
		expect(merged[0].stale[0].status).toBe("resolved");
		expect(providerHasNoPending(merged[0])).toBe(true);
	});
});
