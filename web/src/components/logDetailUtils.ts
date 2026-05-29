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

export function formatDuration(ms: number): string {
	if (ms >= 1000) {
		return `${(ms / 1000).toFixed(2)}s`;
	}
	return `${Math.round(ms)}ms`;
}
