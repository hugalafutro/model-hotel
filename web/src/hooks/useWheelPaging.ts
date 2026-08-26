import { wheelPagingIntent } from "@web-shared/wheel-paging";
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
	 * behaves normally. Pass `totalPages > 1` so we only hijack the horizontal
	 * wheel when there is actually more than one page to move between.
	 */
	enabled?: boolean;
}

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

		// The gesture clock wheelPagingIntent reads and hands back: a gap larger
		// than its idle threshold marks the start of a fresh gesture.
		let lastEventAt = 0;

		const onWheel = (e: WheelEvent) => {
			const intent = wheelPagingIntent(e, el, lastEventAt, Date.now());
			if (intent.action === "ignore") return;
			lastEventAt = intent.lastEventAt;
			// The browser still owns the scroll while the container has somewhere
			// to go sideways.
			if (intent.action === "scroll") return;

			// At the horizontal boundary: turn the gesture into paging and stop
			// the container from rubber-banding sideways. This happens for an
			// absorbed event too, which is what keeps a held paddle from
			// rubber-banding between page turns.
			e.preventDefault();
			if (intent.action !== "page") return;

			const s = stateRef.current;
			if (intent.forward && s.canNext) s.onNext();
			else if (!intent.forward && s.canPrev) s.onPrev();
		};

		el.addEventListener("wheel", onWheel, { passive: false });
		return () => el.removeEventListener("wheel", onWheel);
	}, [enabled]);

	return ref;
}
