import { describe, expect, it } from "vitest";
import type { CircuitBreakerProviderStatus } from "../../../api/types";
import {
	entryCircuitStatus,
	entryCircuitView,
	groupCircuitSummary,
} from "../entryCircuit";

function row(
	over: Partial<CircuitBreakerProviderStatus>,
): CircuitBreakerProviderStatus {
	return {
		provider_id: "p-1",
		provider_name: "Provider One",
		state: "closed",
		consecutive_fails: 0,
		provider_open: false,
		...over,
	};
}

describe("entryCircuitStatus", () => {
	it("reports nothing when the provider has no circuit at all", () => {
		expect(entryCircuitStatus(undefined, "gpt-4")).toBeUndefined();
	});

	it("reports nothing for a closed circuit, whatever else the row carries", () => {
		// A closed row is the answer, not an input to weigh against the other
		// fields: the breaker is routing to this provider, so nothing on the entry
		// may claim otherwise even if a verdict or a model list rode along.
		expect(
			entryCircuitStatus(row({ state: "closed" }), "gpt-4"),
		).toBeUndefined();
		expect(
			entryCircuitStatus(
				row({ state: "closed", provider_open: true, open_models: ["gpt-4"] }),
				"gpt-4",
			),
		).toBeUndefined();
	});

	it("reports the row for the model the breaker is blocking", () => {
		const status = row({
			state: "open",
			provider_open: false,
			open_models: ["gpt-4"],
		});
		expect(entryCircuitStatus(status, "gpt-4")).toBe(status);
	});

	it("leaves a sibling model alone while only one model is blocked", () => {
		// The whole point of keying circuits per model: a row saying "open" now
		// means some model of this provider is dark, not this one. At the default
		// span of 2 the provider is still serving every other model, so the
		// sibling entry must render as the healthy, routable member it is.
		const status = row({
			state: "open",
			provider_open: false,
			open_models: ["gpt-4"],
		});
		expect(entryCircuitStatus(status, "gpt-4o-mini")).toBeUndefined();
	});

	it("reports the row on every entry once the provider itself is skipped", () => {
		// Two models corroborate (or a quota pin fires): the derived verdict is
		// open, so the breaker turns away every model behind the provider,
		// including ones with no circuit of their own.
		const status = row({
			state: "open",
			provider_open: true,
			open_models: ["gpt-4", "gpt-4o"],
		});
		expect(entryCircuitStatus(status, "gpt-4o-mini")).toBe(status);
	});

	it("reports a half-open row on every entry of the provider", () => {
		// A circuit owed a probe names no model: open_models carries only circuits
		// still blocking. There is nothing to attribute it to, and "recovering"
		// never reads as "down", so it stays on every entry.
		const status = row({ state: "half-open", provider_open: false });
		expect(entryCircuitStatus(status, "gpt-4o-mini")).toBe(status);
	});
});

