/** Shared column widths for the models table.
 *  Used by both ModelTable.tsx (paginated) and VirtualModelTable.tsx (scroll).
 *  Edit here once — both modes stay in sync. */

/** Columns when provider column is visible (Models page).
 *  The page takes the full content width (a Layout table route), and Model
 *  name has no width: it takes whatever the other columns leave, so a wide
 *  window goes to the long names that need it. Below the 2xl breakpoint the
 *  table is about as wide as the old 80rem column, and Capabilities needs its
 *  24% there for the collapsed filter strip; from 2xl up, Capabilities,
 *  Provider and Status switch to fixed widths (22rem fits four row pills) so
 *  a full-width table does not balloon them. A <col> cannot use min(): a
 *  percentage inside it is treated as auto. Outputs, Discovered, Ctx and Max Out are fixed: their
 *  content (the collapsed Outputs strip, a header plus the sort arrow) does
 *  not grow with the window. Fixed widths are in rem because the root font
 *  size scales with the viewport (index.css), and the text they hold scales
 *  with it. Other headers ellipsize (+ title tooltip) when the window is too
 *  narrow for them. */
export const MODEL_COL_WIDTHS_WITH_PROVIDER = [
	"", // Model name - auto: takes the remaining width
	"w-[24%] 2xl:w-[22rem]", // Capabilities - fits the collapsed strip and four row pills
	"w-[8.5rem]", // Outputs - fixed: fits the collapsed strip
	"w-[9%] 2xl:w-[14rem]", // Provider (truncates)
	"w-[8rem]", // Discovered - fixed: fits "DISCOVERED" plus the sort arrow
	"w-[1%]", // (spacer)
	"w-[4.5rem]", // Ctx - fixed: fits "CTX" plus the sort arrow and 7 digits
	"w-[1%]", // (spacer)
	"w-[6.75rem]", // Max Out - fixed: fits "MAX OUT" plus the sort arrow
	"w-[1%]", // (spacer)
	"w-[8%] 2xl:w-[9rem]", // Status
] as const;

/** Columns when provider column is hidden (ProviderModelsModal). */
export const MODEL_COL_WIDTHS_NO_PROVIDER = [
	"", // Model name - auto, as above
	"w-[25%] 2xl:w-[22rem]", // Capabilities, as above
	"w-[8.5rem]", // Outputs - fixed, as above
	"w-[8rem]", // Discovered - fixed, as above
	"w-[2%]", // (spacer)
	"w-[4.5rem]", // Ctx - fixed, as above
	"w-[2%]", // (spacer)
	"w-[6.75rem]", // Max Out - fixed, as above
	"w-[2%]", // (spacer)
	"w-[10%] 2xl:w-[10rem]", // Status (wider to fit "Manually Disabled")
] as const;
