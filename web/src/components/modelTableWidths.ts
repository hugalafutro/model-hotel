/** Shared column widths for the models table.
 *  Used by both ModelTable.tsx (paginated) and VirtualModelTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */

/** Columns when provider column is visible (Models page).
 *  Capabilities and Outputs are sized so their collapsed filter strips (toggle
 *  plus three pills) stay on one line; that width comes from Model name and
 *  Provider, which truncate. Discovered, Ctx and Max Out get FIXED pixel
 *  widths sized to their header plus the sort arrow: their data is always
 *  short, and a percentage either clipped the arrow on narrow windows or
 *  ballooned on wide ones. Other headers ellipsize (+ title tooltip) when the
 *  window is too narrow for them. */
export const MODEL_COL_WIDTHS_WITH_PROVIDER = [
	"w-[19%]", // Model name (truncates long names)
	"w-[24%]", // Capabilities - the collapsed strip must fit one line
	"w-[12%]", // Outputs - icons only; the collapsed strip must fit one line
	"w-[9%]", // Provider (truncates)
	"w-[120px]", // Discovered - fixed: fits "DISCOVERED" plus the sort arrow
	"w-[1%]", // (spacer)
	"w-[68px]", // Ctx - fixed: fits "CTX" plus the sort arrow and 7 digits
	"w-[1%]", // (spacer)
	"w-[100px]", // Max Out - fixed: fits "MAX OUT" plus the sort arrow
	"w-[1%]", // (spacer)
	"w-[8%]", // Status
] as const;

/** Columns when provider column is hidden (ProviderModelsModal). */
export const MODEL_COL_WIDTHS_NO_PROVIDER = [
	"w-[28%]", // Model name (wider without provider col)
	"w-[25%]", // Capabilities
	"w-[10%]", // Outputs - icons only
	"w-[120px]", // Discovered - fixed, as above
	"w-[2%]", // (spacer)
	"w-[68px]", // Ctx - fixed, as above
	"w-[2%]", // (spacer)
	"w-[100px]", // Max Out - fixed, as above
	"w-[2%]", // (spacer)
	"w-[10%]", // Status (wider to fit "Manually Disabled")
] as const;
