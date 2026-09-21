import i18next from "i18next";
/* =========================================================
   Date helpers for the accent-themed calendar picker
   ===================================================== */
export function pad(n: number): string {
	return n.toString().padStart(2, "0");
}

export function toISODate(d: Date): string {
	// Use local date components so "today" matches the user's timezone
	// rather than UTC (which would differ near midnight).
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function todayISO(): string {
	return toISODate(new Date());
}

export function daysInMonth(year: number, month: number): number {
	return new Date(year, month + 1, 0).getDate();
}

export function firstDayOfMonth(year: number, month: number): number {
	return new Date(year, month, 1).getDay();
}

/* =========================================================
   Date range formatting
   ===================================================== */
export function formatDateRangeShort(
	from: string,
	to: string,
	locale: string = i18next.language,
): string {
	// Use toISODate to convert any input (plain date or ISO timestamp)
	// to local date components, then parse components directly to avoid
	// any further Date constructor ambiguity.  Bare "YYYY-MM-DD" strings
	// parse as UTC per ECMAScript spec, so we append T00:00:00 to force
	// local-time parsing (same approach as useDateRangePicker).
	const fromDate = new Date(from.includes("T") ? from : `${from}T00:00:00`);
	const toDate = new Date(to.includes("T") ? to : `${to}T00:00:00`);
	const fromLocal = toISODate(fromDate);
	const toLocal = toISODate(toDate);
	const [fy, fm, fd] = fromLocal.split("-").map(Number);
	const [ty, tm, td] = toLocal.split("-").map(Number);
	const sameMonth = fm === tm && fy === ty;
	// Day and month in the reader's own order (05/03 is March 5th to a British
	// reader and May 3rd to an American one), from the locale, not a fixed
	// dd/mm. Built from local components so the calendar day is the one shown.
	const fromDay = new Date(fy, fm - 1, fd);
	const toDay = new Date(ty, tm - 1, td);
	const dayMonth = new Intl.DateTimeFormat(locale, {
		day: "2-digit",
		month: "2-digit",
	});
	const dayMonthYear = new Intl.DateTimeFormat(locale, {
		day: "2-digit",
		month: "2-digit",
		year: "numeric",
	});
	const dayMonthShortYear = new Intl.DateTimeFormat(locale, {
		day: "2-digit",
		month: "2-digit",
		year: "2-digit",
	});
	return sameMonth
		? `${dayMonth.format(fromDay)}-${dayMonthYear.format(toDay)}`
		: `${dayMonthShortYear.format(fromDay)} - ${dayMonthYear.format(toDay)}`;
}
