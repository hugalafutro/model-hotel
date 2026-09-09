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
	announcement,
	onPrev,
	onNext,
}: ModalNavProps & {
	/** What to read out, written by Modal when the user steps. */
	announcement: string;
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
			    close button, and it reads as bare digits. This says the same
			    thing in a sentence, and is announced because stepping changes
			    which row the dialog shows while its title stays the same. */}
			<span aria-live="polite" className="sr-only">
				{announcement}
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
