import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useWheelPaging } from "../useWheelPaging";

// The React half of horizontal-wheel paging: attaching the listener, keeping it
// pointed at the latest callbacks, and turning a paging decision into
// preventDefault + a page turn. The decision itself (delta modes, jitter floor,
// gesture clock, edge detection) is arithmetic shared with the other frontend
// and is covered against web-shared/wheel-paging directly.

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

	it("pages forward on a dominant rightward wheel and suppresses sideways scroll", () => {
		const { container, onNext, onPrev } = setup();
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
		expect(onPrev).not.toHaveBeenCalled();
		expect(notCanceled).toBe(false);
	});

	it("pages backward on a dominant leftward wheel", () => {
		const { container, onNext, onPrev } = setup();
		fireEvent.wheel(container, { deltaX: -120, deltaY: 0 });
		expect(onPrev).toHaveBeenCalledTimes(1);
		expect(onNext).not.toHaveBeenCalled();
	});

	it("leaves a dominant vertical wheel untouched so normal scrolling is preserved", () => {
		const { container, onNext, onPrev } = setup();
		const notCanceled = fireEvent.wheel(container, { deltaX: 0, deltaY: 200 });
		expect(onNext).not.toHaveBeenCalled();
		expect(onPrev).not.toHaveBeenCalled();
		// Default not prevented -> the container still scrolls vertically.
		expect(notCanceled).toBe(true);
	});

	it("suppresses sideways scroll at a boundary it cannot page past", () => {
		const { container, onNext } = setup({ canNext: false });
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		expect(notCanceled).toBe(false);
	});

	it("hands a scrollable container back to the browser", () => {
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

	it("absorbs the rest of a held paddle, then pages again once the wheel goes idle", () => {
		const { container, onNext } = setup();
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(1);
		vi.advanceTimersByTime(250);
		fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).toHaveBeenCalledTimes(2);
	});

	it("calls the callbacks from the latest render, not the ones it was mounted with", () => {
		// The listener is bound once and reads its props through a ref, which is
		// the whole reason a page change does not reset the gesture clock.
		const first = vi.fn();
		const second = vi.fn();
		const { getByTestId, rerender } = render(
			<Harness canPrev canNext onPrev={vi.fn()} onNext={first} />,
		);
		rerender(<Harness canPrev canNext onPrev={vi.fn()} onNext={second} />);
		fireEvent.wheel(getByTestId("container"), { deltaX: 120, deltaY: 0 });
		expect(first).not.toHaveBeenCalled();
		expect(second).toHaveBeenCalledTimes(1);
	});

	it("does not attach the listener when disabled", () => {
		const { container, onNext } = setup({ enabled: false });
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		// Listener absent -> default untouched, horizontal scroll behaves normally.
		expect(notCanceled).toBe(true);
	});

	it("removes the listener when the component unmounts", () => {
		const { container, onNext, unmount } = setup();
		unmount();
		const notCanceled = fireEvent.wheel(container, { deltaX: 120, deltaY: 0 });
		expect(onNext).not.toHaveBeenCalled();
		expect(notCanceled).toBe(true);
	});
});
