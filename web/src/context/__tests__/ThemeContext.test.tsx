import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ThemeProvider, useTheme } from "../ThemeContext";

describe("ThemeContext", () => {
	it("useTheme returns default values (dark theme, clean-saas style, #546de5 accent)", () => {
		const { result } = renderHook(() => useTheme(), {
			wrapper: ThemeProvider,
		});

		expect(result.current.theme).toBe("dark");
		expect(result.current.uiStyle).toBe("clean-saas");
		// Default accent is per-theme now (clean-saas default = warm copper)
		expect(result.current.accentColor).toBe("#e0823f");
		expect(result.current.accentPresets).toBeInstanceOf(Array);
		expect(result.current.accentPresets.length).toBeGreaterThan(0);
	});

	it("setTheme changes to light and updates document.documentElement class", () => {
		const { result } = renderHook(() => useTheme(), {
			wrapper: ThemeProvider,
		});

		// Initial state
		expect(document.documentElement.classList.contains("dark")).toBe(true);
		expect(document.documentElement.classList.contains("light")).toBe(false);

		act(() => {
			result.current.setTheme("light");
		});

		expect(result.current.theme).toBe("light");
		expect(document.documentElement.classList.contains("light")).toBe(true);
		expect(document.documentElement.classList.contains("dark")).toBe(false);

		// Change back to dark
		act(() => {
			result.current.setTheme("dark");
		});

		expect(result.current.theme).toBe("dark");
		expect(document.documentElement.classList.contains("dark")).toBe(true);
		expect(document.documentElement.classList.contains("light")).toBe(false);
	});

	it("setUIStyle changes to cyber-terminal and updates data-ui-style attribute", () => {
		const { result } = renderHook(() => useTheme(), {
			wrapper: ThemeProvider,
		});

		// Initial state
		expect(document.documentElement.getAttribute("data-ui-style")).toBe(
			"clean-saas",
		);

		act(() => {
			result.current.setUIStyle("cyber-terminal");
		});

		expect(result.current.uiStyle).toBe("cyber-terminal");
		expect(document.documentElement.getAttribute("data-ui-style")).toBe(
			"cyber-terminal",
		);

		// Change to glassmorphism-lite
		act(() => {
			result.current.setUIStyle("glassmorphism-lite");
		});

		expect(result.current.uiStyle).toBe("glassmorphism-lite");
		expect(document.documentElement.getAttribute("data-ui-style")).toBe(
			"glassmorphism-lite",
		);
	});

	it("setAccentColor changes the accent color and updates CSS variables", () => {
		const { result } = renderHook(() => useTheme(), {
			wrapper: ThemeProvider,
		});

		const newColor = "#1dd1a1";

		act(() => {
			result.current.setAccentColor(newColor);
		});

		expect(result.current.accentColor).toBe(newColor);

		// Verify CSS variables were updated
		const accentVar = getComputedStyle(
			document.documentElement,
		).getPropertyValue("--accent");
		expect(accentVar).toBeTruthy();

		// Change to another color
		act(() => {
			result.current.setAccentColor("#b8860b");
		});

		expect(result.current.accentColor).toBe("#b8860b");
	});

	it("accentPresets is an array with expected values", () => {
		const { result } = renderHook(() => useTheme(), {
			wrapper: ThemeProvider,
		});

		const presets = result.current.accentPresets;

		expect(presets).toBeInstanceOf(Array);
		expect(presets.length).toBeGreaterThan(0);

		// Check that presets have the expected structure
		presets.forEach((preset) => {
			expect(preset).toHaveProperty("name");
			expect(preset).toHaveProperty("color");
			expect(preset).toHaveProperty("lightColor");
			expect(typeof preset.name).toBe("string");
			expect(typeof preset.color).toBe("string");
			expect(typeof preset.lightColor).toBe("string");
		});

		// Verify default accent color is in presets
		const defaultPreset = presets.find((p) => p.color === "#546de5");
		expect(defaultPreset).toBeDefined();
		expect(defaultPreset?.name).toBe("theme.accent.steelBlue");
	});
});
