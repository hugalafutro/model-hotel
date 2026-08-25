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

interface UseLocalStorageValueOptions<T> {
	/** Turns the raw stored string (null when the key is absent) into the value. */
	deserialize?: (stored: string | null, fallback: T) => T;
	/**
	 * Extra window events that also announce a change to this key, for writers
	 * that dispatch their own (e.g. "sidebarQuotaToggle").
	 */
	events?: string[];
}

/** Current value of `key`, or `fallback` when it is absent or unreadable. */
function readStored<T>(
	key: string,
	fallback: T,
	deserialize?: (stored: string | null, fallback: T) => T,
): T {
	try {
		const item = localStorage.getItem(key);
		if (deserialize) return deserialize(item, fallback);
		return item === null ? fallback : (item as T);
	} catch {
		// localStorage unavailable (private browsing, quota, iframe)
		return fallback;
	}
}

/**
 * Read-only view of a localStorage key another screen owns and writes.
 *
 * Returns the current value and re-reads it whenever the key changes: a
 * cross-tab `storage` event, the `localStorageChange` this module dispatches on
 * every write, or any extra event name the writer announces. It never writes,
 * so it cannot re-enter the listener a write-through setter would trigger.
 */
export function useLocalStorageValue<T>(
	key: string,
	fallback: T,
	options: UseLocalStorageValueOptions<T> = {},
): T {
	const { deserialize, events } = options;

	// Refs so the listener below re-reads with the current deserializer and
	// fallback without resubscribing when the caller passes them inline.
	const deserializeRef = useRef(deserialize);
	const fallbackRef = useRef(fallback);
	useEffect(() => {
		deserializeRef.current = deserialize;
		fallbackRef.current = fallback;
	}, [deserialize, fallback]);

	const [value, setValue] = useState<T>(() =>
		readStored(key, fallback, deserialize),
	);

	// Joined so an inline array literal does not resubscribe on every render.
	const extraEvents = events?.join(" ") ?? "";
	useEffect(() => {
		const handler = (e: Event) => {
			// The custom event names the key that changed; ignore the others.
			if (
				e.type === "localStorageChange" &&
				(e as CustomEvent).detail?.key !== key
			) {
				return;
			}
			// A cross-tab storage event carries its key, or null for a clear().
			if (e instanceof StorageEvent && e.key !== null && e.key !== key) {
				return;
			}
			setValue(readStored(key, fallbackRef.current, deserializeRef.current));
		};
		const names = [
			"storage",
			"localStorageChange",
			...(extraEvents === "" ? [] : extraEvents.split(" ")),
		];
		for (const name of names) window.addEventListener(name, handler);
		return () => {
			for (const name of names) window.removeEventListener(name, handler);
		};
	}, [key, extraEvents]);

	return value;
}
