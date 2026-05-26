/** Shared column widths for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */
export const LOG_COL_WIDTHS = [
	"w-30", // Time/Date
	"w-30.5", // Hash
	"w-49.5", // Model (trimmed from 55 to give Key more room)
	"w-25", // Provider
	"w-14", // Status
	"w-21", // Tokens
	"w-16.25", // T/s
	"w-18.75", // TTFT
	"w-13.75", // Duration
	"w-17.5", // Overhead
	"w-23", // Key (expanded from 17.5)
] as const;
