import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, ChevronRight } from "@/lib/icons";

export type StepDirection = "prev" | "next";

/** A step the reader took. seq grows with every step, so two steps the same
 * way still read as two separate announcements. */
export interface StepAnnouncement {
	dir: StepDirection;
	seq: number;
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
 * window is stepped through exactly as it is drawn behind the modal. It shows
 * no "n of m" count for the same reason: the loaded window is a slice of the
 * log, not the log, so its size and offsets are not numbers worth reading.
 */
export function ModalNav({
	index,
	total,
	lastStep,
	onPrev,
	onNext,
}: ModalNavProps & {
	/** The step the reader last took, from Modal, or null before they do. */
	lastStep: StepAnnouncement | null;
}) {
	const { t } = useTranslation();
	const canPrev = index > 0;
	const canNext = index < total - 1;
	const prevRef = useRef<HTMLButtonElement>(null);
	const nextRef = useRef<HTMLButtonElement>(null);

	// Every step, clicked or from an arrow key, plays a press on the arrow it
	// went through: the body swaps in place, so without it a keyboard step
	// changes the data with nothing on screen saying a step happened. Keyed
	// on seq, so two steps the same way pulse twice.
	useEffect(() => {
		if (!lastStep) return;
		const btn = (lastStep.dir === "prev" ? prevRef : nextRef).current;
		if (!btn) return;
		const reduced = window.matchMedia(
			"(prefers-reduced-motion: reduce)",
		).matches;
		const lit = {
			color: "var(--icon-hover-color)",
			filter: "drop-shadow(var(--icon-hover-glow))",
		};
		btn.animate(
			[
				{ ...lit, transform: reduced ? "none" : "scale(0.8)" },
				{ transform: "none" },
			],
			{ duration: 250, easing: "ease-out" },
		);
	}, [lastStep]);

	return (
		<div className="flex items-center gap-0.5">
			<button
				ref={prevRef}
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
			{/* Stepping swaps the dialog's body while its title and the focused
			    arrow stay the same, so a screen reader would otherwise hear
			    nothing. The announcement is keyed by the step, so it is a new
			    node each time and is read even when the words repeat. */}
			<span aria-live="polite" className="sr-only">
				{lastStep && (
					<span key={lastStep.seq}>
						{t(lastStep.dir === "prev" ? "common.prevRow" : "common.nextRow")}
					</span>
				)}
			</span>
			<button
				ref={nextRef}
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
