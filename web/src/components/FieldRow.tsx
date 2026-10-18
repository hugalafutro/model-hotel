import type { ReactNode } from "react";

// One label-left form row for a form laid out as
// `grid grid-cols-[auto_minmax(0,1fr)]`: the label in the first column, the
// field and its hints in the second. The label's top padding lines its text
// up with a single-line input rather than centring it on field plus hints.
export function FieldRow({
	label,
	htmlFor,
	fieldClassName,
	children,
}: {
	label: ReactNode;
	htmlFor?: string;
	// Extra classes on the field cell, e.g. col-span-3 for a row that runs the
	// width of a form with two label/field pairs per row.
	fieldClassName?: string;
	children: ReactNode;
}) {
	const labelClass = "ui-label pt-2.5";
	return (
		<>
			{htmlFor ? (
				<label htmlFor={htmlFor} className={labelClass}>
					{label}
				</label>
			) : (
				<span className={labelClass}>{label}</span>
			)}
			<div className={`min-w-0 ${fieldClassName ?? ""}`}>{children}</div>
		</>
	);
}
