import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LoadingSpinner } from "../LoadingSpinner";

describe("LoadingSpinner", () => {
	it("renders spinner div", () => {
		render(<LoadingSpinner />);
		const spinner = screen.getByTestId("spinner");
		expect(spinner).toBeInTheDocument();
		expect(spinner).toHaveClass("animate-spin");
		expect(spinner).toHaveClass("border-b-2");
		expect(spinner).toHaveClass("border-(--accent)");
	});

	it("renders with custom className", () => {
		render(<LoadingSpinner className="custom-spinner" />);
		const spinner = screen.getByTestId("spinner");
		expect(spinner).toHaveClass("custom-spinner");
	});

	it("renders inside centered container", () => {
		render(<LoadingSpinner />);
		const container = screen.getByTestId("spinner").parentElement;
		expect(container).toHaveClass("flex");
		expect(container).toHaveClass("items-center");
		expect(container).toHaveClass("justify-center");
		expect(container).toHaveClass("h-64");
	});
});
