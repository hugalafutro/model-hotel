import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AccentCalendar } from "../AccentCalendar";
import { formatDateRangeShort } from "../AccentCalendar.utils";

interface DateRangePickerPopoverProps {
	pickerYear: number;
	pickerMonth: number;
	pendingFrom: string | null;
	pendingTo: string | null;
	onCalendarSelect: (dateStr: string) => void;
	onApply: () => void;
	onClear: () => void;
	onClose: () => void;
	/** Which side to anchor the popover. "right" for AppLogs, "left" for Logs. */
	anchor?: "left" | "right";
}

export function DateRangePickerPopover({
	pickerYear,
	pickerMonth,
	pendingFrom,
	pendingTo,
	onCalendarSelect,
	onApply,
	onClear,
	onClose,
	anchor = "right",
}: DateRangePickerPopoverProps) {
	const { t } = useTranslation();

	return (
		<div
			className={`absolute ${anchor}-0 mt-2 w-72 p-4 ui-card shadow-2xl z-50`}
		>
			<div className="flex items-center justify-between mb-3">
				<span className="text-sm font-semibold text-(--text-primary)">
					{t("components.logs.dateRangePicker.selectDateRange")}
				</span>
				<button
					type="button"
					onClick={onClose}
					className="text-gray-400 hover:text-(--text-primary) transition-colors leading-none p-1 hover:drop-shadow-[var(--glow-accent-lg)]"
					title={t("components.logs.dateRangePicker.close")}
					aria-label={t("components.logs.dateRangePicker.close")}
				>
					<X size={16} />
				</button>
			</div>

			<AccentCalendar
				initialYear={pickerYear}
				initialMonth={pickerMonth}
				from={pendingFrom || ""}
				to={pendingTo || ""}
				onSelect={onCalendarSelect}
			/>

			<div className="mt-3 flex items-center justify-between text-xs text-gray-400 min-h-5">
				{pendingFrom && pendingTo ? (
					<span>{formatDateRangeShort(pendingFrom, pendingTo)}</span>
				) : pendingFrom ? (
					<span className="text-(--accent)">
						{t("components.logs.dateRangePicker.selectEndDate")}
					</span>
				) : (
					<span>{t("components.logs.dateRangePicker.selectStartDate")}</span>
				)}
			</div>

			<div className="flex gap-2 mt-3">
				<button
					type="button"
					onClick={onClear}
					className="flex-1 px-3 py-1.5 text-xs rounded-lg border border-(--border-input) text-(--text-secondary) hover:text-(--text-primary) hover:bg-(--surface-hover) transition-colors"
				>
					{t("components.logs.dateRangePicker.clear")}
				</button>
				<button
					type="button"
					onClick={onApply}
					disabled={!pendingFrom}
					className="flex-1 px-3 py-1.5 text-xs rounded-lg border border-(--accent-light) bg-(--accent-light) text-(--accent) hover:brightness-125 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{t("components.logs.dateRangePicker.apply")}
				</button>
			</div>
		</div>
	);
}
