import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useLayoutEffect, useRef, useState } from "react";

const EDGE_THRESHOLD_PX = 500;

/**
 * The virtual window over a bidirectionally-fetched list: the virtualizer,
 * the scroll-position correction when rows are prepended, the table padding
 * that stands in for the unmounted rows, the edge-triggered fetches, and the
 * visible index range for the footer.
 */
export function useVirtualRows<T extends { id?: string }>({
	entries,
	listVersion,
	hasBefore,
	hasAfter,
	isLoadingBefore,
	isLoadingAfter,
	fetchNewer,
	fetchOlder,
	estimateSize = 45,
	getItemKey,
	pinTop = false,
}: {
	entries: T[];
	/**
	 * Changes whenever the rows are replaced wholesale (useBidirectionalFetch's
	 * listVersion, or any other token of which list is shown): a change
	 * scrolls back to the top.
	 */
	listVersion: number | string;
	hasBefore: boolean;
	hasAfter: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	fetchNewer: () => void;
	fetchOlder: () => void;
	/** Row height before measurement. Also the fallback the prepend correction uses. */
	estimateSize?: number;
	/** Row identity, for lists whose rows carry no id of their own. */
	getItemKey?: (item: T, index: number) => string | number;
	/**
	 * For a list that follows a live tail: a scroller parked at the top skips
	 * the prepend correction so the rows that just arrived stay in view. Lists
	 * whose top is a normal reading position leave this off and always correct,
	 * so the row being read holds its place.
	 */
	pinTop?: boolean;
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
		getItemKey: (index) => {
			const item = entries[index];
			if (item === undefined) return index;
			return getItemKey ? getItemKey(item, index) : (item.id ?? index);
		},
		getScrollElement: () => scrollEl,
		estimateSize: () => estimateSize,
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
			const keyOf = (item: T | undefined, index: number) => {
				if (item === undefined) return undefined;
				return getItemKey ? getItemKey(item, index) : item.id;
			};
			if (
				keyOf(entries[newItemCount], newItemCount) === keyOf(prev[0], 0) &&
				scrollEl
			) {
				// The rows committed in this same pass were measured by their ref
				// callbacks a moment ago; getVirtualItems() folds those sizes into
				// the measurements before they are read.
				virtualizer.getVirtualItems();
				const cache = virtualizer.measurementsCache;
				const first = cache[0];
				const oldFirst = cache[newItemCount];
				const added =
					first && oldFirst
						? oldFirst.start - first.start
						: newItemCount * estimateSize;
				if (!pinTop || scrollEl.scrollTop > 1) {
					scrollEl.scrollTop += added;
				}
				prevEntriesRef.current = entries;
				forceRerender((c) => c + 1);
				return;
			}
		}
		prevEntriesRef.current = entries;
	}, [entries, virtualizer, scrollEl, estimateSize, getItemKey, pinTop]);

	// A new list version replaced the rows wholesale (see useBidirectionalFetch
	// listVersion): start it at the top. Declared after the prepend correction
	// so a replacement that happens to look like a prepend still ends at 0.
	const prevListVersionRef = useRef(listVersion);
	useLayoutEffect(() => {
		if (listVersion === prevListVersionRef.current) return;
		prevListVersionRef.current = listVersion;
		if (scrollEl && scrollEl.scrollTop !== 0) {
			scrollEl.scrollTop = 0;
			forceRerender((c) => c + 1);
		}
	}, [listVersion, scrollEl]);

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

	// Loading more waits for a scroll, and a list shorter than its box cannot
	// scroll: on a tall or zoomed-out window the first page can fit whole and
	// the rest would never load. Whenever the rows or the box change, pull the
	// next page while the list still does not fill the box. One attempt per
	// list state (which list, how many rows): a failed fetch leaves both
	// unchanged, so it is not retried in a loop, only after new rows or a
	// resize.
	const fillAttemptRef = useRef<string | null>(null);
	useLayoutEffect(() => {
		if (!scrollEl) return;
		const fill = (retry: boolean) => {
			const state = `${listVersion}:${entries.length}`;
			if (
				hasAfter &&
				!isLoadingAfter &&
				entries.length > 0 &&
				(retry || fillAttemptRef.current !== state) &&
				// No height means no layout (hidden, or not yet laid out): nothing
				// to fill yet.
				scrollEl.clientHeight > 0 &&
				scrollEl.scrollHeight <= scrollEl.clientHeight
			) {
				fillAttemptRef.current = state;
				fetchOlder();
			}
		};
		fill(false);
		const onResize = () => fill(true);
		window.addEventListener("resize", onResize);
		return () => window.removeEventListener("resize", onResize);
	}, [
		scrollEl,
		listVersion,
		entries.length,
		hasAfter,
		isLoadingAfter,
		fetchOlder,
	]);

	// The rows actually on screen, 1-based, for the footer. virtualItems also
	// holds the overscan rendered off screen on either side, so it would
	// overstate the range; the virtualizer's own range does not. A virtualizer
	// that has not measured a range yet falls back to the rendered rows.
	const range = virtualizer.range;
	const startIndex = range
		? range.startIndex + 1
		: virtualItems.length > 0
			? virtualItems[0].index + 1
			: 0;
	const endIndex = range
		? range.endIndex + 1
		: virtualItems.length > 0
			? virtualItems[virtualItems.length - 1].index + 1
			: 0;

	return {
		/** Attach to the scroller: `ref={scrollRef}`. */
		scrollRef: setScrollEl,
		/** The scroller itself, once mounted, for controls that read its position. */
		scrollEl,
		virtualizer,
		virtualItems,
		paddingTop,
		paddingBottom,
		handleScroll,
		startIndex,
		endIndex,
	};
}
