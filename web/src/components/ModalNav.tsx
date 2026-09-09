import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight } from "@/lib/icons";

/** A row the stepper moved to, counted from one. */
export interface StepPosition {
	position: number;
	total: number;
}

export interface ModalNavProps {
	/** Zero-based position of the open row inside the list behind the modal. */
	index: number;
	total: number;
	onPrev: () => void;
	onNext: () => void;
}

/**
 * Prev/next stepper for a detail modal opened from a list row, so a run of
 * rows can be read without closing the dialog between each one. Rendered by
 * Modal, which also binds the left/right arrow keys that do the same thing.
 *
 * It walks the rows the list has already loaded, which makes the ends of that
 * window the ends of the walk: the dialog never fetches, so a page or scroll
 * window is stepped through exactly as it is drawn behind the modal.
 */
export function ModalNav({
	index,
	total,
	steppedTo,
	onPrev,
	onNext,
}: ModalNavProps & {
	/** The row the user last stepped to, from Modal, or null before they do.
	 * Kept out of this component so a list that shifts underneath an open
	 * dialog does not read itself out. */
	steppedTo: StepPosition | null;
}) {
	const { t } = useTranslation();
	const canPrev = index > 0;
	const canNext = index < total - 1;

	return (
		<div className="flex items-center gap-0.5">
			<button
				type="button"
				onClick={canPrev ? onPrev : undefined}
				// aria-disabled, not disabled: a disabled button drops out of the
				// tab order, and stepping to an end of the list would throw the
				// keyboard focus that just pressed it out of the dialog.
				aria-disabled={!canPrev}
				className="ui-icon-btn p-2"
				aria-label={t("common.prevRow")}
			>
				<ChevronLeft size={18} />
			</button>
			<span
				aria-hidden="true"
				className="text-xs text-(--text-tertiary) tabular-nums select-none"
			>
				{index + 1}/{total}
			</span>
			{/* The compact readout above is what there is room for beside the
			    close button, and it reads as bare digits. These say the same
			    thing in a sentence: the first so the position can be read on
			    arrival, the second because stepping changes which row the
			    dialog shows while its title stays the same, and it is only
			    ever filled in by a step the user took. */}
			<span className="sr-only">
				{t("common.rowPosition", { position: index + 1, total })}
			</span>
			<span aria-live="polite" className="sr-only">
				{steppedTo &&
					t("common.rowPosition", {
						position: steppedTo.position,
						total: steppedTo.total,
					})}
			</span>
			<button
				type="button"
				onClick={canNext ? onNext : undefined}
				aria-disabled={!canNext}
				className="ui-icon-btn p-2"
				aria-label={t("common.nextRow")}
			>
				<ChevronRight size={18} />
			</button>
		</div>
	);
}
