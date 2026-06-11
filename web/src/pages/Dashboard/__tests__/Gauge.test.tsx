import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Gauge } from "../Gauge";

// Gauge uses dropTrailingZero which is a pure function - no animation.
// But suffix is rendered inline with the value text.

describe("Gauge", () => {
	const defaultProps = {
		label: "Error Rate",
		value: 45,
		decimals: 0,
		suffix: "%",
		color: "#ef4444",
	};

	it("renders with value, suffix, and label", () => {
		const { container } = render(<Gauge {...defaultProps} />);

		expect(container.textContent).toContain("45");
		expect(container.textContent).toContain("Error Rate");
	});

	it("displays percentage value correctly", () => {
		const { container } = render(<Gauge {...defaultProps} value={75} />);

		expect(container.textContent).toContain("75");
	});

	it("works with 0% value", () => {
		const { container } = render(<Gauge {...defaultProps} value={0} />);

		expect(container.textContent).toContain("0");
	});

	it("works with 100% value", () => {
		const { container } = render(<Gauge {...defaultProps} value={100} />);

		expect(container.textContent).toContain("100");
	});

	it("displays value with decimals when specified", () => {
		const { container } = render(
			<Gauge {...defaultProps} value={45.678} decimals={2} />,
		);

		expect(container.textContent).toContain("45.68");
	});

	it("displays different suffix", () => {
		const { container } = render(
			<Gauge {...defaultProps} value={250} suffix="ms" decimals={0} />,
		);

		expect(container.textContent).toContain("250");
		expect(container.textContent).toContain("ms");
	});

	it("renders tooltip if provided", () => {
		render(
			<Gauge
				{...defaultProps}
				tooltip="Shows the current error rate percentage"
			/>,
		);

		const gauge = screen.getByTitle("Shows the current error rate percentage");
		expect(gauge).toBeInTheDocument();
	});

	it("applies interactive styles when onClick is provided", () => {
		const onClick = vi.fn();
		render(<Gauge {...defaultProps} onClick={onClick} />);

		const button = screen.getByRole("button");
		// Pointer comes from the global base rule; clickable gauges only add
		// the hover affordance and must not opt out via cursor-default.
		expect(button).not.toHaveClass("cursor-default");
		expect(button).toHaveClass("hover:opacity-80");
	});

	it("does not apply interactive styles when onClick is not provided", () => {
		render(<Gauge {...defaultProps} />);

		const button = screen.getByRole("button");
		expect(button).toHaveClass("cursor-default");
	});

	it("renders SVG gauge with correct arc path", () => {
		const { container } = render(<Gauge {...defaultProps} />);

		const svg = container.querySelector("svg");
		expect(svg).toBeInTheDocument();
		expect(svg).toHaveAttribute("viewBox", "0 0 100 60");

		const paths = container.querySelectorAll("path");
		expect(paths).toHaveLength(2);
	});

	it("scales correctly with maxScale parameter", () => {
		const { container } = render(
			<Gauge {...defaultProps} value={500} maxScale={1000} />,
		);

		expect(container.textContent).toContain("500");
	});

	it("renders values above scale correctly", () => {
		const { container } = render(<Gauge {...defaultProps} value={150} />);

		expect(container.textContent).toContain("150");
	});
});
