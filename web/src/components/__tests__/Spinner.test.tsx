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

	it("clears interval on unmount", () => {
		const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
		const { unmount } = render(<Spinner />, { wrapper: AllProviders });

		unmount();

		expect(clearIntervalSpy).toHaveBeenCalled();
		clearIntervalSpy.mockRestore();
	});
});

describe("cyber-terminal mode", () => {
	beforeEach(() => {
		vi.useFakeTimers();
		localStorage.setItem("uiStyle", "cyber-terminal");
	});

	afterEach(() => {
		vi.useRealTimers();
		localStorage.removeItem("uiStyle");
	});

	it("renders braille character instead of spinning circle", () => {
		render(<Spinner />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");
		expect(spinner).toBeInTheDocument();
		expect(spinner).toHaveTextContent("⠋");
		expect(spinner).not.toHaveClass("animate-spin");
		expect(spinner).toHaveClass("w-[1ch]");
	});

	it("displays braille character that changes over time", () => {
		render(<Spinner />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");
		const initialText = spinner.textContent;

		act(() => {
			vi.advanceTimersByTime(80);
		});

		expect(spinner.textContent).not.toBe(initialText);
	});

	it("cycles through all braille characters", () => {
		render(<Spinner />, { wrapper: AllProviders });
		const spinner = screen.getByTestId("spinner");
		const initialText = spinner.textContent;

		// Advance through all 10 frames (80ms * 10 = 800ms)
		act(() => {
			vi.advanceTimersByTime(800);
		});

		// Should return to starting character
		expect(spinner.textContent).toBe(initialText);
	});
});
