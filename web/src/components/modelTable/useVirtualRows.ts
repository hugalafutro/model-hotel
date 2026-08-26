import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useLayoutEffect, useRef, useState } from "react";

const EDGE_THRESHOLD_PX = 500;

/**
 * The virtual window over a bidirectionally-fetched list: the virtualizer,
 * the scroll-position correction when rows are prepended, the table padding
 * that stands in for the unmounted rows, the edge-triggered fetches, and the
 * visible index range for the footer.
 */
export function useVirtualRows<T extends { id: string }>({
	entries,
	hasBefore,
	hasAfter,
	isLoadingBefore,
	isLoadingAfter,
	fetchNewer,
	fetchOlder,
}: {
	entries: T[];
	hasBefore: boolean;
	hasAfter: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	fetchNewer: () => void;
	fetchOlder: () => void;
}) {
	"use no memo";
	// TanStack Virtual hands back mutable functions and a measurements cache
	// that changes identity under the compiler's feet; memoizing around them
	// silently disables the prepend correction below.
	//
	// The scroll element lives in state behind a callback ref rather than in a
	// ref: the virtualizer and the handlers below read it during render and in
	// callbacks, and a ref read there is exactly what react-hooks/refs forbids.
	const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
	// eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual returns mutable functions; compiler skips memoization
	const virtualizer = useVirtualizer({
		count: entries.length,
		// Measurements follow the ROW, not its index: after a prepend every row
		// shifts to a new index, and index-keyed measurements would describe the
		// old rows' heights under the new rows' slots, which is exactly the data
		// the prepend correction below reads.
		getItemKey: (index) => entries[index]?.id ?? index,
		getScrollElement: () => scrollEl,
		estimateSize: () => 45,
		overscan: 20,
	});

	const virtualItems = virtualizer.getVirtualItems();

	const prevEntriesRef = useRef(entries);
	// State counter to force synchronous re-render after scrollTop adjustment.
	// React guarantees setState inside useLayoutEffect is flushed before paint.
	const [, forceRerender] = useState(0);

	// When items are prepended (fetchNewer), all item indices shift but
	// scrollTop stays the same, so the virtualizer maps the old scroll
	// position to different items. Push scrollTop down by the height the new
	// rows occupy: the start offset of the row that used to be first, which
	// the virtualizer computes from measured sizes where it has them (rows are
	// keyed by id, so a measurement survives the shift) and estimateSize where
	// it does not. Read measurementsCache BY INDEX: it is
	// a lazy view, not an array, so Array.prototype iteration (reduce, slice)
	// over it sees nothing and an averaged size came out as 0. Then force a
	// synchronous re-render so the virtualizer recomputes before the browser
	// paints.
	useLayoutEffect(() => {
		const prev = prevEntriesRef.current;
		if (entries.length > prev.length && prev.length > 0) {
			const newItemCount = entries.length - prev.length;
			if (entries[newItemCount]?.id === prev[0]?.id && scrollEl) {
				// The rows committed in this same pass were measured by their ref
				// callbacks a moment ago; getVirtualItems() folds those sizes into
				// the measurements before they are read.
				virtualizer.getVirtualItems();
				const cache = virtualizer.measurementsCache;
				const first = cache[0];
				const oldFirst = cache[newItemCount];
				const added =
					first && oldFirst ? oldFirst.start - first.start : newItemCount * 45;
				scrollEl.scrollTop += added;
				prevEntriesRef.current = entries;
				forceRerender((c) => c + 1);
				return;
			}
		}
		prevEntriesRef.current = entries;
	}, [entries, virtualizer, scrollEl]);

	const [paddingTop, paddingBottom] =
		virtualItems.length > 0
			? [
					Math.max(0, virtualItems[0].start),
					Math.max(
						0,
						virtualizer.getTotalSize() -
							virtualItems[virtualItems.length - 1].end,
					),
				]
			: [0, 0];

	const handleScroll = useCallback(() => {
		const el = scrollEl;
		if (!el) return;

		const nearTop = el.scrollTop < EDGE_THRESHOLD_PX;
		const nearBottom =
			el.scrollHeight - el.scrollTop - el.clientHeight < EDGE_THRESHOLD_PX;

		if (nearTop && hasBefore && !isLoadingBefore) {
			fetchNewer();
		}
		if (nearBottom && hasAfter && !isLoadingAfter) {
			fetchOlder();
		}
	}, [
		scrollEl,
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer,
		fetchOlder,
	]);

	const startIndex = virtualItems.length > 0 ? virtualItems[0].index + 1 : 0;
	const endIndex =
		virtualItems.length > 0
			? virtualItems[virtualItems.length - 1].index + 1
			: 0;

	return {
		/** Attach to the scroller: `ref={scrollRef}`. */
		scrollRef: setScrollEl,
		virtualizer,
		virtualItems,
		paddingTop,
		paddingBottom,
		handleScroll,
		startIndex,
		endIndex,
	};
}
