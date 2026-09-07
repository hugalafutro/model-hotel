import { useTranslation } from "react-i18next";
import { AccentCalendar } from "../AccentCalendar";
import { formatDateRangeShort } from "../AccentCalendar.utils";
import { AnchoredPopover } from "../AnchoredPopover";

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
	/**
	 * Ref to the container element holding the trigger button.
	 * When provided, the DOM query for the trigger is scoped to this element
	 * instead of document, avoiding conflicts if multiple instances exist.
	 */
	triggerRef?: React.RefObject<HTMLElement | null>;
}

/**
 * Date range picker in an AnchoredPopover, hanging from the date-range trigger.
 */
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
	triggerRef,
}: DateRangePickerPopoverProps) {
	const { t } = useTranslation();

	return (
		<AnchoredPopover
			triggerSelector="date-range"
			triggerRef={triggerRef}
			anchor={anchor}
			title={t("components.logs.dateRangePicker.selectDateRange")}
			onClose={onClose}
		>
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
					className="flex-1 px-3 py-1.5 text-xs rounded-(--radius-button) border border-(--border-input) text-(--text-secondary) hover:text-(--text-primary) hover:bg-(--surface-hover) transition-colors"
				>
					{t("components.logs.dateRangePicker.clear")}
				</button>
				<button
					type="button"
					onClick={onApply}
					disabled={!pendingFrom}
					className="flex-1 px-3 py-1.5 text-xs rounded-(--radius-button) border border-(--accent-light) bg-(--accent-light) text-(--accent) hover:brightness-125 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{t("components.logs.dateRangePicker.apply")}
				</button>
			</div>
		</AnchoredPopover>
	);
}
