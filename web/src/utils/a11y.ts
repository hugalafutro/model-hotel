import type { KeyboardEvent } from "react";

/**
 * Enter/Space activation for a clickable element that is not a button, which
 * is what a role="button" element owes the keyboard. Space is prevented so the
 * page does not scroll under the activation.
 */
export function onActivateKey(fn: (e: KeyboardEvent) => void) {
	return (e: KeyboardEvent) => {
		if (e.key === "Enter" || e.key === " ") {
			e.preventDefault();
			fn(e);
		}
	};
}
