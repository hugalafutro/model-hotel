import { expect, it } from "vitest";
import { statusBadge } from "../events";

// The card's pill and the wizard's closing pill read one probe the same way, so
// the classification is checked once, here, rather than through both renders.
it("classifies a probe as unreachable, unhealthy or ok", () => {
	expect(statusBadge({ reachable: false, healthy: false })).toEqual({
		variant: "ui-badge-danger",
		key: "statusUnreachable",
	});
	// Answering but not healthy: apprise is up and cannot deliver, which is a
	// warning rather than an outage.
	expect(statusBadge({ reachable: true, healthy: false })).toEqual({
		variant: "ui-badge-warn",
		key: "statusUnhealthy",
	});
	expect(statusBadge({ reachable: true, healthy: true })).toEqual({
		variant: "ui-badge-ok",
		key: "statusOk",
	});
	// Unreachable outranks a healthy flag: nothing was proven about delivery.
	expect(statusBadge({ reachable: false, healthy: true }).key).toBe(
		"statusUnreachable",
	);
});
