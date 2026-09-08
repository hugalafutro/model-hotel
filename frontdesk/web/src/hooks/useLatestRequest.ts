import { useMemo, useRef } from "react";

// useLatestRequest keeps a fetch that can overlap itself honest. An SSE event, a
// fallback poll, a filter change and a manual refresh can all have a request in
// flight at once, and the response that lands last is not necessarily the answer
// to the newest question: without a guard a slower earlier read can overwrite a
// newer one and put a stale snapshot back on screen. Each read takes a ticket
// with `next()` and applies its result only while `isCurrent` still holds it.
// Calling `next()` without reading (on unmount, say) invalidates everything
// still in flight, so nothing sets state on a dead tree.
//
// The returned object is stable for the life of the component, so it is safe in
// a useCallback/useEffect dependency list.
export function useLatestRequest(): {
	next: () => number;
	isCurrent: (seq: number) => boolean;
} {
	const seqRef = useRef(0);
	return useMemo(
		() => ({
			next: () => ++seqRef.current,
			isCurrent: (seq: number) => seq === seqRef.current,
		}),
		[],
	);
}
