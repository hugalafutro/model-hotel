import { CalendarDays, X } from "lucide-react";
import { useTranslation } from "react-i18next";
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
	const { t } = useTranslation();
	const dateRangeLabel =
		dateFrom && dateTo ? formatDateRangeShort(dateFrom, dateTo) : "";

	return (
		<div className="flex items-center gap-1">
			<button
				type="button"
				onClick={onToggleDatePicker}
				className={`flex items-center justify-center h-9 w-9 rounded-(--radius-button) text-sm border transition-colors cursor-pointer ${
					hasDateFilter
						? "bg-(--accent)/15 text-(--accent) border-(--accent)/40 hover:bg-(--accent)/25"
						: "bg-(--surface-input) text-(--text-secondary) border-(--border-input) hover:text-(--text-primary) hover:border-(--border-default)"
				}`}
				title={
					hasDateFilter && dateFrom && dateTo
						? t("components.logs.dateFilterButton.dateFilterWithRange", {
								range: dateRangeLabel,
							})
						: t("components.logs.dateFilterButton.filterByDateRange")
				}
				aria-label={
					hasDateFilter && dateFrom && dateTo
						? t("components.logs.dateFilterButton.dateFilterWithRange", {
								range: dateRangeLabel,
							})
						: t("components.logs.dateFilterButton.filterByDateRange")
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
							? t("components.logs.dateFilterButton.clearDateFilter", {
									range: dateRangeLabel,
								})
							: t("components.logs.dateFilterButton.clearDateFilter", {
									range: "",
								})
					}
					aria-label={
						hasDateFilter && dateFrom && dateTo
							? t("components.logs.dateFilterButton.clearDateFilter", {
									range: dateRangeLabel,
								})
							: t("components.logs.dateFilterButton.clearDateFilter", {
									range: "",
								})
					}
				>
					<X size={14} />
				</button>
			)}
		</div>
	);
}
