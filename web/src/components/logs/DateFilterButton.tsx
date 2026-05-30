import { CalendarDays, X } from "lucide-react";
import { formatDateRangeShort } from "../AccentCalendar.utils";

interface DateFilterButtonProps {
	hasDateFilter: boolean;
	dateFrom: string | null;
	dateTo: string | null;
	onToggleDatePicker: () => void;
	onClearDateFilter: () => void;
}

export function DateFilterButton({
	hasDateFilter,
	dateFrom,
	dateTo,
	onToggleDatePicker,
	onClearDateFilter,
}: DateFilterButtonProps) {
	return (
		<div className="flex items-center gap-1">
			<button
				type="button"
				onClick={onToggleDatePicker}
				className={`flex items-center justify-center h-9 w-9 rounded-(--radius-button) text-sm border transition-colors cursor-pointer ${
					hasDateFilter
						? "bg-(--accent)/15 text-(--accent) border-(--accent)/40 hover:bg-(--accent)/25"
						: "bg-gray-900/40 text-gray-400 border-gray-700/50 hover:text-(--text-primary) hover:border-gray-500"
				}`}
				title={
					hasDateFilter && dateFrom && dateTo
						? `Date filter: ${formatDateRangeShort(dateFrom, dateTo)} - click to change`
						: "Filter by date range"
				}
				aria-label={
					hasDateFilter && dateFrom && dateTo
						? `Date filter: ${formatDateRangeShort(dateFrom, dateTo)} - click to change`
						: "Filter by date range"
				}
			>
				<CalendarDays size={16} />
			</button>
			{hasDateFilter && (
				<button
					type="button"
					className="inline-flex items-center justify-center h-9 w-6 rounded-(--radius-button) bg-(--accent)/30 text-(--accent) hover:text-(--text-primary) transition-all cursor-default hover:drop-shadow-[var(--glow-accent-lg)]"
					onClick={onClearDateFilter}
					title={
						hasDateFilter && dateFrom && dateTo
							? `Clear date filter (${formatDateRangeShort(dateFrom, dateTo)})`
							: "Clear date filter"
					}
					aria-label={
						hasDateFilter && dateFrom && dateTo
							? `Clear date filter (${formatDateRangeShort(dateFrom, dateTo)})`
							: "Clear date filter"
					}
				>
					<X size={14} />
				</button>
			)}
		</div>
	);
}
