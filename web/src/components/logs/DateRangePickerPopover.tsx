import { X } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
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

/**
 * Date range picker popover that uses a React Portal to escape any
 * overflow-hidden parent containers. Positions itself relative to the
 * trigger element using a provided containerRef.
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
}: DateRangePickerPopoverProps) {
	const { t } = useTranslation();
	const popoverRef = useRef<HTMLDivElement>(null);
	const [position, setPosition] = useState<{ top: number; left: number }>({
		top: 0,
		left: 0,
	});

	// Find the trigger button (sibling of the portal mount point) to position relative to
	useLayoutEffect(() => {
		// The trigger is the closest button in the parent container
		const trigger = document.querySelector(
			'[aria-label="Filter by date range"]',
		);
		if (!trigger) return;

		const triggerRect = trigger.getBoundingClientRect();
		const popoverWidth = 288; // w-72 = 18rem = 288px
		const gap = 8; // mt-2

		const top = triggerRect.bottom + gap;
		const left =
			anchor === "right" ? triggerRect.right - popoverWidth : triggerRect.left;

		setPosition({ top, left });
	}, [anchor]);

	// Close on click outside
	useLayoutEffect(() => {
		const handleClickOutside = (e: MouseEvent) => {
			if (
				popoverRef.current &&
				!popoverRef.current.contains(e.target as Node)
			) {
				// Check if click is on the trigger button (which toggles the picker)
				const trigger = document.querySelector(
					'[aria-label="Filter by date range"]',
				);
				if (trigger?.contains(e.target as Node)) return;
				onClose();
			}
		};
		document.addEventListener("mousedown", handleClickOutside);
		return () => document.removeEventListener("mousedown", handleClickOutside);
	}, [onClose]);

	const popover = (
		<div
			ref={popoverRef}
			className="fixed w-72 p-4 ui-card shadow-2xl z-50"
			style={{ top: position.top, left: position.left }}
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

	return createPortal(popover, document.body);
}
