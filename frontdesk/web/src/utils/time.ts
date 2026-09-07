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
		if (Math.abs(diffMs) >= ms)
			return rtf.format(Math.round(diffMs / ms), unit);
	}
	return rtf.format(Math.round(diffMs / 1000), "second");
}

// fmt is the shared body of the absolute formatters below: guard an empty or
// unparseable value with the caller's fallback, then render the date with the
// caller's Intl options in the active i18next language.
function fmt(
	iso: string | undefined,
	opts: Intl.DateTimeFormatOptions,
	invalid: string,
): string {
	if (!iso) return invalid;
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return invalid;
	return new Intl.DateTimeFormat(i18next.language, opts).format(d);
}

// formatTimeOfDay renders an ISO timestamp as the active locale's wall-clock
// time only (e.g. "1:45:30 PM"), for "last updated" labels where the date is
// implied and only the time-of-day matters. Falls back to "never" for an
// empty/invalid value.
export function formatTimeOfDay(iso: string | undefined): string {
	return fmt(iso, { timeStyle: "medium" }, i18next.t("common.never"));
}

// formatHourTick renders an ISO bucket timestamp as a short wall-clock label for
// a chart's X-axis hour ticks, in the active locale (e.g. "14:00"). Returns the
// raw string on an unparseable value so a tick is never blank.
export function formatHourTick(iso: string): string {
	return fmt(iso, { hour: "2-digit", minute: "2-digit" }, iso);
}

// formatAbsolute renders an ISO timestamp in the active locale's date+time
// format, for tables where an exact time matters more than recency.
export function formatAbsolute(iso: string | undefined): string {
	return fmt(
		iso,
		{ dateStyle: "medium", timeStyle: "medium" },
		i18next.t("common.never"),
	);
}
