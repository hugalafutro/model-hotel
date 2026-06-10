import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FuseOutline } from "../FuseOutline";

// Controllable useResizeObserver mock: tests shrink the measured size to 0 to
// unmount the rect (as happens when the entry collapses during a drag-settle)
// and restore it to remount the rect.
const mockSize = { width: 100, height: 40 };
vi.mock("../../hooks/useResizeObserver", () => ({
	useResizeObserver: () => ({ ref: { current: null }, ...mockSize }),
}));

function getRect(container: HTMLElement) {
	return container.querySelector("rect");
}

describe("FuseOutline", () => {
	beforeEach(() => {
		vi.useFakeTimers({ toFake: ["performance"] });
		mockSize.width = 100;
		mockSize.height = 40;
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("starts the fuse animation from zero on first mount", () => {
		const { container } = render(
			<FuseOutline color="#fca5a5" durationMs={10000} />,
		);

		const rect = getRect(container);
		expect(rect?.style.animation).toContain("fuse 10000ms linear forwards");
		expect(rect?.style.animationDelay).toBe("");
	});

	it("resumes mid-animation when the rect remounts after a size collapse", () => {
		const { container, rerender } = render(
			<FuseOutline color="#fca5a5" durationMs={10000} />,
		);
		expect(getRect(container)).not.toBeNull();

		vi.advanceTimersByTime(4000);

		// Size collapses (drag-settle): rect unmounts.
		mockSize.width = 0;
		mockSize.height = 0;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} />);
		expect(getRect(container)).toBeNull();

		// Size restored: rect remounts and must resume 4s in, not restart.
		mockSize.width = 100;
		mockSize.height = 40;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} />);

		const rect = getRect(container);
		expect(rect?.style.animation).toContain("fuse 10000ms linear forwards");
		expect(rect?.style.animationDelay).toBe("-4000ms");
	});

	it("excludes paused time when resuming after a remount", () => {
		const { container, rerender } = render(
			<FuseOutline color="#fca5a5" durationMs={10000} />,
		);

		vi.advanceTimersByTime(2000);
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} paused />);
		expect(getRect(container)?.style.animationPlayState).toBe("paused");

		vi.advanceTimersByTime(1000);
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} />);
		expect(getRect(container)?.style.animationPlayState).toBe("running");

		vi.advanceTimersByTime(2000);

		mockSize.width = 0;
		mockSize.height = 0;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} />);
		mockSize.width = 100;
		mockSize.height = 40;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} />);

		// 5s of wall time minus 1s paused = 4s of animation progress.
		expect(getRect(container)?.style.animationDelay).toBe("-4000ms");
	});

	it("counts an in-progress pause when resuming while paused", () => {
		const { container, rerender } = render(
			<FuseOutline color="#fca5a5" durationMs={10000} />,
		);

		vi.advanceTimersByTime(3000);
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} paused />);

		vi.advanceTimersByTime(2000);

		mockSize.width = 0;
		mockSize.height = 0;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} paused />);
		mockSize.width = 100;
		mockSize.height = 40;
		rerender(<FuseOutline color="#fca5a5" durationMs={10000} paused />);

		// 5s of wall time minus the 2s still-running pause = 3s of progress.
		const rect = getRect(container);
		expect(rect?.style.animationDelay).toBe("-3000ms");
		expect(rect?.style.animationPlayState).toBe("paused");
	});

	it("restarts from zero when the duration changes", () => {
		const { container, rerender } = render(
			<FuseOutline color="#fca5a5" durationMs={10000} />,
		);

		vi.advanceTimersByTime(4000);
		rerender(<FuseOutline color="#fca5a5" durationMs={30000} />);

		const rect = getRect(container);
		expect(rect?.style.animation).toContain("fuse 30000ms linear forwards");
		expect(rect?.style.animationDelay).toBe("");
	});

	it("renders nothing for a non-positive duration", () => {
		const { container } = render(
			<FuseOutline color="#fca5a5" durationMs={0} />,
		);
		expect(container.querySelector("svg")).toBeNull();
	});
});
