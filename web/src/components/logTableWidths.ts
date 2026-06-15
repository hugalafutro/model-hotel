/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync.
 *  Sized against mono digits at the 12px table size (numeric cells are
 *  font-mono in every theme, so the budget is theme-independent):
 *  ~7.2px per character plus the 16px px-2 cell padding.
 *
 *  A couple of columns (status, duration) are budgeted for their HEADER word
 *  rather than their short data so the English labels don't truncate; the
 *  borrowed width comes from model/key, which already truncate their long
 *  values anyway. Total stays ~constant so the table width is unchanged.
 *  Headers ellipsize (+ title tooltip) as the safety net for longer locales. */
export const LOG_COL_WIDTHS = [
	{ key: "date", width: "w-38" }, // Time/Date - "12/06/2026, 10:45:12" (20ch)
	{ key: "model", width: "w-48" }, // Model (truncates long names)
	{ key: "provider", width: "w-25" }, // Provider
	{ key: "status", width: "w-17" }, // Status - fits the "STATUS" header
	{ key: "tokens", width: "w-27" }, // Tokens - "99,999+9,999" (12ch)
	{ key: "tps", width: "w-18.25" }, // T/s
	{ key: "headers", width: "w-20.75" }, // Headers (response_header_ms)
	{ key: "ttft", width: "w-20.75" }, // TTFT
	{ key: "duration", width: "w-20" }, // Duration - fits the "DURATION" header
	{ key: "overhead", width: "w-19.5" }, // Overhead
	{ key: "key", width: "w-24" }, // Key (truncates; cell is max-w-[7rem])
] as const;
