import {
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
	useSyncExternalStore,
} from "react";

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

/**
 * Read-only view of a localStorage key another screen owns and writes.
 *
 * The key is treated as the external store React subscribes to: `subscribe`
 * attaches the listeners that announce a change to it, and the snapshot is the
 * RAW stored string, so React's Object.is check bails out of a re-render when an
 * announcement leaves the value alone. The parsed value is derived from that
 * snapshot, which is what makes a rerender with a different `key`, `fallback` or
 * deserializer show the new reading in that same render. It never writes, so it
 * cannot re-enter the listener a write-through setter would trigger.
 */
export function useLocalStorageValue<T>(
	key: string,
	fallback: T,
	options: UseLocalStorageValueOptions<T> = {},
): T {
	const { deserialize, events } = options;

	// The names ride as JSON so an inline array literal does not resubscribe on
	// every render, and so a name is restored exactly as given, spaces included.
	const extraEvents = JSON.stringify(events ?? []);

	const subscribe = useCallback(
		(onStoreChange: () => void) => {
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
				onStoreChange();
			};
			const names = [
				"storage",
				"localStorageChange",
				...(JSON.parse(extraEvents) as string[]),
			];
			for (const name of names) window.addEventListener(name, handler);
			return () => {
				for (const name of names) window.removeEventListener(name, handler);
			};
		},
		[key, extraEvents],
	);

	// A string or null, never a fresh object, so React can compare snapshots.
	const getSnapshot = useCallback((): string | null => {
		try {
			return localStorage.getItem(key);
		} catch {
			// localStorage unavailable (private browsing, quota, iframe): the key
			// reads as absent, which lands on the fallback below.
			return null;
		}
	}, [key]);

	// A server render has no localStorage, so the key is absent there too.
	const getServerSnapshot = useCallback((): string | null => null, []);

	const raw = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

	return useMemo(() => {
		try {
			if (deserialize) return deserialize(raw, fallback);
			return raw === null ? fallback : (raw as T);
		} catch {
			// A deserializer that chokes on what is stored still owes a value.
			return fallback;
		}
	}, [raw, fallback, deserialize]);
}
