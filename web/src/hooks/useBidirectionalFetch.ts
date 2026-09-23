import { useCallback, useEffect, useRef, useState } from "react";
import i18next from "../i18n";
import { errorMessage } from "../utils/errors";

const FETCH_SIZE = 200;
const MAX_ROWS = 10000;

export interface CursorResponse<T> {
	entries: T[];
	total: number;
	has_before: boolean;
	has_after: boolean;
}

export type CursorFetchFn<
	T,
	R extends CursorResponse<T> = CursorResponse<T>,
> = (params: {
	cursor?: string;
	direction: "after" | "before";
	limit: number;
	sort_dir: string;
	[key: string]: string | number | undefined;
}) => Promise<R>;

export interface UseBidirectionalFetchOptions<
	T,
	R extends CursorResponse<T> = CursorResponse<T>,
> {
	fetchFn: CursorFetchFn<T, R>;
	filters: Record<string, string | undefined>;
	sortDir: string;
	getCursor: (entry: T) => string;
	getId: (entry: T) => string;
}

export interface UseBidirectionalFetchReturn<
	T,
	R extends CursorResponse<T> = CursorResponse<T>,
> {
	entries: T[];
	total: number;
	/**
	 * The most recent response the hook accepted, null after a reset. Callers
	 * read endpoint-specific extras (filter-wide counts and the like) from
	 * here rather than inside fetchFn, so a response discarded by the
	 * generation guard cannot leak its extras either.
	 */
	lastResponse: R | null;
	hasBefore: boolean;
	hasAfter: boolean;
	/**
	 * Changes whenever the rows are replaced wholesale (a fresh first page, a
	 * reset, a failed refetch), never on a prepend, append or merge. A filter
	 * change keeps the old rows on screen until the new page lands, so the
	 * scroller uses this, not an empty list, as its cue to return to the top.
	 */
	listVersion: number;
	isLoadingInitial: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	error: string | null;
	fetchInitial: () => Promise<void>;
	fetchNewer: () => Promise<void>;
	fetchOlder: () => Promise<void>;
	reset: () => void;
	mergeEntries: (updated: T[]) => void;
}

function deepEqualFilters(
	a: Record<string, string | undefined>,
	b: Record<string, string | undefined>,
): boolean {
	const keysA = Object.keys(a);
	const keysB = Object.keys(b);
	if (keysA.length !== keysB.length) return false;
	for (const key of keysA) {
		if (a[key] !== b[key]) return false;
	}
	return true;
}

export function useBidirectionalFetch<
	T,
	R extends CursorResponse<T> = CursorResponse<T>,
