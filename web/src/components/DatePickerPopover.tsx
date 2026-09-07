import { useTranslation } from "react-i18next";
import { AccentCalendar } from "./AccentCalendar";
import { AnchoredPopover } from "./AnchoredPopover";

interface DatePickerPopoverProps {
	value: string | null;
	minDate: string;
	onSelect: (dateStr: string) => void;
	onApply: () => void;
	onCancel: () => void;
	onClose: () => void;
	triggerRef?: React.RefObject<HTMLElement | null>;
}

/**
 * Single-day date picker in an AnchoredPopover, hanging from the
 * schedule-disable trigger. The range picker is the same popover with a range
 * calendar in it.
 */
export function DatePickerPopover({
	value,
	minDate,
	onSelect,
	onApply,
	onCancel,
	onClose,
	triggerRef,
}: DatePickerPopoverProps) {
	const { t } = useTranslation();
	const seed = value ?? minDate;
	const seedDate = new Date(`${seed}T00:00:00`);

	return (
		<AnchoredPopover
			triggerSelector="schedule-disable"
			triggerRef={triggerRef}
			title={t("providers.schedule_disable_tooltip")}
			onClose={onClose}
		>
			<AccentCalendar
				initialYear={seedDate.getFullYear()}
				initialMonth={seedDate.getMonth()}
				from={value ?? ""}
				to={value ?? ""}
				minDate={minDate}
				onSelect={onSelect}
			/>

			<p data-testid="date-picker-hint" className="mt-3 text-xs text-gray-400">
				{t("providers.schedule_disable_hint")}
			</p>

			<div className="flex gap-2 mt-3">
				<button
					type="button"
					data-testid="date-picker-cancel"
					onClick={onCancel}
					className="flex-1 px-3 py-1.5 text-xs rounded-(--radius-button) border border-(--border-input) text-(--text-secondary) hover:text-(--text-primary) hover:bg-(--surface-hover) transition-colors"
				>
					{t("common.cancel")}
				</button>
				<button
					type="button"
					data-testid="date-picker-apply"
					onClick={onApply}
					disabled={!value}
					className="flex-1 px-3 py-1.5 text-xs rounded-(--radius-button) border border-(--accent-light) bg-(--accent-light) text-(--accent) hover:brightness-125 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{t("components.logs.dateRangePicker.apply")}
				</button>
			</div>
		</AnchoredPopover>
	);
}
