/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync.
 *  Sized against mono digits at the 12px table size (numeric cells are
 *  font-mono in every theme, so the budget is theme-independent):
 *  ~7.2px per character plus the 16px px-2 cell padding. */
export const LOG_COL_WIDTHS = [
	{ key: "date", width: "w-38" }, // Time/Date - "12/06/2026, 10:45:12" (20ch)
	{ key: "model", width: "w-54" }, // Model
	{ key: "provider", width: "w-25" }, // Provider
	{ key: "status", width: "w-12" }, // Status
	{ key: "tokens", width: "w-27" }, // Tokens - "99,999+9,999" (12ch)
	{ key: "tps", width: "w-18.25" }, // T/s
	{ key: "headers", width: "w-20.75" }, // Headers (response_header_ms)
	{ key: "ttft", width: "w-20.75" }, // TTFT
	{ key: "duration", width: "w-14.75" }, // Duration
	{ key: "overhead", width: "w-19.5" }, // Overhead
	{ key: "key", width: "w-28" }, // Key - matches the cell's max-w-[7rem]
] as const;
