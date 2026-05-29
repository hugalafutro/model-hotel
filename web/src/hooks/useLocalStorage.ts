import { useCallback, useEffect, useRef, useState } from "react";

type SetValue<T> = (value: T | ((prev: T) => T)) => void;

interface UseLocalStorageOptions<T> {
	serialize?: (v: T) => string;
	deserialize?: (stored: string, fallback: T) => T;
	/** When false, skips reading from and writing to localStorage. Defaults to true. */
	enabled?: boolean;
}

/**
 * useState backed by localStorage.
 *
 * - Init: reads `key` from localStorage (via `deserialize`), falls back to `initialValue`.
 * - Setter: updates state AND writes to localStorage (via `serialize`).
 * - When `enabled` is false, skips both read and write.
 * - Write errors (quota exceeded) are silently ignored.
 */
export function useLocalStorage<T>(
	key: string,
	initialValue: T,
	options: UseLocalStorageOptions<T> = {},
): [T, SetValue<T>] {
	const { serialize = String, deserialize, enabled = true } = options;

	// Refs so the setter callback never goes stale
	const enabledRef = useRef(enabled);
	const serializeRef = useRef(serialize);
	useEffect(() => {
		enabledRef.current = enabled;
	}, [enabled]);
	useEffect(() => {
		serializeRef.current = serialize;
	}, [serialize]);

	const [storedValue, setStoredValue] = useState<T>(() => {
		if (!enabled) return initialValue;
		try {
			const item = localStorage.getItem(key);
			if (item === null) return initialValue;
			return deserialize ? deserialize(item, initialValue) : (item as T);
		} catch {
			return initialValue;
		}
	});

	const setValue: SetValue<T> = useCallback(
		(value) => {
			setStoredValue((prev) => {
				const nextValue =
					typeof value === "function" ? (value as (prev: T) => T)(prev) : value;
				if (enabledRef.current) {
					try {
						localStorage.setItem(key, serializeRef.current(nextValue));
						// Notify other components in the same tab that this key changed.
						window.dispatchEvent(
							new CustomEvent("localStorageChange", { detail: { key } }),
						);
					} catch {
						/* quota exceeded - silently ignore */
					}
				}
				return nextValue;
			});
		},
		[key],
	);

	return [storedValue, setValue];
}
