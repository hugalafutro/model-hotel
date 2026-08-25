import i18next from "i18next";

// The locale-independent formatters live once in web-shared/ and are re-exported
// here, so every existing "utils/format" import keeps working and the two
// dashboards cannot drift on how a magnitude reads. Everything defined below
// phrases itself through i18next and is the dashboard's own.
export {
	formatCompact,
	formatDollars,
	formatKwh,
	formatTokens,
} from "@web-shared/format";
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
 * Count-prefixed label for a page header: "Models", "1 Model", "5 Models".
 *
 * `key` is an i18next plural base ("models.page_title"), not a pair of
 * resolved forms. i18next picks the category from Intl.PluralRules for the
 * *active* language, so Russian gets its own form at 2 and again at 5 and
 * Arabic gets its dual at 2. A caller that hands over a singular and a plural
 * can only ever reach two of the six categories, which is why this takes the
 * key instead.
 *
 * Zero is deliberately not a plural lookup. The header names the collection
 * and drops the numeral ("Models", not "0 Models"), and "_other" is the form
 * that reads as a bare collection noun everywhere we ship. Asking the plural
 * rules would title an empty page "Model" in French, which selects "one" at
 * zero.
 */
export function countLabel(count: number | undefined, key: string): string {
	const n = count ?? 0;
	if (n === 0) return i18next.t(`${key}_other`);
	return `${n} ${i18next.t(key, { count: n })}`;
}

export function formatDate(ts: number | string): string {
	return new Date(ts).toLocaleDateString(undefined, {
		day: "numeric",
		month: "short",
		year: "numeric",
	});
}

/** Clock time alone, in the browser's locale and its 12/24-hour convention. */
export function formatTime(ts: number | string): string {
	return new Date(ts).toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
	});
}

export function formatDateTimeShort(ts: number | string): string {
	return new Date(ts).toLocaleDateString(undefined, {
		day: "numeric",
		month: "short",
		year: "numeric",
		hour: "2-digit",
		minute: "2-digit",
	});
}

export function formatWithCommas(n: number): string {
	return Math.round(n).toLocaleString();
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

	// Under an hour is minutes, which Intl phrases for every locale we ship, so
	// no catalog key is involved. Anything above zero rounds up to one minute
	// rather than reading as a whole "0 hours" away.
	if (diff < 1000 * 60 * 60) {
		const minutes = Math.max(1, Math.floor(diff / 60000));
		return new Intl.RelativeTimeFormat(i18next.language, {
			numeric: "always",
		}).format(minutes, "minute");
	}

	const hours = Math.floor(diff / (1000 * 60 * 60));
	const days = Math.floor(hours / 24);
	const remainingHours = hours % 24;

	// Glue each value to its unit word with a non-breaking space so a line wrap
	// can't strand the number at the end of one line and "days"/"hours" at the
	// start of the next. Breaks are still allowed at the comma between units.
	// Binds wherever a digit is directly followed by a space + word, i.e. the
	// "<number> <unit>" order, which holds for LTR and the RTL locales we ship
	// (ar/he render the numeral before the unit too). It is simply a no-op where
	// that sequence doesn't occur: CJK has no space, and a hypothetical
	// unit-before-number ordering would keep the prior wrapping behaviour rather
	// than regress.
	const t = (key: string, opts: Record<string, number>): string =>
		i18next.t(key, opts).replace(/(\d)\s+(\S)/g, "$1\u00a0$2");

	if (days > 0) {
		if (days === 1 && remainingHours === 1) {
			return t("format.inDaysHours_one_day_one_hour", {
				days,
				hours: remainingHours,
			});
		}
		if (days === 1) {
			return t("format.inDaysHours_one_day_other_hours", {
				days,
				hours: remainingHours,
			});
		}
		if (remainingHours === 1) {
			return t("format.inDaysHours_other_days_one_hour", {
				days,
				hours: remainingHours,
			});
		}
		return t("format.inDaysHours_other_days_other_hours", {
			days,
			hours: remainingHours,
		});
	}
	// Pick the suffix through i18next rather than by hand: "not 1" is only two
	// forms in English, and Russian needs a third at 2 and a fourth at 5. The
	// hand-written `hours === 1` split could only ever reach _one and _other.
	return t("format.inHours_only", { count: hours, hours });
}

/**
 * Format a latency value in milliseconds to a human-readable string.
 * Values >= 1000ms are shown as seconds (e.g., "8.4s", "15s").
 * Values < 1000ms are shown as milliseconds (e.g., "980ms").
 */
export function formatLatency(ms: number): string {
	if (ms >= 1000) {
		const sec = ms / 1000;
		return sec >= 10 ? `${Math.round(sec)}s` : `${sec.toFixed(1)}s`;
	}
	return `${Math.round(ms)}ms`;
}
