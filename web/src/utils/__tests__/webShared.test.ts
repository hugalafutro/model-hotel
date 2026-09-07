import { groupByCategory } from "@web-shared/alerts/events";
import {
	type Action,
	canNext,
	EMPTY_DRAFT,
	isDuplicate,
	newDraft,
	reducer,
	type WizardState,
} from "@web-shared/alerts/wizardState";
import { clamp, formatCount } from "@web-shared/format";
import { localeCodes } from "@web-shared/i18n";
import { getZaiCodingMcpLimit } from "@web-shared/quota";
import { describe, expect, it } from "vitest";

describe("clamp", () => {
	it("confines a value to the range", () => {
		expect(clamp(5, 0, 10)).toBe(5);
		expect(clamp(-1, 0, 10)).toBe(0);
		expect(clamp(11, 0, 10)).toBe(10);
	});
});

describe("formatCount", () => {
	it("groups digits without abbreviating", () => {
		expect(formatCount(1249)).toBe("1,249");
		expect(formatCount(0)).toBe("0");
	});

	it("reads an absent count as a dash", () => {
		expect(formatCount(null)).toBe("-");
		expect(formatCount(undefined)).toBe("-");
	});
});

describe("localeCodes", () => {
	it("inverts the ./locales/<lang>.json key convention", () => {
		expect(
			localeCodes({
				"./locales/de.json": async () => ({ default: {} }),
				"./locales/zh.json": async () => ({ default: {} }),
			}),
		).toEqual(["de", "zh"]);
	});
});

describe("getZaiCodingMcpLimit", () => {
	const mcp = { type: "TIME_LIMIT", unit: 5, percentage: 40 };

	it("finds the TIME_LIMIT entry on unit 5", () => {
		expect(
			getZaiCodingMcpLimit({
				data: {
					limits: [{ type: "TOKENS_LIMIT", unit: 3 }, mcp],
				},
			}),
		).toEqual(mcp);
	});

	it("is undefined without one", () => {
		expect(getZaiCodingMcpLimit({ data: { limits: [] } })).toBeUndefined();
		expect(getZaiCodingMcpLimit(null)).toBeUndefined();
	});
});

describe("shared alerts wizard machine", () => {
	const base: WizardState = {
		step: 4,
		minStep: 1,
		apiUrl: "http://apprise:8000",
		probedUrl: "http://apprise:8000",
		apiStatus: { configured: true, reachable: true, healthy: true },
		apiChecking: false,
		draft: { ...EMPTY_DRAFT, kind: "ntfy", url: "ntfy://host/topic" },
		added: ["ntfy://host/other"],
		listSeen: false,
		saved: [],
		events: new Set<string>(),
		testing: false,
		testError: "",
		testOk: false,
		finishing: false,
		finishError: "",
		done: false,
		finalStatus: null,
		sendingAll: false,
		sentAll: "none",
	};
	const run = (state: WizardState, action: Action) => reducer(state, action);

	it("keeps the run's proofs while the address field is merely edited", () => {
		const next = run(base, { type: "setApiUrl", value: "http://other:8000" });

		expect(next.added).toEqual(["ntfy://host/other"]);
		expect(next.apiUrl).toBe("http://other:8000");
	});

	it("drops them once a different apprise is verified", () => {
		const next = run(base, {
			type: "probed",
			url: "http://other:8000",
			status: { configured: true, reachable: true, healthy: true },
			demote: false,
		});

		expect(next.added).toEqual([]);
		expect(next.draft.tested).toBe(false);
	});

	it("keeps them when the same apprise is re-probed", () => {
		const next = run(
			{ ...base, draft: { ...base.draft, tested: true } },
			{
				type: "probed",
				url: "http://apprise:8000",
				status: { configured: true, reachable: true, healthy: true },
				demote: false,
			},
		);

		expect(next.added).toEqual(["ntfy://host/other"]);
		expect(next.draft.tested).toBe(true);
	});

	it("falls back to step 1 when a shortcut probe comes back unhealthy", () => {
		const next = run(base, {
			type: "probed",
			url: "http://apprise:8000",
			status: { configured: true, reachable: false, healthy: false },
			demote: true,
		});

		expect(next.step).toBe(1);
		expect(next.minStep).toBe(1);
	});

	it("gates step 4 on a delivered test", () => {
		expect(canNext(base)).toBe(false);
		expect(canNext({ ...base, draft: { ...base.draft, tested: true } })).toBe(
			true,
		);
	});

	it("keeps an accepted destination once and lets an edit replace it", () => {
		const state: WizardState = {
			...base,
			step: 3,
			draft: newDraft("ntfy", "https://ntfy.example.com"),
			added: [],
		};
		const withTopic = reducer(state, {
			type: "setField",
			key: "topic",
			value: "alerts",
		});
		const accepted = reducer(withTopic, { type: "acceptDraft" });

		expect(accepted.step).toBe(5);
		expect(accepted.added).toEqual([accepted.draft.url]);
		expect(accepted.draft.acceptedUrl).toBe(accepted.draft.url);
		// Its own accepted row is not a duplicate of itself, so re-accepting an
		// edit replaces it instead of adding a second entry.
		expect(isDuplicate(accepted)).toBe(false);
		expect(
			isDuplicate({
				...accepted,
				draft: { ...accepted.draft, acceptedUrl: null },
			}),
		).toBe(true);

		const dropped = reducer(accepted, {
			type: "dropAdded",
			url: accepted.draft.url,
		});
		expect(dropped.added).toEqual([]);
		expect(dropped.draft.acceptedUrl).toBeNull();
	});
});

describe("groupByCategory re-export path", () => {
	it("is reachable from the shared alerts module", () => {
		expect(groupByCategory([{ category: "x" }])).toEqual([
			["x", [{ category: "x" }]],
		]);
	});
});
