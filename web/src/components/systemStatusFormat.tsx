import type { ReactNode } from "react";

/** Dims the unit that trails a figure, so the number reads first. */
export const unitClass = "text-(--text-muted)";

// One figure with its unit dimmed. The unit carries its own leading space when
// the reading calls for one ("12 MB" against "12d").
function figure(value: ReactNode, unit: string) {
	return (
		<>
			{value}
			<span className={unitClass}>{unit}</span>
		</>
	);
}

/** Uptime as the two largest units that apply: "3d 4h", "4h 5m", or "5m". */
export function formatDuration(seconds: number) {
	const d = Math.floor(seconds / 86400);
	const h = Math.floor((seconds % 86400) / 3600);
	const m = Math.floor((seconds % 3600) / 60);
	if (d > 0)
		return (
			<>
				{figure(d, "d")} {figure(h, "h")}
			</>
		);
	if (h > 0)
		return (
			<>
				{figure(h, "h")} {figure(m, "m")}
			</>
		);
	return figure(m, "m");
}

/** Counts abbreviated to one decimal past a thousand: "1.2K", "3.4M". */
export function formatNumber(n: number) {
	if (n >= 1_000_000) return figure((n / 1_000_000).toFixed(1), "M");
	if (n >= 1_000) return figure((n / 1_000).toFixed(1), "K");
	return n.toLocaleString();
}

/** Memory in the unit that keeps it readable: "0.5 MB", "512 MB", "1.5 GB". */
export function formatMB(mb: number) {
	if (mb < 1) return figure(mb.toFixed(1), " MB");
	if (mb >= 1024) return figure((mb / 1024).toFixed(1), " GB");
	return figure(Math.round(mb), " MB");
}

/** Throughput in the unit that keeps it readable, down to a flat "0 B/s". */
export function formatBytesPerSec(bytesPerSec: number) {
	if (bytesPerSec <= 0) return figure(0, " B/s");
	if (bytesPerSec >= 1024 * 1024)
		return figure((bytesPerSec / 1024 / 1024).toFixed(1), " MB/s");
	if (bytesPerSec >= 1024)
		return figure((bytesPerSec / 1024).toFixed(1), " KB/s");
	return figure(Math.round(bytesPerSec), " B/s");
}
