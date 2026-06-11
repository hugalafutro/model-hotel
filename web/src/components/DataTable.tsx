import type { MouseEvent, ReactNode } from "react";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";

type SortDir = "asc" | "desc";

export interface SortState<F> {
	field: F;
	dir: SortDir;
}

const HEADER_BASE =
	"px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

export function SortableHeader<F extends string>({
	label,
	field,
	sort,
	onSort,
	tooltip,
	className,
}: {
	label: string;
	field: F;
	sort: SortState<F>;
	onSort: (f: F) => void;
	tooltip?: string;
	className?: string;
}) {
	const { t } = useTranslation();
	const active = sort.field === field;
	return (
		<th
			className={`${HEADER_BASE} select-none hover:text-gray-200 ${className ?? ""}`}
			title={tooltip}
		>
			<button
				type="button"
				onClick={() => onSort(field)}
				aria-label={t("components.dataTable.sortBy", { label })}
			>
				{label}{" "}
				<span className="inline-block w-3 text-center">
					{active ? (sort.dir === "asc" ? "↑" : "↓") : " "}
				</span>
			</button>
		</th>
	);
}

export function StaticHeader({
	children,
	className = "",
	tooltip,
}: {
	children: ReactNode;
	className?: string;
	tooltip?: string;
}) {
	return (
		<th className={`${HEADER_BASE} ${className}`} title={tooltip}>
			{children}
			<span className="inline-block w-3" />
		</th>
	);
}

export function StaticHeaderNoArrow({
	children,
	className = "",
	tooltip,
}: {
	children: ReactNode;
	className?: string;
	tooltip?: string;
}) {
	return (
		<th className={`${HEADER_BASE} ${className}`} title={tooltip}>
			{children}
		</th>
	);
}

export function Row({
	children,
	className = "",
	onClick,
}: {
	children: ReactNode;
	className?: string;
	onClick?: (e: MouseEvent<HTMLTableRowElement>) => void;
}) {
	return (
		<tr
			className={`hover:bg-(--surface-hover) transition-colors ${className} ${onClick ? "cursor-pointer" : ""}`}
			role={onClick ? "button" : undefined}
			tabIndex={onClick ? 0 : undefined}
			onClick={onClick}
			onKeyDown={
				onClick
					? (e) => {
							if (e.key === "Enter" || e.key === " ") {
								e.preventDefault();
								onClick(e as unknown as MouseEvent<HTMLTableRowElement>);
							}
						}
					: undefined
			}
		>
			{children}
		</tr>
	);
}

export function EmptyRow({
	colSpan,
	message,
}: {
	colSpan: number;
	message: string;
}) {
	return (
		<tr>
			<td colSpan={colSpan} className="px-4 py-8 text-center text-gray-500">
				{message}
			</td>
		</tr>
	);
}

export function PaginationBar({
	page,
	totalPages,
	totalItems,
	pageSize,
	onPageChange,
	onPageSizeChange,
	label = "entries",
	hideCount = false,
}: {
	page: number;
	totalPages: number;
	totalItems: number;
	pageSize: number;
	onPageChange: (page: number) => void;
	onPageSizeChange: (size: number) => void;
	label?: string;
	hideCount?: boolean;
}) {
	const { t } = useTranslation();
	const setPage = useCallback(
		(page: number) => onPageChange(Math.max(1, Math.min(totalPages, page))),
		[onPageChange, totalPages],
	);

	const singular = label.replace(/s$/, "");

	const start = (page - 1) * pageSize + 1;
	const end = Math.min(page * pageSize, totalItems);

	return (
		<div className="flex items-center gap-3">
			{!hideCount &&
				(totalItems === 0 ? (
					<div className="text-sm text-gray-500" />
				) : totalItems === 1 ? (
					<div className="text-sm text-gray-500">
						{t("components.dataTable.countOne", { label: singular })}
					</div>
				) : totalItems <= pageSize ? (
					<div className="text-sm text-gray-500">
						{t("components.dataTable.entriesShort", {
							start: String(start),
							end: String(end),
							total: totalItems,
							label,
						})}
					</div>
				) : (
					<div className="text-sm text-gray-500">
						{t("components.dataTable.entries", {
							start: String(start),
							to: t("components.dataTable.to"),
							of: t("components.dataTable.of"),
							end: String(end),
							total: totalItems,
							label,
						})}
					</div>
				))}
			{totalItems > 0 && (
				<select
					value={pageSize}
					onChange={(e) => onPageSizeChange(Number(e.target.value))}
					className="ui-input ui-input-sm"
				>
					<option value={10}>
						{t("components.dataTable.perPage", { size: 10 })}
					</option>
					<option value={20}>
						{t("components.dataTable.perPage", { size: 20 })}
					</option>
					<option value={30}>
						{t("components.dataTable.perPage", { size: 30 })}
					</option>
					<option value={40}>
						{t("components.dataTable.perPage", { size: 40 })}
					</option>
					<option value={50}>
						{t("components.dataTable.perPage", { size: 50 })}
					</option>
				</select>
			)}
			{totalPages > 1 && (
				<div className="flex items-center gap-1">
					<button
						type="button"
						onClick={() => setPage(page - 1)}
						disabled={page === 1}
						className="pagination-btn px-2 py-1 text-xs rounded-(--radius-button) border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{t("common.prev")}
					</button>
					{Array.from({ length: Math.min(7, totalPages) }, (_, i) => {
						let pageNum: number;
						if (totalPages <= 7) {
							pageNum = i + 1;
						} else if (page <= 4) {
							pageNum = i + 1;
							if (i === 6) pageNum = totalPages;
						} else if (page >= totalPages - 3) {
							pageNum = totalPages - 6 + i;
							if (i === 0) pageNum = 1;
						} else {
							pageNum = page - 3 + i;
							if (i === 0) pageNum = 1;
							if (i === 6) pageNum = totalPages;
						}
						return (
							<button
								key={pageNum}
								type="button"
								onClick={() => setPage(pageNum)}
								className={`pagination-btn px-2 py-1 text-xs rounded-(--radius-button) border min-w-8 text-center ${
									page === pageNum
										? "bg-(--accent) text-white border-(--accent)"
										: "bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600"
								}`}
							>
								{pageNum}
							</button>
						);
					})}
					<button
						type="button"
						onClick={() => setPage(page + 1)}
						disabled={page >= totalPages}
						className="pagination-btn px-2 py-1 text-xs rounded-(--radius-button) border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{t("common.next")}
					</button>
				</div>
			)}
		</div>
	);
}
