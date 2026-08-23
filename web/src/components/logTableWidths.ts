/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync.
 *
 *  Budgeted for the 1440p layout the dashboard targets, where the centered
 *  content column gives the table ~1340px: the widths below sum to just under
 *  that (one Tailwind unit renders at 4.25px under the app's 17px root font
 *  size), so at that size every column renders at its declared width with no
 *  horizontal scroll. In fixed table layout the column sum acts as a MINIMUM
 *  table width, so a sum past the card budget makes the card scroll sideways.
 *  Two costs drive the numbers:
 *  - paginated headers (SortableHeader) spend px-4 padding plus a reserved
 *    sort-arrow slot: label text + ~48px;
 *  - mono cells at the 12px table size run ~7.2px per character plus the
 *    16px px-2 cell padding.
 *  Each column takes max(header, widest expected cell). Model and key
 *  truncate long values by design (title tooltip carries the rest), and the
 *  IP column fits a full IPv4 ("255.255.255.255", 15ch) while IPv6 truncates
 *  with its tooltip. Below the 1440p budget the table scales down evenly and
 *  headers ellipsize (+ title tooltip) as the safety net, min-w-250 last. */
export const LOG_COL_WIDTHS = [
	{ key: "date", width: "w-38" }, // Time/Date - "23/08/2026, 22:59:28" (20ch mono)
	{ key: "model", width: "w-35.5" }, // Model (truncates long names; takes the leftover)
	{ key: "provider", width: "w-26.5" }, // Provider - "PROVIDER" header
	{ key: "status", width: "w-22.5" }, // Status - "STATUS" header
	{ key: "tokens", width: "w-24.25" }, // Tokens - "99,999+9,999" (12ch)
	{ key: "tps", width: "w-16" }, // T/s
	{ key: "headers", width: "w-25" }, // Headers - "HEADERS" header
	{ key: "ttft", width: "w-18.75" }, // TTFT
	{ key: "duration", width: "w-26.75" }, // Duration - "DURATION" header
	{ key: "overhead", width: "w-27.75" }, // Overhead - "OVERHEAD" header
	{ key: "key", width: "w-23.75" }, // Key (truncates; cell is max-w-[7rem])
	{ key: "ip", width: "w-31" }, // IP - full IPv4 (15ch); IPv6 truncates
] as const;
