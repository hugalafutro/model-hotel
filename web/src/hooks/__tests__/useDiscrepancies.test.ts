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
