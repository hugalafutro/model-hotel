import { describe, expect, it } from "vitest";
import { isNaEntry, naReasonKey } from "../failoverEntry";

describe("naReasonKey", () => {
	it("returns null when model and provider are both live", () => {
		expect(
			naReasonKey({ model_enabled: true, provider_enabled: true }),
		).toBeNull();
	});

	it("flags a disabled provider first, even if the model is also disabled", () => {
		expect(
			naReasonKey({
				model_enabled: false,
				provider_enabled: false,
				disabled_manually: true,
			}),
		).toBe("failoverGroups.entry.reasonProviderOff");
	});

	it("distinguishes a hand-disabled model from a discovery-dropped one", () => {
		expect(
			naReasonKey({
				model_enabled: false,
				provider_enabled: true,
				disabled_manually: true,
			}),
		).toBe("failoverGroups.entry.reasonManuallyDisabled");
		expect(
			naReasonKey({
				model_enabled: false,
				provider_enabled: true,
				disabled_manually: false,
			}),
		).toBe("failoverGroups.entry.reasonRemovedByDiscovery");
	});
});

describe("isNaEntry", () => {
	it("is false only when both the model and provider are enabled", () => {
		expect(isNaEntry({ model_enabled: true, provider_enabled: true })).toBe(
			false,
		);
		expect(isNaEntry({ model_enabled: false, provider_enabled: true })).toBe(
			true,
		);
		expect(isNaEntry({ model_enabled: true, provider_enabled: false })).toBe(
			true,
		);
	});
});
