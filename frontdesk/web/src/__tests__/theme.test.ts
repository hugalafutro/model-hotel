import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	applyTheme,
	initTheme,
	readStoredTheme,
	setTheme,
	THEME_STORAGE_KEY,
} from "../theme";

describe("theme", () => {
	beforeEach(() => {
		localStorage.clear();
		document.documentElement.removeAttribute("data-theme");
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("defaults to dark when nothing is stored", () => {
		expect(readStoredTheme()).toBe("dark");
	});

	it("ignores a stored value that is not a theme", () => {
		localStorage.setItem(THEME_STORAGE_KEY, "sepia");
		expect(readStoredTheme()).toBe("dark");
	});

	it("stamps data-theme only for light, so dark stays the stylesheet default", () => {
		applyTheme("light");
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");
		applyTheme("dark");
		expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
	});

	it("keeps the browser chrome colour in step with the theme", () => {
		const meta = document.createElement("meta");
		meta.setAttribute("name", "theme-color");
		meta.setAttribute("content", "#0b0c0f");
		document.head.appendChild(meta);
		try {
			applyTheme("light");
			expect(meta.getAttribute("content")).toBe("#f6f7f9");
			applyTheme("dark");
			expect(meta.getAttribute("content")).toBe("#0b0c0f");
		} finally {
			meta.remove();
		}
	});

	it("setTheme applies and persists", () => {
		setTheme("light");
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");
		expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
	});

	it("setTheme still applies when storage refuses the write", () => {
		vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new Error("quota");
		});
		setTheme("light");
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");
	});

	it("readStoredTheme falls back to dark when storage is unreadable", () => {
		vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
			throw new Error("blocked");
		});
		expect(readStoredTheme()).toBe("dark");
	});

	it("initTheme applies the persisted choice before render", () => {
		localStorage.setItem(THEME_STORAGE_KEY, "light");
		initTheme();
		expect(document.documentElement.getAttribute("data-theme")).toBe("light");
	});
});
