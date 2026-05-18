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
	const f = new Date(from);
	const t = new Date(to);
	const sameMonth =
		f.getMonth() === t.getMonth() && f.getFullYear() === t.getFullYear();
	const fd = `${pad(f.getDate())}/${pad(f.getMonth() + 1)}`;
	const td = `${pad(t.getDate())}/${pad(t.getMonth() + 1)}/${t.getFullYear()}`;
	return sameMonth
		? `${fd}-${td}`
		: `${fd}/${f.getFullYear().toString().slice(2)} - ${td}`;
}
