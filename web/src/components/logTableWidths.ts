/** Shared column definitions for the requests/logs table.
 *  Used by both Logs.tsx (paginated) and VirtualLogTable.tsx (scroll).
 *  Edit here once — both modes stay in sync.
 *
 *  Every column except Model has an absolute rem width fitted to its
 *  content, and Model (width: auto) absorbs whatever the table has left.
 *  In fixed table layout the auto column takes the leftover, so the fitted
 *  columns keep their tight gaps at any table width: a wider viewport shows
 *  more of the model name instead of padding every column with dead space.
 *  Percentages did the opposite (every column grew with the table).
 *
 *  Fitting rules (root-font independent because everything is rem):
 *  - cells: 0.5rem padding each side (.ui-table td), mono text at 0.75rem
 *    with ~0.6em advance, so ~0.45rem per character;
 *  - headers: same 0.5rem padding, 0.65625rem uppercase text with tracking
 *    (~0.5rem per character), plus the SortableHeader arrow slot (gap-1 +
 *    w-3 = 1rem) that the paginated mode reserves even when unsorted;
 *  - each column = max(header, widest expected cell) + a hair of slack, so a
 *    theme with a slightly wider mono still gets a visible gap;
 *  - header-driven columns are fitted to the ENGLISH label on purpose. Longer
 *    translations (ru "Продолжительность", fr "Fournisseur") ellipsize and
 *    the th title tooltip carries the full word, as before.
 *
 *  LOG_TABLE_MIN_W keeps Model from collapsing to zero: below the fitted
 *  sum plus a ~6.5rem Model floor the table scrolls horizontally instead
 *  of squeezing (fitted columns cannot give, they would overflow). */
export const LOG_COL_WIDTHS = [
	{ key: "date", width: "w-[10.25rem]" }, // "23/08/2026, 22:59:28" (20ch mono)
	{ key: "model", width: "w-auto" }, // Model takes the leftover; truncates with tooltip
	{ key: "provider", width: "w-[6.5rem]" }, // "PROVIDER" header; "Ollama Cloud" fits, longer names truncate
	{ key: "status", width: "w-[5.25rem]" }, // "STATUS" header + arrow slot (wider than the [Live] badge)
	{ key: "tokens", width: "w-[7.25rem]" }, // "154,304+3,796" (13ch mono)
	{ key: "tps", width: "w-[4rem]" }, // "1234.5" (6ch mono)
	{ key: "headers", width: "w-[5.75rem]" }, // "99999.9ms" (9ch mono) vs "HEADERS" header + arrow slot
	{ key: "ttft", width: "w-[5.75rem]" }, // same as Headers; same value shape
	{ key: "duration", width: "w-[6.125rem]" }, // "DURATION" header + arrow slot; long durations may overflow
	{ key: "overhead", width: "w-[6.125rem]" }, // "OVERHEAD" header + arrow slot
	{ key: "key", width: "w-[6rem]" }, // Key (truncates with tooltip)
	{ key: "ip", width: "w-[8rem]" }, // full IPv4 (15ch mono); IPv6 truncates with tooltip
] as const;

/** Fitted columns sum to 71rem; the rest is the Model floor. Deliberately
 *  above the 62.5rem the other tables use: the fitted columns cannot shrink,
 *  so a smaller minimum would only starve Model, and the dashboard's 80rem
 *  content column fits this at both the 1080p and 1440p root font sizes. */
export const LOG_TABLE_MIN_W = "min-w-[77rem]";
