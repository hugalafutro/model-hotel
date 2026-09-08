import { expect, it } from "vitest";
import { statusBadge } from "../events";

// The key each branch asks for, rather than the rendered sentence, so the test
// stays out of the locale catalogs.
const t = (key: string) => key;

// The card's pill and the wizard's closing pill read one probe the same way, so
// the classification is checked once, here, rather than through both renders.
it("classifies a probe as unreachable, unhealthy or ok", () => {
	expect(statusBadge({ reachable: false, healthy: false }, t)).toEqual({
		variant: "ui-badge-danger",
		label: "settings.alerts.statusUnreachable",
	});
	// Answering but not healthy: apprise is up and cannot deliver, which is a
	// warning rather than an outage.
	expect(statusBadge({ reachable: true, healthy: false }, t)).toEqual({
		variant: "ui-badge-warn",
		label: "settings.alerts.statusUnhealthy",
	});
	expect(statusBadge({ reachable: true, healthy: true }, t)).toEqual({
		variant: "ui-badge-ok",
		label: "settings.alerts.statusOk",
	});
	// Unreachable outranks a healthy flag: nothing was proven about delivery.
	expect(statusBadge({ reachable: false, healthy: true }, t).label).toBe(
		"settings.alerts.statusUnreachable",
	);
});
