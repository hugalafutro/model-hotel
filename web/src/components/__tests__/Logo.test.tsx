import { render, screen } from "@testing-library/react";
import { Logo } from "../Logo";

describe("Logo", () => {
	it("renders SVG element", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		expect(svg.tagName).toBe("svg");
	});

	it("renders with default empty className", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		expect(svg).toHaveAttribute("class", "");
	});

	it("applies custom className", () => {
		render(<Logo className="custom-logo-class" />);
		const svg = screen.getByLabelText("Model Hotel");
		expect(svg).toHaveClass("custom-logo-class");
	});

	it("renders hotel building icon", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		const roofPath = svg.querySelector("path");
		expect(roofPath).toBeInTheDocument();
	});

	it("renders spire dot with accent color", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		const circle = svg.querySelector("circle");
		expect(circle).toBeInTheDocument();
		expect(circle).toHaveAttribute("fill", "var(--accent, #4f8cff)");
	});

	it("renders windows", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		const rects = svg.querySelectorAll("rect");
		expect(rects.length).toBeGreaterThanOrEqual(3);
	});

	it("renders 'Model' text", () => {
		render(<Logo />);
		expect(screen.getByText("Model")).toBeInTheDocument();
	});

	it("renders 'Hotel' text with accent color", () => {
		render(<Logo />);
		const hotelText = screen.getByText("Hotel");
		expect(hotelText).toBeInTheDocument();
		expect(hotelText).toHaveAttribute("fill", "var(--accent, #4f8cff)");
	});

	it("has correct viewBox", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		expect(svg).toHaveAttribute("viewBox", "0 0 220 48");
	});

	it("has xmlns attribute", () => {
		render(<Logo />);
		const svg = screen.getByLabelText("Model Hotel");
		expect(svg).toHaveAttribute("xmlns", "http://www.w3.org/2000/svg");
	});
});