// The per-entry view behind the chips and the group header: the entry's own
// circuit when the member reports circuits[], the row's open_models on an
// older member, busy only inside the window, and the header's live count and
// earliest retry.
describe("entryCircuitView", () => {
	const now = Date.parse("2026-08-31T14:48:00Z");
	const row = (
		over: Partial<CircuitBreakerProviderStatus>,
	): CircuitBreakerProviderStatus => ({
		provider_id: "p1",
		state: "closed",
		consecutive_fails: 0,
		provider_open: false,
		...over,
	});

	it("is live with no row, and on a closed circuit whose last verdict was a success", () => {
		expect(entryCircuitView(undefined, "m", now).chip).toBe("live");
		const v = entryCircuitView(
			row({
				circuits: [
					{
						model: "m",
						state: "closed",
						consecutive_fails: 0,
						last_cause: "success",
						last_at: "2026-08-31T14:47:50Z",
					},
				],
			}),
			"m",
			now,
		);
		expect(v.chip).toBe("live");
		expect(v.lastCause).toBe("success");
	});

	it("is busy only for a saturated verdict inside the window", () => {
		const saturated = (at: string) =>
			row({
				circuits: [
					{
						model: "m",
						state: "closed",
						consecutive_fails: 0,
						last_cause: "upstream status 429 (saturated)",
						last_status: 429,
						last_at: at,
					},
				],
			});
		expect(
			entryCircuitView(saturated("2026-08-31T14:47:30Z"), "m", now).chip,
		).toBe("busy");
		expect(
			entryCircuitView(saturated("2026-08-31T14:40:00Z"), "m", now).chip,
		).toBe("live");
	});

	it("reads open, pinned and probe off the entry's own circuit, not its sibling's", () => {
		const r = row({
			state: "open",
			open_models: ["dark"],
			circuits: [
				{
					model: "dark",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
					pin_source: "advisor",
					next_retry_at: "2026-08-31T18:41:00Z",
					last_cause: "upstream status 429 (exhausted)",
					last_status: 429,
				},
				{ model: "probing", state: "half-open", consecutive_fails: 5 },
				{ model: "fine", state: "closed", consecutive_fails: 0 },
			],
		});
		const dark = entryCircuitView(r, "dark", now);
		expect(dark.chip).toBe("pinned");
		expect(dark.nextRetryAt).toBe("2026-08-31T18:41:00Z");
		expect(entryCircuitView(r, "probing", now).chip).toBe("probe");
		expect(entryCircuitView(r, "fine", now).chip).toBe("live");
		const unpinned = row({
			state: "open",
			circuits: [
				{
					model: "dark",
					state: "open",
					consecutive_fails: 5,
					next_retry_at: "2026-08-31T14:49:00Z",
				},
			],
		});
		expect(entryCircuitView(unpinned, "dark", now).chip).toBe("open");
	});

	it("turns every entry of a skipped provider away, pinned when a pin holds it", () => {
		const r = row({
			state: "open",
			provider_open: true,
			quota_pinned: true,
			next_retry_at: "2026-08-31T18:41:00Z",
			open_models: ["a", "b"],
			circuits: [
				{ model: "a", state: "open", consecutive_fails: 5 },
				{ model: "c", state: "closed", consecutive_fails: 0 },
			],
		});
		expect(entryCircuitView(r, "c", now).chip).toBe("pinned");
		expect(entryCircuitView(r, "c", now).nextRetryAt).toBe(
			"2026-08-31T18:41:00Z",
		);
	});

	it("falls back to open_models on a member that reports no circuits", () => {
		const r = row({
			state: "open",
			open_models: ["dark"],
			next_retry_at: "2026-08-31T14:49:00Z",
		});
		expect(entryCircuitView(r, "dark", now).chip).toBe("open");
		expect(entryCircuitView(r, "dark", now).nextRetryAt).toBe(
			"2026-08-31T14:49:00Z",
		);
		expect(entryCircuitView(r, "fine", now).chip).toBe("live");
		expect(entryCircuitView(row({ state: "half-open" }), "any", now).chip).toBe(
			"probe",
		);
	});

	it("does not paint an entry from a sibling model's circuit when circuits[] is reported", () => {
		// The row says half-open, but that is another model of the provider;
		// this entry has never been routed and has no circuit of its own.
		const r = row({
			state: "half-open",
			circuits: [{ model: "other", state: "half-open", consecutive_fails: 5 }],
		});
		expect(entryCircuitView(r, "mine", now).chip).toBe("live");
		expect(entryCircuitView(r, "other", now).chip).toBe("probe");
	});
});

describe("groupCircuitSummary", () => {
	it("counts live, busy and probing entries as live and names the earliest retry when all are dark", () => {
		expect(
			groupCircuitSummary([
				{ chip: "live" },
				{ chip: "busy" },
				{ chip: "open", nextRetryAt: "2026-08-31T15:00:00Z" },
			]),
		).toEqual({
			live: 2,
			total: 3,
			allDark: false,
			earliestRetryAt: "2026-08-31T15:00:00Z",
		});
		expect(
			groupCircuitSummary([
				{ chip: "open", nextRetryAt: "2026-08-31T16:00:00Z" },
				{ chip: "pinned", nextRetryAt: "2026-08-31T15:00:00Z" },
			]),
		).toEqual({
			live: 0,
			total: 2,
			allDark: true,
			earliestRetryAt: "2026-08-31T15:00:00Z",
		});
		expect(groupCircuitSummary([])).toEqual({
			live: 0,
			total: 0,
			allDark: false,
			earliestRetryAt: undefined,
		});
	});
});
