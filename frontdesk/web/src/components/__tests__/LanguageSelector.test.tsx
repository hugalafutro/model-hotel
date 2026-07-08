import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18next from "i18next";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LANGUAGE_STORAGE_KEY } from "../../i18n";
import { LanguageSelector } from "../LanguageSelector";

describe("LanguageSelector", () => {
	beforeEach(() => {
		localStorage.clear();
		// jsdom does not implement scrollIntoView; stub it so the dropdown-open
		// effect (which scrolls the active option into view) does not throw.
		Element.prototype.scrollIntoView = vi.fn();
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("renders a button labelled 'Language'", () => {
		render(<LanguageSelector />);
		expect(
			screen.getByRole("button", { name: "Language" }),
		).toBeInTheDocument();
	});

	it("hides the dropdown until the trigger is clicked", () => {
		render(<LanguageSelector />);
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});

	it("opens a dropdown with all 11 languages on click", async () => {
		const user = userEvent.setup();
		render(<LanguageSelector />);
		await user.click(screen.getByRole("button", { name: "Language" }));

		expect(screen.getByRole("listbox")).toBeInTheDocument();
		expect(screen.getAllByRole("option")).toHaveLength(11);
		// Autonyms appear in their own scripts (spot-check across scripts).
		expect(screen.getByRole("option", { name: "Čeština" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "Deutsch" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "日本語" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "中文" })).toBeInTheDocument();
		// English is intentionally last so it sits nearest the trigger.
		expect(screen.getByRole("option", { name: "English" })).toBeInTheDocument();
	});

	it("marks the active language (English) as selected and pins it to the top", async () => {
		const user = userEvent.setup();
		render(<LanguageSelector />);
		await user.click(screen.getByRole("button", { name: "Language" }));

		const options = screen.getAllByRole("option");
		const en = screen.getByRole("option", { name: "English" });
		expect(en).toHaveAttribute("aria-selected", "true");
		// The dropdown opens downward, so the active language sits at the top.
		expect(options[0]).toBe(en);
	});

	it("switches language, persists the choice, and closes the dropdown", async () => {
		// Mock changeLanguage so the test does not trigger an async lazy locale
		// load (the lazy backend's dynamic import is not relevant here).
		const changeSpy = vi
			.spyOn(i18next, "changeLanguage")
			.mockImplementation(() => Promise.resolve(i18next.t));
		// The component verifies the catalog loaded before persisting.
		vi.spyOn(i18next, "getResourceBundle").mockReturnValue({
			common: { cancel: "Abbrechen" },
		});
		const user = userEvent.setup();
		render(<LanguageSelector />);

		await user.click(screen.getByRole("button", { name: "Language" }));
		await user.click(screen.getByRole("option", { name: "Deutsch" }));

		expect(changeSpy).toHaveBeenCalledWith("de");
		// Persistence happens in the .then() after the catalog loads.
		await waitFor(() => {
			expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("de");
		});
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});

	it("does not persist when the catalog falls back silently", async () => {
		// partialBundledLanguages: changeLanguage resolves even if the lazy
		// catalog failed — i18next falls back to English. The component must
		// detect this (no resource bundle) and skip persisting.
		vi.spyOn(i18next, "changeLanguage").mockResolvedValue(i18next.t);
		vi.spyOn(i18next, "getResourceBundle").mockReturnValue(undefined);
		const user = userEvent.setup();
		render(<LanguageSelector />);

		await user.click(screen.getByRole("button", { name: "Language" }));
		await user.click(screen.getByRole("option", { name: "Deutsch" }));

		await waitFor(() => {
			expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
		});
		expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
	});

	it("does not persist when changeLanguage rejects", async () => {
		vi.spyOn(i18next, "changeLanguage").mockRejectedValue(
			new Error("network error"),
		);
		const user = userEvent.setup();
		render(<LanguageSelector />);

		await user.click(screen.getByRole("button", { name: "Language" }));
		await user.click(screen.getByRole("option", { name: "Deutsch" }));

		await waitFor(() => {
			expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
		});
		expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
	});

	it("closes the dropdown when clicking outside", async () => {
		const user = userEvent.setup();
		render(
			<div>
				<h1>Elsewhere</h1>
				<LanguageSelector />
			</div>,
		);

		await user.click(screen.getByRole("button", { name: "Language" }));
		expect(screen.getByRole("listbox")).toBeInTheDocument();

		// Click a sibling outside the selector's ref boundary.
		await user.click(screen.getByRole("heading", { name: "Elsewhere" }));
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});

	it("moves focus to the active option when the dropdown opens", async () => {
		const user = userEvent.setup();
		render(<LanguageSelector />);
		await user.click(screen.getByRole("button", { name: "Language" }));

		// English is active and pinned to the top; it receives keyboard focus.
		expect(screen.getByRole("option", { name: "English" })).toHaveFocus();
	});

	it("navigates options with arrow keys", async () => {
		const user = userEvent.setup();
		render(<LanguageSelector />);
		await user.click(screen.getByRole("button", { name: "Language" }));

		const options = screen.getAllByRole("option");
		expect(options[0]).toHaveFocus();

		await user.keyboard("{ArrowDown}");
		expect(options[1]).toHaveFocus();
		await user.keyboard("{ArrowDown}");
		expect(options[2]).toHaveFocus();
		// ArrowUp walks back up.
		await user.keyboard("{ArrowUp}");
		expect(options[1]).toHaveFocus();
	});

	it("closes on Escape and returns focus to the trigger", async () => {
		const user = userEvent.setup();
		render(<LanguageSelector />);
		const trigger = screen.getByRole("button", { name: "Language" });

		await user.click(trigger);
		expect(screen.getByRole("listbox")).toBeInTheDocument();

		await user.keyboard("{Escape}");
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
		expect(trigger).toHaveFocus();
	});

	it("closes on Tab so the menu does not linger with focus elsewhere", async () => {
		const user = userEvent.setup();
		render(
			<div>
				<LanguageSelector />
				<button type="button">Next focusable</button>
			</div>,
		);

		await user.click(screen.getByRole("button", { name: "Language" }));
		expect(screen.getByRole("listbox")).toBeInTheDocument();

		await user.tab();
		expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
	});
});
