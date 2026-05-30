export function formatDateTime(iso: string): string {
	try {
		return new Date(iso).toLocaleString(undefined, {
			year: "numeric",
			month: "2-digit",
			day: "2-digit",
			hour: "2-digit",
			minute: "2-digit",
			second: "2-digit",
			hour12: false,
		});
	} catch {
		return iso;
	}
}

export function splitDuration(ms: number): { value: string; unit: string } {
	if (ms >= 1000) {
		return { value: (ms / 1000).toFixed(2), unit: "s" };
	}
	return { value: String(Math.round(ms)), unit: "ms" };
}
