import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBidirectionalFetch } from "../useBidirectionalFetch";

type TestEntry = {
	id: string;
	name: string;
};

const defaultResponse = {
	entries: [{ id: "1", name: "item-1" }] as TestEntry[],
	total: 1,
	has_before: false,
	has_after: false,
};

describe("useBidirectionalFetch", () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	describe("deepEqualFilters", () => {
		it("returns true for identical filters", async () => {
			const filters = { status: "active", type: "user" };

			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters,
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			// Wait for initial fetch
			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			// Should only fetch once (filters are equal, no refetch)
			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});

		it("returns false when values differ", async () => {
			const filtersA = { status: "active" };
			const filtersB = { status: "inactive" };

			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result, rerender } = renderHook(
				({ filters }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters,
						sortDir: "asc",
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { filters: filtersA } },
			);

			// Wait for initial fetch
			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			// Change filters - should trigger reset and refetch
			rerender({ filters: filtersB });

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});
		});

		it("returns false when key counts differ", async () => {
			const filtersA = { status: "active" };
			const filtersB = { status: "active", type: "user" };

			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result, rerender } = renderHook(
				({ filters }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters,
						sortDir: "asc",
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { filters: filtersA } },
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			rerender({ filters: filtersB });

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});
		});
	});

	describe("Initial state", () => {
		it("returns default state on mount", () => {
			const mockFetchFn = vi.fn();

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			// Initial state before fetch completes
			expect(result.current.entries).toEqual([]);
			expect(result.current.total).toBe(0);
			expect(result.current.hasBefore).toBe(false);
			expect(result.current.hasAfter).toBe(false);
			// Note: isLoadingInitial may be true since hook auto-fetches on mount
			expect(result.current.isLoadingBefore).toBe(false);
			expect(result.current.isLoadingAfter).toBe(false);
			expect(result.current.error).toBeNull();
		});

		it("calls fetchInitial on mount automatically via filter change effect", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			expect(result.current.entries).toHaveLength(1);
		});
	});

	describe("fetchInitial", () => {
		it("sets entries/total/hasBefore/hasAfter from response", async () => {
			const response = {
				entries: [
					{ id: "1", name: "item-1" },
					{ id: "2", name: "item-2" },
				] as TestEntry[],
				total: 42,
				has_before: true,
				has_after: true,
			};
			const mockFetchFn = vi.fn().mockResolvedValue(response);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(2);
			});

			expect(result.current.total).toBe(42);
			expect(result.current.hasBefore).toBe(true);
			expect(result.current.hasAfter).toBe(true);
		});

		it("sets error on fetch failure", async () => {
			const mockFetchFn = vi.fn().mockRejectedValue(new Error("Network error"));

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.error).toBe("Network error");
			});

			expect(result.current.entries).toEqual([]);
		});

		it("guards against concurrent calls", async () => {
			let resolvePromise: (value: typeof defaultResponse) => void;
			const promise = new Promise<typeof defaultResponse>((resolve) => {
				resolvePromise = resolve;
			});

			const mockFetchFn = vi.fn().mockReturnValue(promise);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			// First call happens automatically on mount
			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			// Try to call again while first is in flight
			act(() => {
				result.current.fetchInitial();
			});

			// Should still be only 1 call (guarded)
			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			// Resolve the promise
			act(() => {
				resolvePromise?.(defaultResponse);
			});

			await waitFor(() => {
				expect(result.current.isLoadingInitial).toBe(false);
			});
		});

		it("discards stale results when generation changes", async () => {
			let resolveFirst: (value: typeof defaultResponse) => void;
			const firstPromise = new Promise<typeof defaultResponse>((resolve) => {
				resolveFirst = resolve;
			});

			const mockFetchFn = vi
				.fn()
				.mockReturnValueOnce(firstPromise)
				.mockResolvedValueOnce({
					entries: [{ id: "fresh", name: "fresh-item" }] as TestEntry[],
					total: 1,
					has_before: false,
					has_after: false,
				});

			const { result, rerender } = renderHook(
				({ filters }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters,
						sortDir: "asc",
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { filters: {} } },
			);

			// First fetch is in flight
			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			// Trigger reset by changing filters (increments generation)
			rerender({ filters: { status: "new" } });

			// Second fetch should start
			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});

			// Resolve the first (stale) promise
			act(() => {
				resolveFirst?.(defaultResponse);
			});

			// Wait for second fetch to complete
			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			// Should have fresh entry, not stale one
			expect(result.current.entries[0].id).toBe("fresh");
		});
	});

	describe("fetchNewer", () => {
		it("prepends new entries and deduplicates by ID", async () => {
			const initialResponse = {
				entries: [
					{ id: "2", name: "item-2" },
					{ id: "3", name: "item-3" },
				] as TestEntry[],
				total: 3,
				has_before: true,
				has_after: false,
			};

			const newerResponse = {
				entries: [
					{ id: "1", name: "item-1" },
					{ id: "2", name: "item-2-duplicate" },
				] as TestEntry[],
				total: 3,
				has_before: false,
				has_after: false,
			};

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(initialResponse)
				.mockResolvedValueOnce(newerResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			// Wait for initial fetch
			await waitFor(() => {
				expect(result.current.entries).toHaveLength(2);
			});

			// Fetch newer entries
			act(() => {
				result.current.fetchNewer();
			});

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(3);
			});

			// Should be prepended: [1, 2, 3]
			expect(result.current.entries[0].id).toBe("1");
			expect(result.current.entries[1].id).toBe("2");
			expect(result.current.entries[2].id).toBe("3");
		});

		it("sets hasBefore=false when response has no entries", async () => {
			const initialResponse = {
				entries: [{ id: "2", name: "item-2" }] as TestEntry[],
				total: 1,
				has_before: true,
				has_after: false,
			};

			const emptyResponse = {
				entries: [] as TestEntry[],
				total: 1,
				has_before: false,
				has_after: false,
			};

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(initialResponse)
				.mockResolvedValueOnce(emptyResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.hasBefore).toBe(true);
			});

			act(() => {
				result.current.fetchNewer();
			});

			await waitFor(() => {
				expect(result.current.hasBefore).toBe(false);
			});
		});

		it("returns early if already loading before", async () => {
			let resolvePromise: (value: typeof defaultResponse) => void;
			const promise = new Promise<typeof defaultResponse>((resolve) => {
				resolvePromise = resolve;
			});

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(defaultResponse)
				.mockReturnValue(promise);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			// Start first fetchNewer
			act(() => {
				result.current.fetchNewer();
			});

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});

			// Try second fetchNewer while first is in flight
			act(() => {
				result.current.fetchNewer();
			});

			// Should still be only 2 calls
			expect(mockFetchFn).toHaveBeenCalledTimes(2);

			// Resolve
			act(() => {
				resolvePromise?.(defaultResponse);
			});
		});

		it("returns early if initial is still loading", async () => {
			let resolveInitial: (value: typeof defaultResponse) => void;
			const promise = new Promise<typeof defaultResponse>((resolve) => {
				resolveInitial = resolve;
			});

			const mockFetchFn = vi.fn().mockReturnValue(promise);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			// Initial fetch is in flight
			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			// Try fetchNewer while initial is loading
			act(() => {
				result.current.fetchNewer();
			});

			// Should not call fetchFn again
			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			// Resolve initial
			act(() => {
				resolveInitial?.(defaultResponse);
			});
		});

		it("returns early if entries.length === 0", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue({
				entries: [] as TestEntry[],
				total: 0,
				has_before: false,
				has_after: false,
			});

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(0);
			});

			// fetchNewer should return early without calling fetchFn again
			act(() => {
				result.current.fetchNewer();
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});

		it("returns early if entries.length >= MAX_ROWS", async () => {
			const manyEntries = Array.from({ length: 10000 }, (_, i) => ({
				id: `${i + 1}`,
				name: `item-${i + 1}`,
			})) as TestEntry[];

			const initialResponse = {
				entries: manyEntries,
				total: 10000,
				has_before: true,
				has_after: false,
			};

			const mockFetchFn = vi.fn().mockResolvedValueOnce(initialResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(10000);
			});

			act(() => {
				result.current.fetchNewer();
			});

			// Should not fetch more
			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});
	});

	describe("fetchOlder", () => {
		it("appends new entries and deduplicates by ID", async () => {
			const initialResponse = {
				entries: [
					{ id: "1", name: "item-1" },
					{ id: "2", name: "item-2" },
				] as TestEntry[],
				total: 3,
				has_before: false,
				has_after: true,
			};

			const olderResponse = {
				entries: [
					{ id: "3", name: "item-3" },
					{ id: "2", name: "item-2-duplicate" },
				] as TestEntry[],
				total: 3,
				has_before: false,
				has_after: false,
			};

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(initialResponse)
				.mockResolvedValueOnce(olderResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(2);
			});

			act(() => {
				result.current.fetchOlder();
			});

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(3);
			});

			// Should be appended: [1, 2, 3]
			expect(result.current.entries[0].id).toBe("1");
			expect(result.current.entries[1].id).toBe("2");
			expect(result.current.entries[2].id).toBe("3");
		});

		it("sets hasAfter=false when response has no entries", async () => {
			const initialResponse = {
				entries: [{ id: "1", name: "item-1" }] as TestEntry[],
				total: 1,
				has_before: false,
				has_after: true,
			};

			const emptyResponse = {
				entries: [] as TestEntry[],
				total: 1,
				has_before: false,
				has_after: false,
			};

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(initialResponse)
				.mockResolvedValueOnce(emptyResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.hasAfter).toBe(true);
			});

			act(() => {
				result.current.fetchOlder();
			});

			await waitFor(() => {
				expect(result.current.hasAfter).toBe(false);
			});
		});

		it("returns early if already loading after", async () => {
			let resolvePromise: (value: typeof defaultResponse) => void;
			const promise = new Promise<typeof defaultResponse>((resolve) => {
				resolvePromise = resolve;
			});

			const mockFetchFn = vi
				.fn()
				.mockResolvedValueOnce(defaultResponse)
				.mockReturnValue(promise);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			act(() => {
				result.current.fetchOlder();
			});

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});

			act(() => {
				result.current.fetchOlder();
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(2);

			act(() => {
				resolvePromise?.(defaultResponse);
			});
		});

		it("returns early if initial is still loading", async () => {
			let resolveInitial: (value: typeof defaultResponse) => void;
			const promise = new Promise<typeof defaultResponse>((resolve) => {
				resolveInitial = resolve;
			});

			const mockFetchFn = vi.fn().mockReturnValue(promise);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			act(() => {
				result.current.fetchOlder();
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			act(() => {
				resolveInitial?.(defaultResponse);
			});
		});

		it("returns early if entries.length === 0", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue({
				entries: [] as TestEntry[],
				total: 0,
				has_before: false,
				has_after: false,
			});

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(0);
			});

			// fetchOlder should return early without calling fetchFn again
			act(() => {
				result.current.fetchOlder();
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});

		it("returns early if entries.length >= MAX_ROWS", async () => {
			const manyEntries = Array.from({ length: 10000 }, (_, i) => ({
				id: `${i + 1}`,
				name: `item-${i + 1}`,
			})) as TestEntry[];

			const initialResponse = {
				entries: manyEntries,
				total: 10000,
				has_before: false,
				has_after: true,
			};

			const mockFetchFn = vi.fn().mockResolvedValueOnce(initialResponse);

			const { result } = renderHook(() =>
				useBidirectionalFetch<TestEntry>({
					fetchFn: mockFetchFn,
					filters: {},
					sortDir: "asc",
					getCursor: (e) => e.id,
					getId: (e) => e.id,
				}),
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(10000);
			});

			act(() => {
				result.current.fetchOlder();
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});
	});

	describe("Filter change effect", () => {
		it("resets and refetches when filters change", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result, rerender } = renderHook(
				({ filters }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters,
						sortDir: "asc",
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { filters: {} } },
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			// Reset the mock to track the second call
			mockFetchFn.mockClear();

			rerender({ filters: { status: "active" } });

			// Wait for the refetch to start
			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(1);
			});

			// Wait for the refetch to complete
			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			// Verify fetch was called with updated filters
			expect(mockFetchFn).toHaveBeenCalledWith(
				expect.objectContaining({
					direction: "after",
					sort_dir: "asc",
					status: "active",
				}),
			);
		});

		it("resets and refetches when sortDir changes", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result, rerender } = renderHook(
				({ sortDir }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters: {},
						sortDir,
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { sortDir: "asc" } },
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			rerender({ sortDir: "desc" });

			await waitFor(() => {
				expect(mockFetchFn).toHaveBeenCalledTimes(2);
			});
		});

		it("does NOT reset when filters and sortDir are unchanged", async () => {
			const mockFetchFn = vi.fn().mockResolvedValue(defaultResponse);

			const { result, rerender } = renderHook(
				({ filters }) =>
					useBidirectionalFetch<TestEntry>({
						fetchFn: mockFetchFn,
						filters,
						sortDir: "asc",
						getCursor: (e) => e.id,
						getId: (e) => e.id,
					}),
				{ initialProps: { filters: {} } },
			);

			await waitFor(() => {
				expect(result.current.entries).toHaveLength(1);
			});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);

			// Rerender with same filters (object identity may differ but values same)
			rerender({ filters: {} });

			// Drain any pending microtasks/effects
			await act(async () => {});

			expect(mockFetchFn).toHaveBeenCalledTimes(1);
		});
	});
});
