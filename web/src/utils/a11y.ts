import type { KeyboardEvent } from "react";

/**
 * Enter/Space activation for a clickable element that is not a button, which
 * is what a role="button" element owes the keyboard. Space is prevented so the
 * page does not scroll under the activation.
 */
export function onActivateKey(fn: (e: KeyboardEvent) => void) {
	return (e: KeyboardEvent) => {
		// A key pressed on a nested control (a copy pill inside a row) is that
		// control's to handle: taken here it would open the row and cancel the
		// button's own activation.
		if (e.target !== e.currentTarget) return;
		if (e.key === "Enter" || e.key === " ") {
			e.preventDefault();
			fn(e);
		}
	};
}

/**
 * moveOptionFocus is the keyboard model of a listbox whose options are real
 * focusable elements: ArrowDown/ArrowUp step through them, Home/End jump to
 * the ends, and a first ArrowDown from outside the list (the trigger, a search
 * box) enters it. Returns whether the key was one it handled, so the caller
 * can preventDefault. Home and End are left to a text field that has focus,
 * where they move the caret.
 */
export function moveOptionFocus(
	list: HTMLElement | null,
	key: string,
	current: Element | null,
): boolean {
	if (!list) return false;
	const options = Array.from(
		list.querySelectorAll<HTMLElement>('[role="option"]:not([disabled])'),
	);
	if (options.length === 0) return false;
	const inText =
		current instanceof HTMLInputElement ||
		current instanceof HTMLTextAreaElement;
	// biome-ignore lint/complexity/useIndexOf: current is Element | null, indexOf would need a cast
	const idx = options.findIndex((o) => o === current);
	let next: number;
	switch (key) {
		case "ArrowDown":
			next = idx < 0 ? 0 : Math.min(idx + 1, options.length - 1);
			break;
		case "ArrowUp":
			next = idx < 0 ? options.length - 1 : Math.max(idx - 1, 0);
			break;
		case "Home":
			if (inText) return false;
			next = 0;
			break;
		case "End":
			if (inText) return false;
			next = options.length - 1;
			break;
		default:
			return false;
	}
	options[next]?.focus();
	return true;
}
