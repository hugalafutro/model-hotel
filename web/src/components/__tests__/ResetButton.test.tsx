import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ResetButton } from "../../components/ResetButton";

describe("ResetButton", () => {
	it("renders with tooltip and aria-label", () => {
		render(<ResetButton tooltip="Reset this setting" onClick={vi.fn()} />);
		const btn = screen.getByRole("button", { name: "Reset this setting" });
		expect(btn).toBeInTheDocument();
		expect(btn).toHaveAttribute("title", "Reset this setting");
	});

	it("calls onClick when clicked", async () => {
		const onClick = vi.fn();
		const user = userEvent.setup();
		render(<ResetButton tooltip="Reset" onClick={onClick} />);
		await user.click(screen.getByRole("button", { name: "Reset" }));
		expect(onClick).toHaveBeenCalledOnce();
	});

	it("uses default size 14 when not specified", () => {
		render(<ResetButton tooltip="Reset" onClick={vi.fn()} />);
		const svg = screen
			.getByRole("button", { name: "Reset" })
			.querySelector("svg");
		expect(svg).toHaveAttribute("width", "14");
		expect(svg).toHaveAttribute("height", "14");
	});

	it("uses custom size when specified", () => {
		render(<ResetButton tooltip="Reset" onClick={vi.fn()} size={18} />);
		const svg = screen
			.getByRole("button", { name: "Reset" })
			.querySelector("svg");
		expect(svg).toHaveAttribute("width", "18");
		expect(svg).toHaveAttribute("height", "18");
	});

	it("is disabled when disabled prop is true", () => {
		render(<ResetButton tooltip="Reset" onClick={vi.fn()} disabled />);
		expect(screen.getByRole("button", { name: "Reset" })).toBeDisabled();
	});
});
