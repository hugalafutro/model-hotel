import {
	DEFAULT_APPRISE_URL,
	EMPTY_DRAFT,
	initialState,
	type WizardSeed,
} from "@web-shared/alerts/wizardState";
import { describe, expect, it } from "vitest";

const seed = (over: Partial<WizardSeed> = {}): WizardSeed => ({
	initialApiUrl: "http://apprise.lan:8000",
	savedTargets: ["ntfy://ntfy.lan/ops"],
	savedEvents: "backup.failed, ,member.down",
	catalog: [
		{ type: "backup.failed", defaultOn: true },
		{ type: "member.down", defaultOn: false },
		{ type: "quota.low", defaultOn: true },
	],
	startAt: 1,
	...over,
});

describe("initialState", () => {
	it("seeds a fresh run from the props", () => {
		const s = initialState(seed(), false);
		expect(s).toMatchObject({
			step: 1,
			minStep: 1,
			apiUrl: "http://apprise.lan:8000",
			probedUrl: "",
			apiStatus: null,
			draft: EMPTY_DRAFT,
			added: [],
			listSeen: false,
			saved: ["ntfy://ntfy.lan/ops"],
			done: false,
			sentAll: "none",
		});
	});

	it("starts at step 2 only for Add destination against a configured apprise", () => {
		expect(initialState(seed({ startAt: 2 }), false).step).toBe(2);
		expect(initialState(seed({ startAt: 2 }), false).minStep).toBe(2);
		const unconfigured = initialState(
			seed({ startAt: 2, initialApiUrl: "" }),
			false,
		);
		expect(unconfigured.step).toBe(1);
		expect(unconfigured.apiUrl).toBe(DEFAULT_APPRISE_URL);
	});

	it("reads the stored selection when the app's rule keeps it", () => {
		expect(initialState(seed(), false).events).toEqual(
			new Set(["backup.failed", "member.down"]),
		);
		// A never-written selection the rule still keeps reads as empty.
		expect(initialState(seed({ savedEvents: null }), false).events).toEqual(
			new Set(),
		);
	});

	it("seeds the recommended preset when the app's rule asks for it", () => {
		expect(initialState(seed(), true).events).toEqual(
			new Set(["backup.failed", "quota.low"]),
		);
	});
});
