import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Tracks the dimensions of a DOM element using ResizeObserver.
 * Returns a ref to attach to the target element and its current width/height.
 */
export function useResizeObserver<
	T extends HTMLElement | SVGElement = HTMLElement,
>() {
	const ref = useRef<T | null>(null);
	const [size, setSize] = useState({ width: 0, height: 0 });

	const compute = useCallback(() => {
		if (ref.current) {
			const { width, height } = ref.current.getBoundingClientRect();
			setSize({ width, height });
		}
	}, []);

	useEffect(() => {
		const el = ref.current;
		if (!el) return;

		compute();
		const ro = new ResizeObserver(compute);
		ro.observe(el);
		return () => ro.disconnect();
	}, [compute]);

	return { ref, ...size };
}
