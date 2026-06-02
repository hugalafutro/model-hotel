import i18next from "i18next";
/** Encode a value as base64, handling Unicode characters safely. */
export function encodeCursor(obj: unknown): string {
	const json = JSON.stringify(obj);
	return btoa(
		encodeURIComponent(json).replace(/%([0-9A-F]{2})/g, (_, p1) =>
			String.fromCharCode(Number.parseInt(p1, 16)),
		),
	);
}

export function formatDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	return `${(ms / 1000).toFixed(1)}s`;
}

export function formatRelativeTime(dateStr: string | null): string {
	if (!dateStr) return i18next.t("format.never");
	const date = new Date(dateStr);
	const now = new Date();
	const diffMs = now.getTime() - date.getTime();
	const diffMin = Math.floor(diffMs / 60000);
	if (diffMin < 1) return i18next.t("format.justNow");
	if (diffMin < 60) return i18next.t("format.minutesAgo", { count: diffMin });
	const diffHr = Math.floor(diffMin / 60);
	if (diffHr < 24) return i18next.t("format.hoursAgo", { count: diffHr });
	const diffDay = Math.floor(diffHr / 24);
	return i18next.t("format.daysAgo", { count: diffDay });
}

export function formatNumber(n: number | null | undefined): string {
	if (n == null) return "-";
	return n.toLocaleString();
}

export function formatTokens(n: number | null | undefined): string {
	if (n == null) return "-";
	if (n >= 1_000_000_000)
		return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, "")}B`;
	if (n >= 1_000_000)
		return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
	if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, "")}K`;
	return n.toString();
}

export function formatTimestamp(ts: number | string): string {
	return new Date(ts).toLocaleString(undefined, {
		day: "numeric",
		month: "short",
		year: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	});
}

/**
 * Returns a count-prefixed label with proper singular/plural using ICU plural rules.
 * 0 → just the plural noun (e.g. "Models")
 * 1 → "1 {singular}" (e.g. "1 Model")
 * 2+ → "{n} {plural}" (e.g. "5 Models")
 */
export function countLabel(
	count: number | undefined,
	singular: string,
	plural: string,
): string {
	const n = count ?? 0;
	if (n === 0) return plural;
	// Use i18next ICU plural format for proper localization
	return i18next.t("format.countLabel", { count: n, singular, plural });
}

export function formatDate(ts: number | string): string {
	return new Date(ts).toLocaleDateString(undefined, {
		day: "numeric",
		month: "short",
		year: "numeric",
	});
}

export function formatWithCommas(n: number): string {
	return Math.round(n).toLocaleString();
}

export function formatCompact(n: number): string {
	if (n === 0) return "0";
	const abs = Math.abs(n);
	const fmt = (v: number) => {
		const s = v.toFixed(1);
		return s.endsWith(".0") ? s.slice(0, -2) : s;
	};
	if (abs >= 1_000_000) return `${fmt(n / 1_000_000)}M`;
	if (abs >= 1_000) return `${fmt(n / 1_000)}K`;
	return fmt(n);
}

export function dropTrailingZero(v: number, decimals: number): string {
	const s = v.toFixed(decimals);
	if (decimals > 0 && s.includes(".")) {
		return s.replace(/\.?0+$/, "");
	}
	return s;
}

/**
 * Format a distribution share percentage, showing "<0.1%" for small/zero
 * shares that would otherwise display as "0.0%" or "0%".
 * - 76.6 → "76.6%"
 * - 0.05 → "0.1%"  (rounds up)
 * - 0.02 → "<0.1%"
 * - 0 → "<0.1%" (rounding artifact; provider wouldn't appear with zero traffic)
 */
export function formatPercent(value: number): string {
	if (value < 0.05) return "<0.1%";
	return `${value.toFixed(1)}%`;
}

export function formatTimeUntil(ts: number): string {
	const now = Date.now();
	const diff = ts - now;
	if (diff <= 0) return i18next.t("format.now");

	const hours = Math.floor(diff / (1000 * 60 * 60));
	const days = Math.floor(hours / 24);
	const remainingHours = hours % 24;

	if (days > 0) {
		if (days === 1 && remainingHours === 1) {
			return i18next.t("format.inDaysHours_one_day_one_hour", {
				days,
				hours: remainingHours,
			});
		}
		if (days === 1) {
			return i18next.t("format.inDaysHours_one_day_other_hours", {
				days,
				hours: remainingHours,
			});
		}
		if (remainingHours === 1) {
			return i18next.t("format.inDaysHours_other_days_one_hour", {
				days,
				hours: remainingHours,
			});
		}
		return i18next.t("format.inDaysHours_other_days_other_hours", {
			days,
			hours: remainingHours,
		});
	}
	if (hours === 1) {
		return i18next.t("format.inHours_only_one", { hours });
	}
	return i18next.t("format.inHours_only_other", { hours });
}
