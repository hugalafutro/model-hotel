import { type RefObject, useEffect, useRef } from "react";

interface ClickOutsideOptions {
	/** When false the listener is not attached. Defaults to true. */
	enabled?: boolean;
	/**
	 * An element whose clicks do not count as outside, resolved at click time.
	 * A trigger that toggles the panel needs this: without it the click that
	 * closes the panel is followed by the trigger reopening it.
	 */
	ignore?: () => Element | null;
}

/**
 * Calls `onOutside` when a mousedown lands outside `ref`. Mousedown rather than
 * click, so the panel closes before a selection inside another control starts.
 */
export function useClickOutside(
	ref: RefObject<HTMLElement | null>,
	onOutside: () => void,
	{ enabled = true, ignore }: ClickOutsideOptions = {},
): void {
	// Held in refs so an inline handler or ignore closure does not re-attach the
	// listener on every render.
	const onOutsideRef = useRef(onOutside);
	const ignoreRef = useRef(ignore);
	useEffect(() => {
		onOutsideRef.current = onOutside;
		ignoreRef.current = ignore;
	});

	useEffect(() => {
		if (!enabled) return;
		const handle = (e: MouseEvent) => {
			const target = e.target as Node;
			if (!ref.current || ref.current.contains(target)) return;
			if (ignoreRef.current?.()?.contains(target)) return;
			onOutsideRef.current();
		};
		document.addEventListener("mousedown", handle);
		return () => document.removeEventListener("mousedown", handle);
	}, [enabled, ref]);
}
