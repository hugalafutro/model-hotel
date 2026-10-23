import i18next from "i18next";
import { formatLocale } from "./format";

// formatRelative renders an ISO timestamp as a localized relative time
// ("5 minutes ago"), falling back to "never" for an empty/zero value. Uses the
// format locale (the app language) so it tracks the rest of the UI.
export function formatRelative(iso: string | undefined): string {
	if (!iso) return i18next.t("common.never");
	const then = new Date(iso).getTime();
	if (Number.isNaN(then) || then <= 0) return i18next.t("common.never");
	const diffMs = then - Date.now();
	const rtf = new Intl.RelativeTimeFormat(formatLocale(), {
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
// caller's Intl options in the format locale.
function fmt(
	iso: string | undefined,
	opts: Intl.DateTimeFormatOptions,
	invalid: string,
): string {
	if (!iso) return invalid;
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return invalid;
	return new Intl.DateTimeFormat(formatLocale(), opts).format(d);
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

// formatEventStamp is formatAbsolute down to the hundredth of a second, for the
// event log, where several events often land in the same second. The fraction
// always shows two digits (".10", never ".1") and the day and hour are 2-digit,
// so rows keep one width; the decimal mark is the locale's ("45.15", "45,15").
// Intl truncates the fraction, so .159 reads .15 and never rolls into the next
// second. dateStyle/timeStyle cannot be combined with a fraction, hence the
// explicit fields.
export function formatEventStamp(iso: string | undefined): string {
	return fmt(
		iso,
		{
			year: "numeric",
			month: "short",
			day: "2-digit",
			hour: "2-digit",
			minute: "2-digit",
			second: "2-digit",
			fractionalSecondDigits: 2,
		},
		i18next.t("common.never"),
	);
}
