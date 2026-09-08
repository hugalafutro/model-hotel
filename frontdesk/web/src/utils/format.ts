// Number formatting for quota badges and modals. The locale-independent
// formatters live once in web-shared/ and are re-exported here, so every
// existing "utils/format" import keeps working and both frontends render the
// same numbers. Date and relative-time formatting lives in ./time.ts; do not
// duplicate it here.
export {
	formatCompact,
	formatCount,
	formatDollars,
	formatKwh,
	formatTokens,
} from "@web-shared/format";
