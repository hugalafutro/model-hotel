import {
	categoryLabel,
	eventLabel,
	groupByCategory,
} from "@web-shared/alerts/events";
import { describe, expect, it } from "vitest";

// A translator that knows one key and reports the default for the rest, so
// the tests pin the key shape without booting i18next.
const t = (key: string, options: { defaultValue: string }) =>
	key === "settings.alerts.category.config_sync"
		? "Sync"
		: options.defaultValue;

describe("categoryLabel", () => {
	it("slugs the server's display category into a locale key", () => {
		expect(categoryLabel(t, "Config Sync")).toBe("Sync");
	});

	it("falls back to the category itself when the locale has no entry", () => {
		expect(categoryLabel(t, "Brand New")).toBe("Brand New");
	});
});

describe("eventLabel", () => {
	it("falls back to the event type when the locale has no entry", () => {
		expect(eventLabel(t, "health.down")).toBe("health.down");
	});
});

describe("groupByCategory", () => {
	it("buckets by category, keeping server order", () => {
		const defs = [
			{ type: "a", category: "Sync" },
			{ type: "b", category: "Quota" },
			{ type: "c", category: "Sync" },
		];

		expect(groupByCategory(defs)).toEqual([
			[
				"Sync",
				[
					{ type: "a", category: "Sync" },
					{ type: "c", category: "Sync" },
				],
			],
			["Quota", [{ type: "b", category: "Quota" }]],
		]);
	});

	it("is empty for an empty catalog", () => {
		expect(groupByCategory([])).toEqual([]);
	});
});
