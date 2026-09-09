import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight } from "@/lib/icons";

export interface ModalNavProps {
	/** Zero-based position of the open row inside the list behind the modal. */
	index: number;
	total: number;
	onPrev: () => void;
	onNext: () => void;
}

/**
 * Prev/next stepper for a detail modal opened from a list row, so a run of
 * rows can be read without closing the dialog between each one.
 *
 * It walks the rows the list has already loaded, which makes the ends of that
 * window the ends of the walk: the dialog never fetches, so a page or scroll
 * window is stepped through exactly as it is drawn behind the modal.
 */
export function ModalNav({ index, total, onPrev, onNext }: ModalNavProps) {
	const { t } = useTranslation();
	const canPrev = index > 0;
	const canNext = index < total - 1;

	// Left/right arrows drive the stepper, matching the buttons. Bound on the
	// document for the same reason Escape is (see Modal): the control that
	// opened the dialog is gone, so focus can sit on <body>.
	useEffect(() => {
		const onKeyDown = (e: KeyboardEvent) => {
			if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
			const el = e.target as HTMLElement | null;
			// Arrow keys belong to the field being typed in, when there is one.
			if (el?.closest?.("input, textarea, select, [contenteditable='true']"))
				return;
			if (e.key === "ArrowLeft" && canPrev) onPrev();
			if (e.key === "ArrowRight" && canNext) onNext();
		};
		document.addEventListener("keydown", onKeyDown);
		return () => document.removeEventListener("keydown", onKeyDown);
	}, [canPrev, canNext, onPrev, onNext]);

	return (
		<div className="flex items-center gap-0.5">
			<button
				type="button"
				onClick={onPrev}
				disabled={!canPrev}
				className="ui-icon-btn p-2"
				aria-label={t("common.prev")}
			>
				<ChevronLeft size={18} />
			</button>
			<span className="text-xs text-(--text-tertiary) tabular-nums select-none">
				{index + 1} / {total}
			</span>
			<button
				type="button"
				onClick={onNext}
				disabled={!canNext}
				className="ui-icon-btn p-2"
				aria-label={t("common.next")}
			>
				<ChevronRight size={18} />
			</button>
		</div>
	);
}
