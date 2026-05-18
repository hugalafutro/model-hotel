/* =========================================================
   Date helpers for the accent-themed calendar picker
   ===================================================== */
export function toISODate(d: Date): string {
	// Use local date components so "today" matches the user's timezone
	// rather than UTC (which would differ near midnight).
	const y = d.getFullYear();
	const m = String(d.getMonth() + 1).padStart(2, "0");
	const day = String(d.getDate()).padStart(2, "0");
	return `${y}-${m}-${day}`;
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

export function pad(n: number): string {
	return n.toString().padStart(2, "0");
}

/* =========================================================
   Date range formatting
   ===================================================== */
export function formatDateRangeShort(from: string, to: string): string {
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
	const fds = `${pad(fd)}/${pad(fm)}`;
	const tds = `${pad(td)}/${pad(tm)}/${ty}`;
	return sameMonth
		? `${fds}-${tds}`
		: `${fds}/${fy.toString().slice(2)} - ${tds}`;
}
