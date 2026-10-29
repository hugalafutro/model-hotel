import {
	canNext,
	DEFAULT_APPRISE_URL,
	EMPTY_DRAFT,
	initialState,
	reducer,
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

describe("reducer and gates at their edges", () => {
	it("a step outside the wizard never advances", () => {
		const s = initialState(seed(), false);
		expect(canNext({ ...s, step: 8 as unknown as typeof s.step })).toBe(false);
	});

	it("ignores a field edit before a destination kind is chosen", () => {
		const s = { ...initialState(seed(), false), draft: EMPTY_DRAFT };
		expect(reducer(s, { type: "setField", key: "host", value: "x" })).toBe(s);
	});

	it("toggles one event type on and off without touching the rest", () => {
		const s = initialState(seed({ savedEvents: "member.down" }), false);
		const on = reducer(s, {
			type: "toggleEvent",
			eventType: "backup.failed",
			on: true,
		});
		expect(on.events).toEqual(new Set(["member.down", "backup.failed"]));
		// A new Set, so the previous state is unchanged.
		expect(s.events).toEqual(new Set(["member.down"]));
		const off = reducer(on, {
			type: "toggleEvent",
			eventType: "member.down",
			on: false,
		});
		expect(off.events).toEqual(new Set(["backup.failed"]));
	});
});
