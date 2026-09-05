import type { FocusEvent } from "react";

/**
 * True when a blur was forced by the control becoming disabled while it had
 * focus: an enclosing fieldset going fleet-managed, or the control's own
 * disabled flag flipping. A commit-on-blur handler must not write in that
 * case: the key just became fleet-owned, and the draft belongs to nobody.
 */
export function isForcedBlur(e: FocusEvent<HTMLElement>): boolean {
	return e.currentTarget.matches(":disabled");
}
