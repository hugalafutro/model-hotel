/**
 * Grows a textarea to fit its content. The height is cleared first so the
 * element can shrink again: scrollHeight never reports less than the height
 * already set.
 */
export function autoExpandTextarea(el: HTMLTextAreaElement): void {
	el.style.height = "auto";
	el.style.height = `${el.scrollHeight}px`;
}
