/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */
export const LOG_COL_WIDTHS = [
	{ key: "date", width: "w-30" }, // Time/Date
	{ key: "hash", width: "w-30" }, // Hash
	{ key: "model", width: "w-52" }, // Model
	{ key: "provider", width: "w-25" }, // Provider
	{ key: "status", width: "w-12" }, // Status
	{ key: "tokens", width: "w-21" }, // Tokens
	{ key: "tps", width: "w-16.25" }, // T/s
	{ key: "headers", width: "w-18.75" }, // Headers (response_header_ms)
	{ key: "ttft", width: "w-18.75" }, // TTFT
	{ key: "duration", width: "w-13.75" }, // Duration
	{ key: "overhead", width: "w-17.5" }, // Overhead
	{ key: "key", width: "w-23" }, // Key (expanded from 17.5)
] as const;
