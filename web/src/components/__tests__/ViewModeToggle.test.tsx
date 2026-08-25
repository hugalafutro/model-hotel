import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ViewModeToggle } from "../ViewModeToggle";

const TOOLTIP = "Click to toggle between pagination and infinite scrolling.";

describe("ViewModeToggle", () => {
	it("shows the infinite-scroll glyph in scroll mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button", {
			name: "Switch to pagination mode",
		});
		expect(button.querySelector(".icon-infinite-scroll")).toBeInTheDocument();
		expect(button.querySelector(".icon-pages")).not.toBeInTheDocument();
	});

	it("shows the pages glyph in paginate mode", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button", {
			name: "Switch to scroll mode",
		});
		expect(button.querySelector(".icon-pages")).toBeInTheDocument();
		expect(
			button.querySelector(".icon-infinite-scroll"),
		).not.toBeInTheDocument();
	});

	it("carries no visible text: the action label is aria only", () => {
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={vi.fn()} />,
		);
		const button = screen.getByRole("button");
		expect(button.textContent).toBe("");
		expect(button.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
	});

	it.each(["paginate", "scroll"] as const)(
		"uses the same tooltip in %s mode",
		(mode) => {
			renderWithProviders(
				<ViewModeToggle viewMode={mode} onChange={vi.fn()} />,
			);
			expect(screen.getByRole("button")).toHaveAttribute("title", TOOLTIP);
		},
	);

	it.each(["paginate", "scroll"] as const)(
		"is accent-styled in %s mode (either-or, no off state)",
		(mode) => {
			renderWithProviders(
				<ViewModeToggle viewMode={mode} onChange={vi.fn()} />,
			);
			expect(screen.getByRole("button")).toHaveClass("ui-btn-primary");
		},
	);

	it("calls onChange with 'scroll' when in paginate mode", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		renderWithProviders(
			<ViewModeToggle viewMode="paginate" onChange={onChange} />,
		);
		await user.click(screen.getByRole("button"));
		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("scroll");
	});

	it("calls onChange with 'paginate' when in scroll mode", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		renderWithProviders(
			<ViewModeToggle viewMode="scroll" onChange={onChange} />,
		);
		await user.click(screen.getByRole("button"));
		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("paginate");
	});
});
