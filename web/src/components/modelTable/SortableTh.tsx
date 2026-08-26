import type { ModelSortField, ModelSortState } from "./modelCursor";

export const MODEL_HEADER_BASE =
	"px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

/** A sortable header cell: the label, the sort arrow for the active field, one click to toggle. */
export function SortableTh({
	field,
	label,
	ariaLabel,
	sort,
	onSort,
}: {
	field: ModelSortField;
	label: string;
	ariaLabel: string;
	sort: ModelSortState;
	onSort: (field: ModelSortField) => void;
}) {
	return (
		<th
			className={`${MODEL_HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
			onClick={() => onSort(field)}
			title={label}
		>
			<button type="button" className="" aria-label={ariaLabel}>
				{label}{" "}
				<span className="inline-block w-3 text-center">
					{sort.field === field ? (sort.dir === "asc" ? "↑" : "↓") : " "}
				</span>
			</button>
		</th>
	);
}
