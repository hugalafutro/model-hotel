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
	// Strip time portion from ISO timestamps so parsing uses local date
	// components (consistent with toISODate/todayISO) rather than UTC,
	// which would show the wrong date near midnight in UTC-X timezones.
	const fromDate = new Date(from.includes("T") ? from.split("T")[0] : from);
	const toDate = new Date(to.includes("T") ? to.split("T")[0] : to);
	const sameMonth =
		fromDate.getMonth() === toDate.getMonth() &&
		fromDate.getFullYear() === toDate.getFullYear();
	const fd = `${pad(fromDate.getDate())}/${pad(fromDate.getMonth() + 1)}`;
	const td = `${pad(toDate.getDate())}/${pad(toDate.getMonth() + 1)}/${toDate.getFullYear()}`;
	return sameMonth
		? `${fd}-${td}`
		: `${fd}/${fromDate.getFullYear().toString().slice(2)} - ${td}`;
}
