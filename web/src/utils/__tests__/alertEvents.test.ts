import { categoryLabel, eventLabel } from "@web-shared/alerts/events";
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
