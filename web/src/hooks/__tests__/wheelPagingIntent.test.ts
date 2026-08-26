import { wheelPagingIntent } from "@web-shared/wheel-paging";
import { describe, expect, it } from "vitest";

// The paging arithmetic both frontends' useWheelPaging runs on. Driven
// directly here — no DOM, no React — so the delta-mode normalization, the
// jitter floor, the scroll-versus-edge test and the gesture clock are pinned
// independently of either app's listener wiring.

/** A container with no horizontal overflow: nothing to scroll, so always at the edge. */
const FITS = { scrollLeft: 0, scrollWidth: 100, clientWidth: 100 };
/** Scrolled hard left of a container twice its visible width. */
const AT_LEFT_EDGE = { scrollLeft: 0, scrollWidth: 300, clientWidth: 100 };
/** Scrolled hard right of the same container. */
const AT_RIGHT_EDGE = { scrollLeft: 200, scrollWidth: 300, clientWidth: 100 };

const NUDGE = { deltaX: 120, deltaY: 0, deltaMode: 0 };
const BACK_NUDGE = { deltaX: -120, deltaY: 0, deltaMode: 0 };

describe("wheelPagingIntent", () => {
	it("ignores a dominant vertical wheel and leaves the gesture clock alone", () => {
		const intent = wheelPagingIntent(
			{ deltaX: 0, deltaY: 200, deltaMode: 0 },
			FITS,
			1000,
			5000,
		);
		expect(intent.action).toBe("ignore");
		expect(intent.lastEventAt).toBe(1000);
	});

	it("ignores an equal diagonal, where there is no horizontal intent", () => {
		const intent = wheelPagingIntent(
			{ deltaX: 100, deltaY: 100, deltaMode: 0 },
			FITS,
			0,
			5000,
		);
		expect(intent.action).toBe("ignore");
	});

	it("ignores sub-pixel jitter and leaves the gesture clock alone", () => {
		const intent = wheelPagingIntent(
			{ deltaX: 1, deltaY: 0, deltaMode: 0 },
			FITS,
			1000,
			5000,
		);
		expect(intent.action).toBe("ignore");
		expect(intent.lastEventAt).toBe(1000);
	});

	it("scales line-mode deltas past the jitter floor", () => {
		// 1 line * 16px = 16px normalized; the same raw 1 in pixel mode is jitter.
		expect(
			wheelPagingIntent({ deltaX: 1, deltaY: 0, deltaMode: 1 }, FITS, 0, 5000)
				.action,
		).toBe("page");
	});

	it("scales page-mode deltas by the container's visible width", () => {
		expect(
			wheelPagingIntent({ deltaX: 1, deltaY: 0, deltaMode: 2 }, FITS, 0, 5000)
				.action,
		).toBe("page");
	});

	it("falls back to the line height for page-mode on a zero-width container", () => {
		// An unmeasured container would otherwise scale the delta to 0 and read
		// as jitter, losing the gesture entirely.
		const intent = wheelPagingIntent(
			{ deltaX: 1, deltaY: 0, deltaMode: 2 },
			{ scrollLeft: 0, scrollWidth: 0, clientWidth: 0 },
			0,
			5000,
		);
		expect(intent.action).toBe("page");
	});

	it("defers to native scroll while content remains to the right", () => {
		const intent = wheelPagingIntent(NUDGE, AT_LEFT_EDGE, 0, 5000);
		expect(intent.action).toBe("scroll");
		expect(intent.forward).toBe(true);
	});

	it("defers to native scroll while content remains to the left", () => {
		const intent = wheelPagingIntent(BACK_NUDGE, AT_RIGHT_EDGE, 0, 5000);
		expect(intent.action).toBe("scroll");
		expect(intent.forward).toBe(false);
	});

	it("advances the gesture clock on a scroll, so a fling into the edge is not fresh", () => {
		const scrolled = wheelPagingIntent(NUDGE, AT_LEFT_EDGE, 0, 5000);
		expect(scrolled.lastEventAt).toBe(5000);
		// The browser has now scrolled to the edge and the same fling continues.
		const atEdge = wheelPagingIntent(
			NUDGE,
			AT_RIGHT_EDGE,
			scrolled.lastEventAt,
			5050,
		);
		expect(atEdge.action).toBe("absorb");
	});

	it("pages forward once pinned at the right edge", () => {
		const intent = wheelPagingIntent(NUDGE, AT_RIGHT_EDGE, 0, 5000);
		expect(intent.action).toBe("page");
		expect(intent.forward).toBe(true);
	});

	it("pages backward once pinned at the left edge", () => {
		const intent = wheelPagingIntent(BACK_NUDGE, AT_LEFT_EDGE, 0, 5000);
		expect(intent.action).toBe("page");
		expect(intent.forward).toBe(false);
	});

	it("pages a container that has no horizontal overflow at all", () => {
		expect(wheelPagingIntent(NUDGE, FITS, 0, 5000).action).toBe("page");
	});

	it("treats a fractional pixel off the edge as pinned", () => {
		// The 1px slack: high-DPI displays report a scrollLeft that never quite
		// reaches scrollWidth - clientWidth.
		const intent = wheelPagingIntent(
			NUDGE,
			{ scrollLeft: 199.5, scrollWidth: 300, clientWidth: 100 },
			0,
			5000,
		);
		expect(intent.action).toBe("page");
	});

	it("absorbs the rest of a held paddle at the edge", () => {
		const first = wheelPagingIntent(NUDGE, AT_RIGHT_EDGE, 0, 5000);
		expect(first.action).toBe("page");
		const second = wheelPagingIntent(
			NUDGE,
			AT_RIGHT_EDGE,
			first.lastEventAt,
			5050,
		);
		expect(second.action).toBe("absorb");
		expect(second.lastEventAt).toBe(5050);
	});

	it("pages again once the wheel has been idle", () => {
		const first = wheelPagingIntent(NUDGE, AT_RIGHT_EDGE, 0, 5000);
		const later = wheelPagingIntent(
			NUDGE,
			AT_RIGHT_EDGE,
			first.lastEventAt,
			5250,
		);
		expect(later.action).toBe("page");
	});
});
