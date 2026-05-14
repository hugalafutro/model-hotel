import { useEffect, useState } from "react";

/**
 * Returns a debounced version of the input value.
 * The returned value only updates after the specified delay
 * has elapsed since the last change to the input.
 */
export function useDebounce<T>(value: T, delay: number): T {
	const [debouncedValue, setDebouncedValue] = useState(value);

	useEffect(() => {
		const timer = setTimeout(() => setDebouncedValue(value), delay);
		return () => clearTimeout(timer);
	}, [value, delay]);

	return debouncedValue;
}
