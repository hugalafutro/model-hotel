import type { ReactNode } from "react";
import { useCallback } from "react";

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
}: {
    label: string;
    field: F;
    sort: SortState<F>;
    onSort: (f: F) => void;
    tooltip?: string;
}) {
    const active = sort.field === field;
    return (
        <th
            className={`${HEADER_BASE} select-none hover:text-gray-200`}
            title={tooltip}
        >
            <span className="cursor-pointer" onClick={() => onSort(field)}>
                {label}{" "}
                <span className="inline-block w-3 text-center">
                    {active ? (sort.dir === "asc" ? "↑" : "↓") : " "}
                </span>
            </span>
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
    index,
    children,
    className = "",
}: {
    index: number;
    children: ReactNode;
    className?: string;
}) {
    return (
        <tr
            className={`${index % 2 === 1 ? "bg-white/3" : ""}   hover:bg-(--surface-hover) transition-colors ${className}`}
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
            <td
                colSpan={colSpan}
                className="px-4 py-8 text-center text-gray-500"
            >
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
}: {
    page: number;
    totalPages: number;
    totalItems: number;
    pageSize: number;
    onPageChange: (page: number) => void;
    onPageSizeChange: (size: number) => void;
    label?: string;
}) {
    const setPage = useCallback(
        (p: number) => onPageChange(Math.max(1, Math.min(totalPages, p))),
        [onPageChange, totalPages],
    );

    return (
        <div className="flex items-center gap-3">
            <div className="text-sm text-gray-500">
                {(page - 1) * pageSize + 1}
                {totalItems > pageSize ? (
                    <> to {Math.min(page * pageSize, totalItems)}</>
                ) : null}{" "}
                of {totalItems} {label}
            </div>
            <select
                value={pageSize}
                onChange={(e) => onPageSizeChange(Number(e.target.value))}
                className="ui-input ui-input-sm"
            >
                <option value={10}>10 / page</option>
                <option value={20}>20 / page</option>
                <option value={30}>30 / page</option>
                <option value={40}>40 / page</option>
                <option value={50}>50 / page</option>
            </select>
            {totalPages > 1 && (
                <div className="flex items-center gap-1">
                    <button
                        type="button"
                        onClick={() => setPage(page - 1)}
                        disabled={page === 1}
                        className="px-2 py-1 text-xs rounded-(--radius-button) border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        Prev
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
                                className={`px-2 py-1 text-xs rounded-(--radius-button) border min-w-8 text-center ${
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
                        className="px-2 py-1 text-xs rounded-(--radius-button) border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        Next
                    </button>
                </div>
            )}
        </div>
    );
}
