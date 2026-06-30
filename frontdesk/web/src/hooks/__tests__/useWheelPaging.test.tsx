import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useWheelPaging } from "../useWheelPaging";

interface HarnessProps {
	canPrev: boolean;
	canNext: boolean;
	onPrev: () => void;
	onNext: () => void;
	enabled?: boolean;
}

function Harness(props: HarnessProps) {
	const ref = useWheelPaging<HTMLDivElement>(props);
	return <div ref={ref} data-testid="container" />;
}

function setup(overrides: Partial<HarnessProps> = {}) {
	const onPrev = vi.fn();
	const onNext = vi.fn();
	const utils = render(
		<Harness canPrev canNext onPrev={onPrev} onNext={onNext} {...overrides} />,
	);
	return {
		...utils,
		onPrev,
		onNext,
		container: utils.getByTestId("container"),
	};
}

function setGeometry(
	el: HTMLElement,
	geo: { clientWidth: number; scrollWidth: number; scrollLeft: number },
) {
	Object.defineProperty(el, "clientWidth", {
		configurable: true,
		value: geo.clientWidth,
	});
	Object.defineProperty(el, "scrollWidth", {
		configurable: true,
		value: geo.scrollWidth,
	});
	Object.defineProperty(el, "scrollLeft", {
		configurable: true,
		writable: true,
		value: geo.scrollLeft,
	});
}

describe("useWheelPaging", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("pages forward on a dominant rightward (positive deltaX) wheel", () => {
		const { container, onNext, onPrev } = setup();
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
		expect(onPrev).not.toHaveBeenCalled();
	});

	it("pages backward on a dominant leftward (negative deltaX) wheel", () => {
		const { container, onNext, onPrev } = setup();
		fireEvent.wheel(container, { deltaX: -120, deltaY: 0 });
		expect(onPrev).toHaveBeenCalledTimes(1);
		expect(onNext).not.toHaveBeenCalled();
	});

	it("ignores a dominant vertical wheel so normal scrolling is preserved", () => {
		const { container, onNext, onPrev } = setup();
		const notCanceled = fireEvent.wheel(container, { deltaX: 0, deltaY: 200 });
		expect(onNext).not.toHaveBeenCalled();
		expect(onPrev).not.toHaveBeenCalled();
		// Default not prevented -> the container still scrolls vertically.
		expect(notCanceled).toBe(true);
	});

	it("prevents default for horizontal gestures so the table does not scroll sideways", () => {
		const { container } = setup();
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(notCanceled).toBe(false);
	});

	it("pages on a single small nudge without waiting to cross a threshold", () => {
		const { container, onNext } = setup();
		// A discrete tilt-paddle click sends a small deltaX; it should page at
		// once on the leading edge rather than needing several to accumulate.
		fireEvent.wheel(container, { deltaX: 8, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
	});

	it("advances only one page per gesture until the wheel goes idle", () => {
		const { container, onNext } = setup();
		// A held paddle / momentum stream arrives as back-to-back events with no
		// idle gap: only the leading edge pages, so it never autoscrolls.
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
		// After an idle gap a fresh nudge pages again.
		vi.advanceTimersByTime(250);
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(2);
	});

	it("recognizes line-mode deltas (deltaMode 1)", () => {
		const { container, onNext } = setup();
		// 5 lines * 16px = 80px normalized, comfortably above the jitter floor.
		fireEvent.wheel(container, { deltaX: 5, deltaY: 0, deltaMode: 1 });
		expect(onNext).toHaveBeenCalledTimes(1);
	});

	it("recognizes page-mode deltas (deltaMode 2)", () => {
		const { container, onNext } = setup();
		// A page-mode tick has raw deltaX 1; normalization keeps it above the
		// jitter floor so it still registers as a nudge.
		fireEvent.wheel(container, { deltaX: 1, deltaY: 0, deltaMode: 2 });
		expect(onNext).toHaveBeenCalledTimes(1);
	});

	it("ignores sub-pixel horizontal jitter and lets it scroll natively", () => {
		const { container, onNext } = setup();
		const notCanceled = fireEvent.wheel(container, { deltaX: 1, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		// Below MIN_DELTA -> default untouched, tiny native scroll still happens.
		expect(notCanceled).toBe(true);
	});

	it("does not page past a boundary but still suppresses sideways scroll", () => {
		const { container, onNext } = setup({ canNext: false });
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		expect(notCanceled).toBe(false);
	});

	it("does not attach the listener when disabled", () => {
		const { container, onNext } = setup({ enabled: false });
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		// Listener absent -> default untouched, horizontal scroll behaves normally.
		expect(notCanceled).toBe(true);
	});

	it("defers to native scroll while horizontal content remains to the right", () => {
		const { container, onNext } = setup();
		setGeometry(container, {
			clientWidth: 100,
			scrollWidth: 300,
			scrollLeft: 0,
		});
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		expect(notCanceled).toBe(true);
	});

	it("pages forward only once pinned at the right edge", () => {
		const { container, onNext } = setup();
		// scrollLeft === scrollWidth - clientWidth -> nothing left to scroll right.
		setGeometry(container, {
			clientWidth: 100,
			scrollWidth: 300,
			scrollLeft: 200,
		});
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
		expect(notCanceled).toBe(false);
	});

	it("defers to native scroll while horizontal content remains to the left", () => {
		const { container, onPrev } = setup();
		setGeometry(container, {
			clientWidth: 100,
			scrollWidth: 300,
			scrollLeft: 200,
		});
		const notCanceled = fireEvent.wheel(container, { deltaX: -120, deltaY: 0 });
		expect(onPrev).not.toHaveBeenCalled();
		expect(notCanceled).toBe(true);
	});

	it("pages backward only once pinned at the left edge", () => {
		const { container, onPrev } = setup();
		setGeometry(container, {
			clientWidth: 100,
			scrollWidth: 300,
			scrollLeft: 0,
		});
		const notCanceled = fireEvent.wheel(container, { deltaX: -120, deltaY: 0 });
		expect(onPrev).toHaveBeenCalledTimes(1);
		expect(notCanceled).toBe(false);
	});

	it("does not flip pages mid-scroll; a separate nudge at the edge pages", () => {
		const { container, onNext } = setup();
		setGeometry(container, {
			clientWidth: 100,
			scrollWidth: 300,
			scrollLeft: 0,
		});
		// Fling rightward while there is still room: scrolls natively, no page.
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		// The browser scrolls us to the right edge; the same continuous fling
		// continues (no idle gap) and must not flip the page.
		container.scrollLeft = 200;
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		// Once the wheel goes idle, a fresh nudge at the edge pages.
		vi.advanceTimersByTime(250);
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
	});
});
