import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { ViewModeToggle } from "../../logs/ViewModeToggle";

describe("ViewModeToggle", () => {
	it("shows '⇊ Scroll' when in paginate mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={vi.fn()} />,
		);
		expect(screen.getByText("⇊ Scroll")).toBeInTheDocument();
	});

	it("shows '⬡ Pages' when in scroll mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		expect(screen.getByText("⬡ Pages")).toBeInTheDocument();
	});

	it("has correct aria-label when in paginate mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("aria-label", "Switch to scroll mode");
	});

	it("has correct aria-label when in scroll mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("aria-label", "Switch to pagination mode");
	});

	it("calls onChange with 'scroll' when in paginate mode", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={onChange} />,
		);

		const button = screen.getByRole("button");
		await user.click(button);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("scroll");
	});

	it("calls onChange with 'paginate' when in scroll mode", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={onChange} />,
		);

		const button = screen.getByRole("button");
		await user.click(button);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("paginate");
	});

	it("has correct title when in paginate mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Switch to scroll mode");
	});

	it("has correct title when in scroll mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Switch to pagination mode");
	});

	it("has border styling in paginate mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveClass("border-gray-700");
		expect(button).toHaveClass("text-gray-400");
	});

	it("has accent styling in scroll mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveClass("bg-(--accent)/20");
		expect(button).toHaveClass("text-(--accent)");
		expect(button).toHaveClass("border-(--accent)/40");
	});
});
