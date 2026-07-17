/** Shared column widths for the models table.
 *  Used by both ModelTable.tsx (paginated) and VirtualModelTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */

/** Columns when provider column is visible (Models page).
 *  Discovered gets a FIXED pixel width sized to its "DISCOVERED" header: its
 *  data is always short ("21m ago"), so a percentage width made it balloon
 *  relative to content on narrow (half-screen) windows. The other narrow
 *  numeric columns keep percentages sized so their header words fit unclipped
 *  in English at the ~1440p width this is developed against; the width is
 *  borrowed from the model-name column, which truncates its long values
 *  anyway. Headers ellipsize (+ title tooltip) below that, which is
 *  acceptable for narrow screens. */
export const MODEL_COL_WIDTHS_WITH_PROVIDER = [
	"w-[27%]", // Model name (truncates long names)
	"w-[24%]", // Capabilities
	"w-[16%]", // Provider
	"w-[104px]", // Discovered - fixed: fits the "DISCOVERED" header, data is short
	"w-[2%]", // (spacer)
	"w-[5%]", // Ctx
	"w-[2%]", // (spacer)
	"w-[7%]", // Max Out - fits the "MAX OUT" header
	"w-[2%]", // (spacer)
	"w-[8%]", // Status
] as const;

/** Columns when provider column is hidden (ProviderModelsModal). */
export const MODEL_COL_WIDTHS_NO_PROVIDER = [
	"w-[38%]", // Model name (wider without provider col)
	"w-[28%]", // Capabilities
	"w-[104px]", // Discovered - fixed: fits the "DISCOVERED" header, data is short
	"w-[2%]", // (spacer)
	"w-[6%]", // Ctx
	"w-[2%]", // (spacer)
	"w-[6%]", // Max Out
	"w-[2%]", // (spacer)
	"w-[10%]", // Status (wider to fit "Manually Disabled")
] as const;
