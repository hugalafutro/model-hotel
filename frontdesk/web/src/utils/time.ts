import i18next from "i18next";

// formatRelative renders an ISO timestamp as a localized relative time
// ("5 minutes ago"), falling back to "never" for an empty/zero value. Uses the
// active i18next language so it tracks the rest of the UI.
export function formatRelative(iso: string | undefined): string {
	if (!iso) return i18next.t("common.never");
	const then = new Date(iso).getTime();
	if (Number.isNaN(then) || then <= 0) return i18next.t("common.never");
	const diffMs = then - Date.now();
	const rtf = new Intl.RelativeTimeFormat(i18next.language, {
		numeric: "auto",
	});
	const units: [Intl.RelativeTimeFormatUnit, number][] = [
		["day", 86_400_000],
		["hour", 3_600_000],
		["minute", 60_000],
		["second", 1000],
	];
	for (const [unit, ms] of units) {
		if (Math.abs(diffMs) >= ms || unit === "second") {
			return rtf.format(Math.round(diffMs / ms), unit);
		}
	}
	return rtf.format(0, "second");
}

// formatTimeOfDay renders an ISO timestamp as the active locale's wall-clock
// time only (e.g. "1:45:30 PM"), for "last updated" labels where the date is
// implied and only the time-of-day matters. Falls back to "never" for an
// empty/invalid value.
export function formatTimeOfDay(iso: string | undefined): string {
	if (!iso) return i18next.t("common.never");
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return i18next.t("common.never");
	return new Intl.DateTimeFormat(i18next.language, {
		timeStyle: "medium",
	}).format(d);
}

// formatAbsolute renders an ISO timestamp in the active locale's date+time
// format, for tables where an exact time matters more than recency.
export function formatAbsolute(iso: string | undefined): string {
	if (!iso) return i18next.t("common.never");
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return i18next.t("common.never");
	return new Intl.DateTimeFormat(i18next.language, {
		dateStyle: "medium",
		timeStyle: "medium",
	}).format(d);
}
