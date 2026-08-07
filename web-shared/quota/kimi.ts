import type { KimiCodeQuotaResponse, KimiCodeQuotaWindow } from "./types";

/**
 * A Kimi string-encoded number, or undefined when the field carries no value.
 *
 * Absent, "" and whitespace are one case: proto3 omits a zero-valued field and
 * the Go snapshot re-marshal writes "" for the omission. Both come back as
 * undefined here, and what the omission means is the caller's to decide, field
 * by field.
 */
function parseKimiNumber(v: string | undefined): number | undefined {
	if (v == null) return undefined;
	const trimmed = v.trim();
	if (trimmed === "") return undefined;
	const n = Number(trimmed);
	return Number.isFinite(n) ? n : undefined;
}

/** True when the field was omitted, which for Kimi means the value is zero. */
function isOmitted(v: string | undefined): boolean {
	return v == null || v.trim() === "";
}

/**
 * How many units of the window are still available.
 *
 * `remaining` wins when it carries a value. Otherwise the window is exhausted
 * enough for Kimi to have dropped `remaining`, and `used` carries the count, so
 * remaining is limit minus used. An omitted `used` means zero used, hence the
 * full limit. A `used` that is present but unreadable yields undefined: an
 * unparseable window must not be turned into a number, ever.
 */
function resolveRemaining(
	limit: number,
	remainingStr: string | undefined,
	usedStr: string | undefined,
): number | undefined {
	const remaining = parseKimiNumber(remainingStr);
	if (remaining !== undefined) return remaining;

	const used = parseKimiNumber(usedStr);
	if (used !== undefined) return Math.max(0, limit - used);

	return isOmitted(usedStr) ? limit : undefined;
}

/**
 * Normalizes one Kimi window. Returns undefined when the limit is unreadable or
 * the remaining count cannot be established, so callers render a "-" rather
 * than a guess.
 */
export function toKimiCodeWindow(
	limitStr: string | undefined,
	remainingStr: string | undefined,
	usedStr: string | undefined,
	resetTime: string | undefined,
): KimiCodeQuotaWindow | undefined {
	const limit = parseKimiNumber(limitStr);
	if (limit === undefined) return undefined;

	const remaining = resolveRemaining(limit, remainingStr, usedStr);
	if (remaining === undefined) return undefined;

	const raw = limit > 0 ? ((limit - remaining) / limit) * 100 : 0;
	const percentage = Math.min(100, Math.max(0, raw));
	return { limit, remaining, resetTime: resetTime ?? "", percentage };
}

/** The rolling 300-minute (5-hour) window. */
export function getKimiCodeFiveHourLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const entry = data?.limits?.find(
		(l) =>
			l.window?.timeUnit === "TIME_UNIT_MINUTE" && l.window?.duration === 300,
	);
	if (!entry) return undefined;
	return toKimiCodeWindow(
		entry.detail?.limit,
		entry.detail?.remaining,
		entry.detail?.used,
		entry.detail?.resetTime,
	);
}

/** The weekly window, carried in the top-level `usage` block. */
export function getKimiCodeWeeklyLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const usage = data?.usage;
	if (!usage) return undefined;
	return toKimiCodeWindow(
		usage.limit,
		usage.remaining,
		usage.used,
		usage.resetTime,
	);
}
