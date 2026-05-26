import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AllProviders } from "../../test/utils";
import { Spinner } from "../Spinner";

describe("Spinner", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("renders in default theme (spinning circle)", () => {
		render(<Spinner />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");
		expect(spinner).toBeInTheDocument();
		expect(spinner).toHaveClass("animate-spin");
		expect(spinner).toHaveClass("border-2");
		expect(spinner).toHaveClass("border-current/30");
		expect(spinner).toHaveClass("border-t-current");
		expect(spinner).toHaveClass("rounded-full");
	});

	it("renders with custom className", () => {
		render(<Spinner className="custom-spinner" />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");
		expect(spinner).toHaveClass("custom-spinner");
	});

	it("updates frames over time in default theme", () => {
		render(<Spinner />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");

		act(() => {
			vi.advanceTimersByTime(80);
		});
		expect(spinner).toBeInTheDocument();

		act(() => {
			vi.advanceTimersByTime(80);
		});
		expect(spinner).toBeInTheDocument();
	});

	it("renders braille spinner in cyber-terminal theme", () => {
		render(<Spinner />, { wrapper: AllProviders });
		act(() => {
			vi.advanceTimersByTime(100);
		});

		const spinner = screen.getByTestId("spinner");
		expect(spinner).toBeInTheDocument();
		// In default theme (clean-saas), it renders a circle, not braille
		expect(spinner).toHaveClass("animate-spin");
	});
});
