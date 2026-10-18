import type { ReactNode } from "react";

// A label-left form: one column of stacked label-over-field rows on a phone,
// label and field side by side from sm up. Rows that run the full width
// (fieldsets, toggles, pickers) carry sm:col-span-2.
export const FORM_GRID =
	"grid grid-cols-1 sm:grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-4";
// The same with two label/field pairs per row from sm up; full-width rows
// carry sm:col-span-4, a field that runs to the end of its row sm:col-span-3.
export const FORM_GRID_PAIRS =
	"grid grid-cols-1 sm:grid-cols-[auto_minmax(0,1fr)_auto_minmax(0,1fr)] gap-x-4 gap-y-4";

// One row of a FORM_GRID form: the label in the first column, the field and
// its hints in the second. On a phone the wrapper is an ordinary block (label
// above field); from sm up it is display: contents, so label and field are
// the grid's own cells. The label's top padding lines its text up with a
// single-line input rather than centring it on field plus hints.
export function FieldRow({
	label,
	htmlFor,
	fieldClassName,
	children,
}: {
	label: ReactNode;
	htmlFor?: string;
	// Extra classes on the field cell, e.g. sm:col-span-3 for a row that runs
	// the width of a FORM_GRID_PAIRS form.
	fieldClassName?: string;
	children: ReactNode;
}) {
	const labelClass = "block ui-label mb-1 sm:mb-0 sm:pt-2.5";
	return (
		<div className="sm:contents">
			{htmlFor ? (
				<label htmlFor={htmlFor} className={labelClass}>
					{label}
				</label>
			) : (
				<span className={labelClass}>{label}</span>
			)}
			<div className={`min-w-0 ${fieldClassName ?? ""}`}>{children}</div>
		</div>
	);
}
