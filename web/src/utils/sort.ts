/** A copy sorted by `name` in the browser's collation order. */
export function sortByName<T extends { name: string }>(
	items: readonly T[] | undefined,
): T[] {
	return [...(items ?? [])].sort((a, b) => a.name.localeCompare(b.name));
}
