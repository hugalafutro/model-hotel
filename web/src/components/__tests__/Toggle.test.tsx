import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { Toggle } from "../Toggle";

describe("Toggle", () => {
	it("renders unchecked", () => {
		const onChange = vi.fn();
		renderWithProviders(<Toggle checked={false} onChange={onChange} />);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeInTheDocument();
		expect(toggle).toHaveAttribute("aria-checked", "false");
		expect(toggle).toHaveClass("bg-gray-600");
	});

	it("renders checked", () => {
		const onChange = vi.fn();
		renderWithProviders(<Toggle checked={true} onChange={onChange} />);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeInTheDocument();
		expect(toggle).toHaveAttribute("aria-checked", "true");
		expect(toggle).toHaveClass("bg-(--accent)");
	});

	it("calls onChange when clicked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(<Toggle checked={false} onChange={onChange} />);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith(true);
	});

	it("calls onChange with false when toggling from checked", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(<Toggle checked={true} onChange={onChange} />);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith(false);
	});

	it("renders disabled state", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} disabled />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toBeDisabled();
		expect(toggle).toHaveClass("cursor-not-allowed");
		expect(toggle).toHaveClass("opacity-50");
	});

	it("does not call onChange when disabled", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} disabled />,
		);

		const toggle = screen.getByRole("switch");
		await user.click(toggle);

		expect(onChange).not.toHaveBeenCalled();
	});

	it("renders with aria-label", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} ariaLabel="Enable feature" />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveAttribute("aria-label", "Enable feature");
	});

	it("renders small size", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} size="sm" />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveClass("h-4");
		expect(toggle).toHaveClass("w-7");
	});

	it("renders default size", () => {
		const onChange = vi.fn();
		renderWithProviders(<Toggle checked={false} onChange={onChange} />);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveClass("h-6");
		expect(toggle).toHaveClass("w-11");
	});

	it("renders with focus ring when showFocusRing is true", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} showFocusRing />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveClass("focus:ring-2");
		expect(toggle).toHaveClass("focus:ring-(--accent)");
	});

	it("renders with custom className", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<Toggle checked={false} onChange={onChange} className="custom-toggle" />,
		);

		const toggle = screen.getByRole("switch");
		expect(toggle).toHaveClass("custom-toggle");
	});
});
