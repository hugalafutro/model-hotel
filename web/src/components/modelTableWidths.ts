/** Shared column widths for the models table.
 *  Used by both ModelTable.tsx (paginated) and VirtualModelTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */

/** Columns when provider column is visible (Models page).
 *  The narrow numeric columns (discovered/ctx/maxOut) are widened beyond their
 *  short data so their HEADER words fit unclipped in English at the ~1440p width
 *  this is developed against; the width is borrowed from the model-name column,
 *  which truncates its long values anyway. Headers ellipsize (+ title tooltip)
 *  below that, which is acceptable for narrow screens. */
export const MODEL_COL_WIDTHS_WITH_PROVIDER = [
	"w-[23%]", // Model name (truncates long names)
	"w-[24%]", // Capabilities
	"w-[16%]", // Provider
	"w-[11%]", // Discovered - fits the "DISCOVERED" header
	"w-[2%]", // (spacer)
	"w-[5%]", // Ctx
	"w-[2%]", // (spacer)
	"w-[7%]", // Max Out - fits the "MAX OUT" header
	"w-[2%]", // (spacer)
	"w-[8%]", // Status
] as const;

/** Columns when provider column is hidden (ProviderModelsModal). */
export const MODEL_COL_WIDTHS_NO_PROVIDER = [
	"w-[34%]", // Model name (wider without provider col)
	"w-[28%]", // Capabilities
	"w-[10%]", // Discovered
	"w-[2%]", // (spacer)
	"w-[6%]", // Ctx
	"w-[2%]", // (spacer)
	"w-[6%]", // Max Out
	"w-[2%]", // (spacer)
	"w-[10%]", // Status (wider to fit "Manually Disabled")
] as const;
