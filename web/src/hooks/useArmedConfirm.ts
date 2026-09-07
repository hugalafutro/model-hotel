import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Two-step confirmation for a destructive button: the first press arms it, the
 * second within `ms` commits. Only one key is armed at a time, so arming a
 * second row disarms the first, and the timer is cleared on unmount.
 */
export function useArmedConfirm<T>(ms = 3000): {
	armed: T | null;
	fire: (key: T, commit: () => void) => void;
	disarm: () => void;
} {
	const [armed, setArmed] = useState<T | null>(null);
	const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

	const clear = useCallback(() => {
		if (timer.current !== null) {
			clearTimeout(timer.current);
			timer.current = null;
		}
	}, []);

	useEffect(() => clear, [clear]);

	const disarm = useCallback(() => {
		clear();
		setArmed(null);
	}, [clear]);

	const fire = useCallback(
		(key: T, commit: () => void) => {
			clear();
			if (armed === key) {
				setArmed(null);
				commit();
				return;
			}
			setArmed(key);
			timer.current = setTimeout(() => setArmed(null), ms);
		},
		[armed, clear, ms],
	);

	return { armed, fire, disarm };
}
