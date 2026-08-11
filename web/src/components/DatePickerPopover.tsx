import { useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/lib/icons";
import { AccentCalendar } from "./AccentCalendar";

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
 * Single-day date picker popover that uses a React Portal to escape any
 * overflow-hidden parent containers. Positions itself relative to the
 * trigger element using a provided containerRef. Mirrors
 * DateRangePickerPopover's portal/positioning/click-outside behavior but
 * selects a single day instead of a range.
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
	const popoverRef = useRef<HTMLDivElement>(null);
	const [position, setPosition] = useState<{ top: number; left: number }>({
		top: 0,
		left: 0,
	});

	// Compute popover position relative to the trigger button.
	// Re-computes on scroll/resize so the popover tracks its anchor.
	useLayoutEffect(() => {
		const scope = triggerRef?.current ?? document;
		const trigger = scope.querySelector<HTMLElement>(
			'[data-popover-trigger="schedule-disable"]',
		);
		if (!trigger) return;

		const popoverWidth = 288; // w-72 = 18rem = 288px
		const gap = 8; // mt-2

		const reposition = () => {
			const triggerRect = trigger.getBoundingClientRect();
			const top = triggerRect.bottom + gap;
			let left = triggerRect.right - popoverWidth;
			// Clamp to viewport so the popover never renders off-screen.
			const viewportWidth = window.innerWidth;
			if (left < 0) left = 0;
			if (left + popoverWidth > viewportWidth)
				left = viewportWidth - popoverWidth;
			setPosition({ top, left });
		};

		reposition();

		window.addEventListener("scroll", reposition, true);
		window.addEventListener("resize", reposition);
		return () => {
			window.removeEventListener("scroll", reposition, true);
			window.removeEventListener("resize", reposition);
		};
	}, [triggerRef]);

	// Close on click outside
	useLayoutEffect(() => {
		const scope = triggerRef?.current ?? document;
		const handleClickOutside = (e: MouseEvent) => {
			if (
				popoverRef.current &&
				!popoverRef.current.contains(e.target as Node)
			) {
				// Check if click is on the trigger button (which toggles the picker)
				const trigger = scope.querySelector<HTMLElement>(
					'[data-popover-trigger="schedule-disable"]',
				);
				if (trigger?.contains(e.target as Node)) return;
				onClose();
			}
		};
		document.addEventListener("mousedown", handleClickOutside);
		return () => document.removeEventListener("mousedown", handleClickOutside);
	}, [onClose, triggerRef]);

	const seed = value ?? minDate;
	const seedDate = new Date(`${seed}T00:00:00`);

	const popover = (
		<div
			ref={popoverRef}
			className="fixed w-72 p-4 ui-card shadow-2xl z-50"
			style={{ top: position.top, left: position.left }}
		>
			<div className="flex items-center justify-between mb-3">
				<span className="text-sm font-semibold text-(--text-primary)">
					{t("providers.schedule_disable_tooltip")}
				</span>
				<button
					type="button"
					onClick={onClose}
					className="ui-icon-btn leading-none p-1"
					title={t("components.logs.dateRangePicker.close")}
					aria-label={t("components.logs.dateRangePicker.close")}
				>
					<X size={16} />
				</button>
			</div>

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
		</div>
	);

	return createPortal(popover, document.body);
}
