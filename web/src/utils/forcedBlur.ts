import type { FocusEvent } from "react";

/**
 * True when a blur was forced by the control becoming disabled, typically an
 * enclosing fieldset going fleet-managed while the control had focus. A
 * commit-on-blur handler must not write in that case: the key just became
 * fleet-owned, and the draft belongs to nobody.
 */
export function isForcedBlur(e: FocusEvent<HTMLElement>): boolean {
	return e.currentTarget.matches(":disabled");
}
