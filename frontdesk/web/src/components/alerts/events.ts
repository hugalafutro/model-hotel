import type { TFunction } from "i18next";

// How an alert event catalog entry is displayed, shared by the Alerts card's
// picker and the wizard's events step so the two always agree: one severity
// palette, one labelling rule, one place to change either.

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
