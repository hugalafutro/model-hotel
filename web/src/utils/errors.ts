// Coercions for the `unknown` a catch block hands over. One spelling each, so
// an error surfaced in a toast reads the same wherever it was thrown.

/**
 * The `message` of anything carrying a string one, the string form of anything
 * else, or `fallback` for an empty result. Duck-typed rather than gated on
 * `instanceof Error`, so a rejection whose Error class came from another bundle
 * or a mock still yields its message instead of "[object Object]".
 */
export function errorMessage(err: unknown, fallback = ""): string {
	const message =
		typeof err === "object" && err !== null
			? (err as { message?: unknown }).message
			: undefined;
	const msg = typeof message === "string" ? message : String(err ?? "");
	return msg || fallback;
}

/** The HTTP status carried by an ApiError-shaped rejection, if it has one. */
export function errorStatus(err: unknown): number | undefined {
	if (typeof err !== "object" || err === null) return undefined;
	const status = (err as { status?: unknown }).status;
	return typeof status === "number" ? status : undefined;
}

/**
 * The value as an Error, wrapping anything that is not already one. A nullish
 * rejection keeps its "null"/"undefined" spelling rather than collapsing to an
 * empty message, so a callout rendering `error.message` is never blank.
 */
export function asError(err: unknown): Error {
	return err instanceof Error ? err : new Error(String(err));
}
