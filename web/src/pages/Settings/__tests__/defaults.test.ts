import { describe, expect, it } from "vitest";
import { SECTION_SETTINGS, SETTING_DEFAULTS } from "../defaults";

describe("SETTING_DEFAULTS", () => {
	it("has a default for every known setting key", () => {
		const allKeys = Object.values(SECTION_SETTINGS).flat();
		for (const key of allKeys) {
			expect(SETTING_DEFAULTS[key], `Missing default for ${key}`).toBeDefined();
		}
	});

	it("has no duplicate keys across sections", () => {
		const allKeys = Object.values(SECTION_SETTINGS).flat();
		const unique = new Set(allKeys);
		expect(unique.size).toBe(allKeys.length);
	});

	it("covers all sections in SECTION_SETTINGS", () => {
		expect(Object.keys(SECTION_SETTINGS)).toEqual([
			"discovery",
			"proxy",
			"rateLimit",
			"circuitBreaker",
			"dataStorage",
		]);
	});

	it("numeric-like defaults parse as valid numbers", () => {
		const numericKeys = [
			"rate_limit_rps",
			"rate_limit_burst",
			"rate_limit_ip_rps",
			"rate_limit_ip_burst",
			"rate_limit_max_wait_ms",
			"circuit_breaker_threshold",
		];
		for (const key of numericKeys) {
			expect(
				Number(SETTING_DEFAULTS[key]),
				`${key} default should be numeric`,
			).not.toBeNaN();
		}
	});

	it("boolean-like defaults are true or false strings", () => {
		const boolKeys = [
			"discovery_on_startup",
			"discovery_on_provider_create",
			"rate_limit_enabled",
			"rate_limit_ip_enabled",
			"circuit_breaker_enabled",
			"failover_on_rate_limit",
		];
		for (const key of boolKeys) {
			expect(["true", "false"], `${key} default should be boolean`).toContain(
				SETTING_DEFAULTS[key],
			);
		}
	});
});