>({
	fetchFn,
	filters,
	sortDir,
	getCursor,
	getId,
}: UseBidirectionalFetchOptions<T, R>): UseBidirectionalFetchReturn<T, R> {
	const [entries, setEntries] = useState<T[]>([]);
	// The generation that produced `entries`, set in the same render as them,
	// so a page fetch can tell rows from before a filter change apart from the
	// current ones even while the refetch's rows are still on their way in.
	const [entriesGen, setEntriesGen] = useState(0);
	const [total, setTotal] = useState<number>(0);
	const [lastResponse, setLastResponse] = useState<R | null>(null);
	const [hasBefore, setHasBefore] = useState<boolean>(false);
	const [hasAfter, setHasAfter] = useState<boolean>(false);
	const [isLoadingInitial, setIsLoadingInitial] = useState<boolean>(false);
	const [isLoadingBefore, setIsLoadingBefore] = useState<boolean>(false);
	const [isLoadingAfter, setIsLoadingAfter] = useState<boolean>(false);
	const [error, setError] = useState<string | null>(null);

	// Refs for loading guards (don't trigger re-renders)
	const isLoadingBeforeRef = useRef<boolean>(false);
	const isLoadingAfterRef = useRef<boolean>(false);
	const isLoadingInitialRef = useRef<boolean>(false);

	// Generation counter: incremented on every reset/filter change.
	// Each fetch captures the current generation at start and discards
	// results if the generation has moved on (stale in-flight request).
	const generationRef = useRef<number>(0);

	// Ref to track previous filter values for change detection
	const prevFiltersRef = useRef<Record<string, string | undefined> | null>(
		null,
	);
	const prevSortDirRef = useRef<string | null>(null);

	// Drops every in-flight fetch without touching the loaded data.
	const invalidate = useCallback(() => {
		generationRef.current++;
		setError(null);
		// The dropped fetches' finally blocks skip on the generation check, so
		// their loading flags are cleared here or they would stay set.
		setIsLoadingBefore(false);
		setIsLoadingAfter(false);
		isLoadingBeforeRef.current = false;
		isLoadingAfterRef.current = false;
		isLoadingInitialRef.current = false;
	}, []);

	const clearData = useCallback(() => {
		setEntries([]);
		setEntriesGen(generationRef.current);
		setTotal(0);
		setLastResponse(null);
		setHasBefore(false);
		setHasAfter(false);
	}, []);

	const reset = useCallback(() => {
		invalidate();
		clearData();
	}, [invalidate, clearData]);

	const mergeEntries = useCallback(
		(updated: T[]) => {
			if (updated.length === 0) return;
			setEntries((prev) => {
				const updateMap = new Map(updated.map((e) => [getId(e), e]));
				return prev.map((e) => updateMap.get(getId(e)) ?? e);
			});
		},
		[getId],
	);

	const fetchInitial = useCallback(async () => {
		if (isLoadingInitialRef.current) return;

		isLoadingInitialRef.current = true;
		setIsLoadingInitial(true);
		setError(null);

		const gen = generationRef.current;

		try {
			const response = await fetchFn({
				direction: "after",
				limit: FETCH_SIZE,
				sort_dir: sortDir,
				...filters,
			});

			// Discard if a newer fetch was triggered (filter change, etc.)
			if (gen !== generationRef.current) return;

			setEntries(response.entries);
			setEntriesGen(gen);
			setTotal(response.total);
			setLastResponse(response);
			setHasBefore(response.has_before);
			setHasAfter(response.has_after);
		} catch (err) {
			if (gen !== generationRef.current) return;
			// Rows kept from before a filter change no longer match the filters.
			clearData();
			setError(
				err instanceof Error
					? err.message
					: i18next.t("hooks.useBidirectionalFetch.initialError"),
			);
		} finally {
			if (gen === generationRef.current) {
				isLoadingInitialRef.current = false;
				setIsLoadingInitial(false);
			}
		}
	}, [fetchFn, filters, sortDir, clearData]);

	// The two directions are one routine: which end of the list supplies the
	// cursor, which loading pair guards it, where the page is spliced in, and
	// which "has more" flag it updates.
	const fetchPage = useCallback(
		async (direction: "before" | "after") => {
			const before = direction === "before";
			const loadingRef = before ? isLoadingBeforeRef : isLoadingAfterRef;
			const setLoading = before ? setIsLoadingBefore : setIsLoadingAfter;
			const setHas = before ? setHasBefore : setHasAfter;

			if (
				loadingRef.current ||
				isLoadingInitialRef.current ||
				// Rows from before a filter change: their cursor belongs to the
				// old filters.
				entriesGen !== generationRef.current ||
				entries.length === 0 ||
				entries.length >= MAX_ROWS
			) {
				return;
			}

			const cursor = getCursor(
				before ? entries[0] : entries[entries.length - 1],
			);

			loadingRef.current = true;
			setLoading(true);
			setError(null);

			const gen = generationRef.current;

			try {
				const response = await fetchFn({
					cursor,
					direction,
					limit: FETCH_SIZE,
					sort_dir: sortDir,
					...filters,
				});

				if (gen !== generationRef.current) return;
				setLastResponse(response);

				if (response.entries.length === 0) {
					setHas(false);
					return;
				}

				setEntries((prev) => {
					const existingIds = new Set(prev.map((e) => getId(e)));
					const fresh = response.entries.filter(
						(e) => !existingIds.has(getId(e)),
					);
					return before ? [...fresh, ...prev] : [...prev, ...fresh];
				});

				setHas(before ? response.has_before : response.has_after);
			} catch (err) {
				if (gen !== generationRef.current) return;
				setError(
					errorMessage(
						err,
						i18next.t(
							before
								? "hooks.useBidirectionalFetch.newerError"
								: "hooks.useBidirectionalFetch.olderError",
						),
					),
				);
			} finally {
				if (gen === generationRef.current) {
					loadingRef.current = false;
					setLoading(false);
				}
			}
		},
		[fetchFn, filters, sortDir, entries, entriesGen, getCursor, getId],
	);

	const fetchNewer = useCallback(() => fetchPage("before"), [fetchPage]);
	const fetchOlder = useCallback(() => fetchPage("after"), [fetchPage]);

	// Detect filter changes and refetch. The current rows stay on screen until
	// the new page replaces them, so the table does not blank and re-fill.
	useEffect(() => {
		const filtersChanged =
			!prevFiltersRef.current ||
			!deepEqualFilters(prevFiltersRef.current, filters);
		const sortDirChanged = prevSortDirRef.current !== sortDir;

		if (filtersChanged || sortDirChanged) {
			prevFiltersRef.current = filters;
			prevSortDirRef.current = sortDir;
			invalidate();
			fetchInitial();
		}
	}, [filters, sortDir, invalidate, fetchInitial]);

	return {
		entries,
		total,
		lastResponse,
		hasBefore,
		hasAfter,
		listVersion: entriesGen,
		isLoadingInitial,
		isLoadingBefore,
		isLoadingAfter,
		error,
		fetchInitial,
		fetchNewer,
		fetchOlder,
		reset,
		mergeEntries,
	};
}
