import { useEffect, useRef } from "react";

interface WheelPagingOptions {
	/** Whether a previous page exists (scroll left). */
	canPrev: boolean;
	/** Whether a next page exists (scroll right). */
	canNext: boolean;
	/** Go to the previous page. */
	onPrev: () => void;
	/** Go to the next page. */
	onNext: () => void;
	/**
	 * When false the wheel listener is not attached, so horizontal scrolling
	 * behaves normally. Pass something like `total > PAGE_SIZE` so we only hijack
	 * the horizontal wheel when there is more than one page to move between.
	 */
	enabled?: boolean;
}

// A discrete wheel nudge / tilt-paddle click pages immediately on its leading
// edge; anything that arrives within IDLE_GAP_MS of the previous wheel event is
// treated as the same gesture and ignored, so holding the paddle (or a trackpad
// momentum stream) advances exactly one page instead of autoscrolling.
const IDLE_GAP_MS = 200;
// Rough px-per-line factor for line-mode (deltaMode === 1) wheel events.
const LINE_HEIGHT = 16;
// Ignore sub-pixel horizontal jitter / momentum dribble (normalized px).
const MIN_DELTA = 2;

/**
 * Pages a scroll container with the mouse's horizontal scroll wheel / tilt
 * buttons (or a trackpad horizontal swipe) while the cursor is over it. A
 * dedicated vertical wheel passes straight through, so regular up/down
 * scrolling is unaffected.
 *
 * Returns a ref to attach to the table's scroll container.
 */
export function useWheelPaging<T extends HTMLElement = HTMLDivElement>({
	canPrev,
	canNext,
	onPrev,
	onNext,
	enabled = true,
}: WheelPagingOptions) {
	const ref = useRef<T | null>(null);
	// Hold the latest props in a ref so the effect can keep a stable listener
	// across renders instead of re-subscribing (and resetting the accumulator)
	// every time the page changes. Synced in an effect (not during render) so
	// the listener always reads fresh values without re-binding.
	const stateRef = useRef({ canPrev, canNext, onPrev, onNext });
	useEffect(() => {
		stateRef.current = { canPrev, canNext, onPrev, onNext };
	});

	useEffect(() => {
		const el = ref.current;
		if (!el || !enabled) return;

		// Timestamp of the previous horizontal wheel event (scroll or page). A
		// gap larger than IDLE_GAP_MS marks the start of a fresh gesture.
		let lastEventAt = 0;

		const onWheel = (e: WheelEvent) => {
			// Only horizontal intent: a dedicated horizontal wheel, tilt buttons,
			// or a trackpad horizontal swipe. A plain vertical wheel has deltaX 0
			// and is ignored, so up/down scrolling keeps working.
			if (Math.abs(e.deltaX) <= Math.abs(e.deltaY)) return;

			// Normalize to ~pixels so the jitter floor and direction hold across
			// wheel delta modes: line-mode (1) scales by LINE_HEIGHT, page-mode
			// (2) by the container's visible width (rare hardware, but a raw
			// page-mode deltaX of 1 would otherwise fall under MIN_DELTA).
			let dx = e.deltaX;
			if (e.deltaMode === 1) dx *= LINE_HEIGHT;
			else if (e.deltaMode === 2) dx *= el.clientWidth || LINE_HEIGHT;
			if (Math.abs(dx) < MIN_DELTA) return;
			const forward = dx > 0;

			const now = Date.now();
			const isNewGesture = now - lastEventAt > IDLE_GAP_MS;
			lastEventAt = now;

			// Scrolling wins: if the table can still scroll sideways in this
			// direction, let the browser scroll it natively. We only page once
			// the container has no horizontal overflow (it fits) or we're pinned
			// at the corresponding edge. The 1px slack absorbs fractional
			// scrollLeft values on high-DPI displays.
			const maxScrollLeft = el.scrollWidth - el.clientWidth;
			const canScrollThisWay = forward
				? el.scrollLeft < maxScrollLeft - 1
				: el.scrollLeft > 1;
			if (canScrollThisWay) return;

			// At the horizontal boundary: turn the gesture into paging and stop
			// the container from rubber-banding sideways.
			e.preventDefault();

			// One page per discrete nudge: ignore the rest of a held paddle or a
			// trackpad momentum stream until the wheel goes idle again.
			if (!isNewGesture) return;

			const s = stateRef.current;
			if (forward && s.canNext) s.onNext();
			else if (!forward && s.canPrev) s.onPrev();
		};

		el.addEventListener("wheel", onWheel, { passive: false });
		return () => el.removeEventListener("wheel", onWheel);
	}, [enabled]);

	return ref;
}
