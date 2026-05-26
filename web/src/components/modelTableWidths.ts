/** Shared column widths for the models table.
 *  Used by both ModelTable.tsx (paginated) and VirtualModelTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */

/** Columns when provider column is visible (Models page). */
export const MODEL_COL_WIDTHS_WITH_PROVIDER = [
	"w-[30%]", // Model name
	"w-[24%]", // Capabilities
	"w-[16%]", // Provider
	"w-[6%]", // Discovered
	"w-[2%]", // (spacer)
	"w-[4%]", // Ctx
	"w-[2%]", // (spacer)
	"w-[4%]", // Max Out
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
