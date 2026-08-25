// Number formatting for quota badges and modals. The locale-independent
// formatters live once in web-shared/ and are re-exported here, so every
// existing "utils/format" import keeps working and both frontends render the
// same numbers. Date and relative-time formatting lives in ./time.ts; do not
// duplicate it here.
export {
	formatCompact,
	formatDollars,
	formatKwh,
	formatTokens,
} from "@web-shared/format";

/**
 * A whole-item count with digit grouping, or "-" when the value is absent.
 * Unlike formatTokens this does NOT abbreviate: a daily image allowance is a
 * small number the operator reads exactly, and "1.2K/1.5K" would hide the
 * difference between 1,200 and 1,249. Pinned to en-US like the shared
 * formatters so every locale renders the same figure.
 */
export function formatCount(n: number | null | undefined): string {
	if (n == null) return "-";
	return Math.round(n).toLocaleString("en-US");
}
