// The horizontal-wheel paging decision, shared by both frontends: given one
// wheel event and the scroll container's geometry, say whether the browser
// should scroll it, whether the gesture is being absorbed at the edge, or
// whether it turns a page. Pure arithmetic — no DOM listeners, no React — so
// each app's useWheelPaging holds only the ref plumbing and the listener.

// A discrete wheel nudge / tilt-paddle click pages immediately on its leading
// edge; anything that arrives within IDLE_GAP_MS of the previous wheel event is
// treated as the same gesture and ignored, so holding the paddle (or a trackpad
// momentum stream) advances exactly one page instead of autoscrolling.
const IDLE_GAP_MS = 200;
// Rough px-per-line factor for line-mode (deltaMode === 1) wheel events.
const LINE_HEIGHT = 16;
// Ignore sub-pixel horizontal jitter / momentum dribble (normalized px).
const MIN_DELTA = 2;

/** The parts of a WheelEvent the decision reads. A DOM WheelEvent satisfies it. */
export interface WheelSample {
	deltaX: number;
	deltaY: number;
	deltaMode: number;
}

/** The parts of the scroll container the decision reads. An HTMLElement satisfies it. */
export interface ScrollGeometry {
	scrollLeft: number;
	scrollWidth: number;
	clientWidth: number;
}

/**
 * What to do with one wheel event:
 * - `ignore` — not a horizontal gesture, or below the jitter floor. Nothing
 *   happens and the gesture clock is not touched.
 * - `scroll` — horizontal, but the container can still scroll that way, so the
 *   browser scrolls it natively.
 * - `absorb` — at the horizontal edge, but still the same gesture as the last
 *   event, so the container must not rubber-band and no page turns.
 * - `page` — at the edge on a fresh gesture: turn one page.
 */
export type WheelPagingAction = "ignore" | "scroll" | "absorb" | "page";

export interface WheelPagingIntent {
	action: WheelPagingAction;
	/** True for a rightward/forward gesture, i.e. towards the next page. */
	forward: boolean;
	/**
	 * The gesture clock the caller should keep. Every horizontal event past the
	 * jitter floor advances it, including the ones handed back to the browser to
	 * scroll, so a scroll that runs into the edge does not read as a fresh nudge.
	 */
	lastEventAt: number;
}

export function wheelPagingIntent(
	e: WheelSample,
	geometry: ScrollGeometry,
	lastEventAt: number,
	now: number,
): WheelPagingIntent {
	// Only horizontal intent: a dedicated horizontal wheel, tilt buttons, or a
	// trackpad horizontal swipe. A plain vertical wheel has deltaX 0 and is
	// ignored, so up/down scrolling keeps working.
	if (Math.abs(e.deltaX) <= Math.abs(e.deltaY)) {
		return { action: "ignore", forward: false, lastEventAt };
	}

	// Normalize to ~pixels so the jitter floor and direction hold across wheel
	// delta modes: line-mode (1) scales by LINE_HEIGHT, page-mode (2) by the
	// container's visible width (rare hardware, but a raw page-mode deltaX of 1
	// would otherwise fall under MIN_DELTA).
	let dx = e.deltaX;
	if (e.deltaMode === 1) dx *= LINE_HEIGHT;
	else if (e.deltaMode === 2) dx *= geometry.clientWidth || LINE_HEIGHT;
	if (Math.abs(dx) < MIN_DELTA) {
		return { action: "ignore", forward: false, lastEventAt };
	}
	const forward = dx > 0;
	const isNewGesture = now - lastEventAt > IDLE_GAP_MS;

	// Scrolling wins: if the container can still scroll sideways in this
	// direction, let the browser scroll it natively. Paging only starts once the
	// container has no horizontal overflow (it fits) or is pinned at the
	// corresponding edge. The 1px slack absorbs fractional scrollLeft values on
	// high-DPI displays.
	const maxScrollLeft = geometry.scrollWidth - geometry.clientWidth;
	const canScrollThisWay = forward
		? geometry.scrollLeft < maxScrollLeft - 1
		: geometry.scrollLeft > 1;
	if (canScrollThisWay) {
		return { action: "scroll", forward, lastEventAt: now };
	}

	// At the edge. One page per discrete nudge: the rest of a held paddle or a
	// trackpad momentum stream is absorbed until the wheel goes idle again.
	return {
		action: isNewGesture ? "page" : "absorb",
		forward,
		lastEventAt: now,
	};
}
