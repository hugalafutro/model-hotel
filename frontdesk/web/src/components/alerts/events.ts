import type { TFunction } from "i18next";

// The alert event catalog as the Alerts card's picker and the wizard's events
// step both see it, so the two always agree: one severity palette, one
// labelling rule, one reading of the stored selection, one place to change any
// of them.

/** Dot colour per catalog severity, in the Front Desk palette. */
export const SEVERITY_COLOR: Record<string, string> = {
	success: "var(--ok)",
	info: "var(--info)",
	warning: "var(--warn)",
	error: "var(--danger)",
};

// eventLabel is the friendly name for an event type. It falls back to the raw
// type so a brand-new server-side event still renders something readable before
// a string is added for it.
export function eventLabel(t: TFunction, type: string): string {
	return t(`settings.alerts.event.${type.replace(/\./g, "_")}`, {
		defaultValue: type,
	});
}

// parseCsv turns the stored alert_events CSV into a membership Set. Blank
// entries are dropped, so a trailing comma or a stray space never becomes an
// event type nothing matches.
export function parseCsv(csv: string): Set<string> {
	return new Set(
		csv
			.split(",")
			.map((s) => s.trim())
			.filter(Boolean),
	);
}
