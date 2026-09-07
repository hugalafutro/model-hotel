/**
 * The set with `key` flipped, or forced to `on` when it is given. Returns a new
 * Set so React state updates see a fresh reference.
 */
export function toggleInSet<T>(
	set: ReadonlySet<T>,
	key: T,
	on?: boolean,
): Set<T> {
	const next = new Set(set);
	if (on ?? !next.has(key)) next.add(key);
	else next.delete(key);
	return next;
}
