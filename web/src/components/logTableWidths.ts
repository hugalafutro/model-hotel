/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync.
 *
 *  Budgeted for the 1440p layout the dashboard targets, where the centered
 *  content column gives the table ~1340px. Each column is a PERCENTAGE of the
 *  table and the twelve sum to exactly 100%, so the table is always precisely
 *  the card's width: in fixed table layout an absolute column sum acts as a
 *  MINIMUM table width, and an absolute budget solved at one root font size
 *  overshot the card by a pixel at another (the 1080p root font is 16px, the
 *  1440p one 17px), leaving a horizontal scrollbar that scrolled nowhere.
 *  The percentages are the 1440p pixel budget below, normalised. Two costs
 *  drive that budget:
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
	{ key: "date", width: "w-[12.03%]" }, // Time/Date - "23/08/2026, 22:59:28" (20ch mono, ~162px)
	{ key: "model", width: "w-[11.24%]" }, // Model (truncates long names; takes the leftover, ~151px)
	{ key: "provider", width: "w-[8.39%]" }, // Provider - "PROVIDER" header (~113px)
	{ key: "status", width: "w-[7.13%]" }, // Status - "STATUS" header (~96px)
	{ key: "tokens", width: "w-[7.68%]" }, // Tokens - "99,999+9,999" (12ch, ~103px)
	{ key: "tps", width: "w-[5.07%]" }, // T/s (~68px)
	{ key: "headers", width: "w-[7.92%]" }, // Headers - "HEADERS" header (~106px)
	{ key: "ttft", width: "w-[5.94%]" }, // TTFT (~80px)
	{ key: "duration", width: "w-[8.47%]" }, // Duration - "DURATION" header (~114px)
	{ key: "overhead", width: "w-[8.79%]" }, // Overhead - "OVERHEAD" header (~118px)
	{ key: "key", width: "w-[7.52%]" }, // Key (truncates; cell is max-w-[7rem], ~101px)
	{ key: "ip", width: "w-[9.82%]" }, // IP - full IPv4 (15ch, ~132px); IPv6 truncates
] as const;
