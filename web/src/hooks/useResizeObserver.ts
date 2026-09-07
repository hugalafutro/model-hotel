import { useCallback, useEffect, useState } from "react";

/**
 * Tracks the dimensions of a DOM element with a ResizeObserver.
 *
 * `ref` is a callback ref, so React reports the element the moment it is
 * attached or swapped, and the observer follows it: an element replaced in
 * place is picked up without waiting for another render. `el` is that element,
 * for consumers that need the node itself (FuseOutline measures its parent).
 */
export function useResizeObserver<
	T extends HTMLElement | SVGElement = HTMLElement,
>() {
	const [el, setEl] = useState<T | null>(null);
	const ref = useCallback((node: T | null) => setEl(node), []);
	const [size, setSize] = useState({ width: 0, height: 0 });

	useEffect(() => {
		if (!el) return;
		const compute = () => {
			const { width, height } = el.getBoundingClientRect();
			setSize({ width, height });
		};
		compute();
		const ro = new ResizeObserver(compute);
		ro.observe(el);
		return () => ro.disconnect();
	}, [el]);

	return { ref, el, ...size };
}
