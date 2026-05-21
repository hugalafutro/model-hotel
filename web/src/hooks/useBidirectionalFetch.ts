import { useCallback, useEffect, useRef, useState } from "react";

const FETCH_SIZE = 200;
const MAX_ROWS = 10000;

export type CursorFetchFn<T> = (params: {
	cursor?: string;
	direction: "after" | "before";
	limit: number;
	sort_dir: string;
	[key: string]: string | number | undefined;
}) => Promise<{
	entries: T[];
	total: number;
	has_before: boolean;
	has_after: boolean;
}>;

export interface UseBidirectionalFetchOptions<T> {
	fetchFn: CursorFetchFn<T>;
	filters: Record<string, string | undefined>;
	sortDir: string;
	getCursor: (entry: T) => string;
	getId: (entry: T) => string;
}

export interface UseBidirectionalFetchReturn<T> {
	entries: T[];
	total: number;
	hasBefore: boolean;
	hasAfter: boolean;
	isLoadingInitial: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	error: string | null;
	fetchInitial: () => Promise<void>;
	fetchNewer: () => Promise<void>;
	fetchOlder: () => Promise<void>;
	reset: () => void;
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

export function useBidirectionalFetch<T>({
	fetchFn,
	filters,
	sortDir,
	getCursor,
	getId,
}: UseBidirectionalFetchOptions<T>): UseBidirectionalFetchReturn<T> {
	const [entries, setEntries] = useState<T[]>([]);
	const [total, setTotal] = useState<number>(0);
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

	// Ref to track previous filter values for change detection
	const prevFiltersRef = useRef<Record<string, string | undefined> | null>(
		null,
	);
	const prevSortDirRef = useRef<string | null>(null);

	const reset = useCallback(() => {
		setEntries([]);
		setTotal(0);
		setHasBefore(false);
		setHasAfter(false);
		setError(null);
		isLoadingBeforeRef.current = false;
		isLoadingAfterRef.current = false;
		isLoadingInitialRef.current = false;
	}, []);

	const fetchInitial = useCallback(async () => {
		if (isLoadingInitialRef.current) return;

		isLoadingInitialRef.current = true;
		setIsLoadingInitial(true);
		setError(null);

		try {
			const response = await fetchFn({
				direction: "after",
				limit: FETCH_SIZE,
				sort_dir: sortDir,
				...filters,
			});

			setEntries(response.entries);
			setTotal(response.total);
			setHasBefore(response.has_before);
			setHasAfter(response.has_after);
		} catch (err) {
			setError(
				err instanceof Error ? err.message : "Failed to fetch initial data",
			);
		} finally {
			isLoadingInitialRef.current = false;
			setIsLoadingInitial(false);
		}
	}, [fetchFn, filters, sortDir]);

	const fetchNewer = useCallback(async () => {
		if (
			isLoadingBeforeRef.current ||
			entries.length === 0 ||
			entries.length >= MAX_ROWS
		) {
			return;
		}

		const firstEntry = entries[0];
		const cursor = getCursor(firstEntry);

		isLoadingBeforeRef.current = true;
		setIsLoadingBefore(true);
		setError(null);

		try {
			const response = await fetchFn({
				cursor,
				direction: "before",
				limit: FETCH_SIZE,
				sort_dir: sortDir,
				...filters,
			});

			if (response.entries.length === 0) {
				setHasBefore(false);
				return;
			}

			// Prepend new entries, deduplicate by id
			setEntries((prev) => {
				const existingIds = new Set(prev.map((e) => getId(e)));
				const newEntries = response.entries.filter(
					(e) => !existingIds.has(getId(e)),
				);
				return [...newEntries, ...prev];
			});

			setHasBefore(response.has_before);
		} catch (err) {
			setError(
				err instanceof Error ? err.message : "Failed to fetch newer entries",
			);
		} finally {
			isLoadingBeforeRef.current = false;
			setIsLoadingBefore(false);
		}
	}, [fetchFn, filters, sortDir, entries, getCursor, getId]);

	const fetchOlder = useCallback(async () => {
		if (
			isLoadingAfterRef.current ||
			entries.length === 0 ||
			entries.length >= MAX_ROWS
		) {
			return;
		}

		const lastEntry = entries[entries.length - 1];
		const cursor = getCursor(lastEntry);

		isLoadingAfterRef.current = true;
		setIsLoadingAfter(true);
		setError(null);

		try {
			const response = await fetchFn({
				cursor,
				direction: "after",
				limit: FETCH_SIZE,
				sort_dir: sortDir,
				...filters,
			});

			if (response.entries.length === 0) {
				setHasAfter(false);
				return;
			}

			// Append new entries, deduplicate by id
			setEntries((prev) => {
				const existingIds = new Set(prev.map((e) => getId(e)));
				const newEntries = response.entries.filter(
					(e) => !existingIds.has(getId(e)),
				);
				return [...prev, ...newEntries];
			});

			setHasAfter(response.has_after);
		} catch (err) {
			setError(
				err instanceof Error ? err.message : "Failed to fetch older entries",
			);
		} finally {
			isLoadingAfterRef.current = false;
			setIsLoadingAfter(false);
		}
	}, [fetchFn, filters, sortDir, entries, getCursor, getId]);

	// Detect filter changes and reset + refetch
	useEffect(() => {
		const filtersChanged =
			!prevFiltersRef.current ||
			!deepEqualFilters(prevFiltersRef.current, filters);
		const sortDirChanged = prevSortDirRef.current !== sortDir;

		if (filtersChanged || sortDirChanged) {
			prevFiltersRef.current = filters;
			prevSortDirRef.current = sortDir;
			reset();
			fetchInitial();
		}
	}, [filters, sortDir, reset, fetchInitial]);

	return {
		entries,
		total,
		hasBefore,
		hasAfter,
		isLoadingInitial,
		isLoadingBefore,
		isLoadingAfter,
		error,
		fetchInitial,
		fetchNewer,
		fetchOlder,
		reset,
	};
}
