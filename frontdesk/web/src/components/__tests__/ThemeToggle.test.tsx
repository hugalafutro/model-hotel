import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { THEME_STORAGE_KEY } from "../../theme";
import { ThemeToggle } from "../ThemeToggle";

describe("ThemeToggle", () => {
	beforeEach(() => {
		localStorage.clear();
		document.documentElement.removeAttribute("data-theme");
	});

	it("offers the light theme while dark is active", () => {
		render(<ThemeToggle />);
		expect(screen.getByTestId("theme-toggle")).toHaveAccessibleName(
			"Switch to light theme",
		);
	});

	it("flips to light on click, persists it, and then offers dark", async () => {
		const user = userEvent.setup();
		render(<ThemeToggle />);
		await user.click(screen.getByTestId("theme-toggle"));
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");
		expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
		expect(screen.getByTestId("theme-toggle")).toHaveAccessibleName(
			"Switch to dark theme",
		);
	});

	it("flips back to dark and clears the attribute", async () => {
		localStorage.setItem(THEME_STORAGE_KEY, "light");
		document.documentElement.setAttribute("data-theme", "light");
		const user = userEvent.setup();
		render(<ThemeToggle />);
		expect(screen.getByTestId("theme-toggle")).toHaveAccessibleName(
			"Switch to dark theme",
		);
		await user.click(screen.getByTestId("theme-toggle"));
		expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
		expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
	});
});
