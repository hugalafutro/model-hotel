import { describe, expect, it } from "vitest";
import type { CircuitBreakerProviderStatus } from "../../../api/types";
import { entryCircuitStatus } from "../entryCircuit";

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
