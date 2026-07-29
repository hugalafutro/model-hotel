import { describe, expect, it } from "vitest";
import type { ModelClaim, ProviderClaims } from "../../api/types";
import { mergeClaims, toSnapshot } from "../useDiscrepancies";

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
