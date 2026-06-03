import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Tracks the dimensions of a DOM element using ResizeObserver.
 * Returns a ref to attach to the target element and its current width/height.
 *
 * If the consumer swaps which DOM element the ref is attached to, the observer
 * automatically re-subscribes to the new element.
 */
export function useResizeObserver<
	T extends HTMLElement | SVGElement = HTMLElement,
>() {
	const ref = useRef<T | null>(null);
	const [size, setSize] = useState({ width: 0, height: 0 });
	const [observedEl, setObservedEl] = useState<T | null>(null);

	const compute = useCallback(() => {
		if (ref.current) {
			const { width, height } = ref.current.getBoundingClientRect();
			setSize({ width, height });
		}
	}, []);

	// Sync observed element state so the effect re-runs when ref.current changes
	useEffect(() => {
		if (ref.current !== observedEl) {
			setObservedEl(ref.current);
		}
	});

	useEffect(() => {
		if (!observedEl) return;

		compute();
		const ro = new ResizeObserver(compute);
		ro.observe(observedEl);
		return () => ro.disconnect();
	}, [observedEl, compute]);

	return { ref, ...size };
}
