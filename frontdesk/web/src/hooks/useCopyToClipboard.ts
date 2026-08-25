import { useCallback, useEffect, useRef, useState } from "react";

interface UseCopyToClipboardOptions {
	/** How long `copied` stays true after a successful write. Defaults to 2000ms. */
	resetAfterMs?: number;
	/**
	 * When false, `copied` stays false and no reset timer is scheduled, for
	 * callers that report the result some other way (a toast) and never render
	 * the flag. Defaults to true.
	 */
	trackCopied?: boolean;
}

interface UseCopyToClipboard {
	/** Writes `text` to the clipboard. Resolves true on success, false on failure. */
	copy: (text: string) => Promise<boolean>;
	/** True from a successful copy until `resetAfterMs` later. */
	copied: boolean;
}

/**
 * A clipboard write plus the "Copied" flag that goes with it.
 *
 * - `copy(text)` resolves false instead of throwing when the clipboard is
 *   missing or refuses, so callers that toast a failure still can. The write
 *   runs inside the async body on purpose: `navigator.clipboard` is undefined in
 *   non-secure (plain HTTP) contexts, and that turns a synchronous throw into a
 *   rejection the catch below sees.
 * - `copied` flips true on success and reverts on a timer cleared on a re-copy
 *   and on unmount. An unmount while the write is still in flight ends the copy
 *   there: the text reaches the clipboard, but nothing is flagged and no timer
 *   is scheduled behind the cleanup that would have cleared it.
 */
export function useCopyToClipboard(
	options: UseCopyToClipboardOptions = {},
): UseCopyToClipboard {
	const { resetAfterMs = 2000, trackCopied = true } = options;
	const [copied, setCopied] = useState(false);

	const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
	// False from unmount onwards, so work resumed after an awaited write knows
	// the component it would touch is gone.
	const alive = useRef(true);
	useEffect(() => {
		alive.current = true;
		return () => {
			alive.current = false;
			if (timer.current !== null) clearTimeout(timer.current);
		};
	}, []);

	const copy = useCallback(
		async (text: string): Promise<boolean> => {
			try {
				await navigator.clipboard.writeText(text);
			} catch {
				return false;
			}
			if (!alive.current || !trackCopied) return true;
			setCopied(true);
			if (timer.current !== null) clearTimeout(timer.current);
			// The handle is dropped as the timer fires, so neither the unmount
			// cleanup nor the next copy clears an id that has already expired.
			timer.current = setTimeout(() => {
				timer.current = null;
				setCopied(false);
			}, resetAfterMs);
			return true;
		},
		[resetAfterMs, trackCopied],
	);

	return { copy, copied };
}
