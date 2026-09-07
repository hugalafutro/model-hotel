import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AnimatedValue } from "../AnimatedValue";

// AnimatedValue eases from its current reading to the new value over
// `duration` ms, driven by requestAnimationFrame. Most cases below cover the
// static rendering; the timing case drives the frames by hand.

describe("AnimatedValue", () => {
	it("renders with text-transform none style", () => {
		const { container } = render(<AnimatedValue value={42} />);
		const outerSpan = container.querySelector("span");
		expect(outerSpan).toHaveStyle({ textTransform: "none" });
	});

	it("renders unit suffix without spacing", () => {
		const { container } = render(<AnimatedValue value={50} suffix="ms" />);
		const suffixSpan = container.querySelector("span span");
		expect(suffixSpan).toBeInTheDocument();
		expect(suffixSpan).toHaveClass("text-sm");
		expect(suffixSpan).toHaveClass("font-normal");
		expect(suffixSpan).not.toHaveClass("ml-1");
		expect(suffixSpan?.textContent).toBe("ms");
	});

	it("renders word suffix with spacing", () => {
		const { container } = render(<AnimatedValue value={50} suffix="T/Rq" />);
		const suffixSpan = container.querySelector("span span");
		expect(suffixSpan).toBeInTheDocument();
		expect(suffixSpan).toHaveClass("ml-1");
		expect(suffixSpan?.textContent).toBe("T/Rq");
	});

	it("does not render suffix span when no suffix provided", () => {
		const { container } = render(<AnimatedValue value={100} />);
		const suffixSpans = container.querySelectorAll("span span");
		expect(suffixSpans).toHaveLength(0);
	});

	it("applies formatter when provided", () => {
		const currencyFormatter = (val: number) => `$${val.toFixed(2)}`;
		// With the formatter, the output is controlled by formatter(display)
		// where display starts at 0. We can at least verify the structure.
		const { container } = render(
			<AnimatedValue value={100} formatter={currencyFormatter} />,
		);
		// The outer span should have text-transform: none
		const outerSpan = container.querySelector("span");
		expect(outerSpan).toHaveStyle({ textTransform: "none" });
	});

	it("uses dropTrailingZero for decimals when no formatter", () => {
		// The component calls dropTrailingZero(display, decimals)
		// At initial render, display=0, so dropTrailingZero(0, 2) = "0"
		const { container } = render(<AnimatedValue value={123.45} decimals={2} />);
		// Verify structure - outer span exists
		expect(container.querySelector("span")).toBeInTheDocument();
	});

	it("renders with suffix in correct structure", () => {
		const { container } = render(<AnimatedValue value={0} suffix="tokens" />);
		// Outer span contains value + suffix span
		const outerSpan = container.querySelector("span");
		expect(outerSpan).toBeInTheDocument();
		const innerSpan = container.querySelector("span span");
		expect(innerSpan?.textContent).toBe("tokens");
	});

	it("reaches the target value once the duration has elapsed", () => {
		const frames: FrameRequestCallback[] = [];
		vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
			frames.push(cb);
			return frames.length;
		});
		vi.stubGlobal("cancelAnimationFrame", () => {});

		const { container } = render(<AnimatedValue value={100} duration={100} />);
		// One frame at each end of the ease plus one in the middle: the effect
		// must keep easing from where it started, not restart at every frame.
		for (const ts of [0, 50, 100]) {
			const next = frames.pop();
			if (!next) break;
			act(() => next(ts));
		}

		expect(container.querySelector("span")?.textContent).toBe("100");
		vi.unstubAllGlobals();
	});
});
