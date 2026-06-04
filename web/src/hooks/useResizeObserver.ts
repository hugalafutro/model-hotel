import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Tracks the dimensions of a DOM element using ResizeObserver.
 * Returns a ref to attach to the target element and its current width/height.
 *
 * The observer re-subscribes automatically when the element changes
 * between renders (i.e. the component unmounts the old element and
 * mounts a new one, causing `observedEl` state to update). However,
 * if `ref.current` is swapped in-place without a re-render (e.g.
 * conditional rendering into the same ref mid-lifecycle), the
 * observer will continue watching the old element until the next render
 * triggers the sync effect. For stable-element consumers like
 * FuseOutline, this is not an issue.
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
	}, [observedEl]);

	useEffect(() => {
		if (!observedEl) return;

		compute();
		const ro = new ResizeObserver(compute);
		ro.observe(observedEl);
		return () => ro.disconnect();
	}, [observedEl, compute]);

	return { ref, ...size };
}
