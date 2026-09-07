// Coercions for the `unknown` a catch block hands over. One spelling each, so
// an error surfaced in a toast reads the same wherever it was thrown.

/** The message of an Error, the string form of anything else, or `fallback` for an empty one. */
export function errorMessage(err: unknown, fallback = ""): string {
	const msg = err instanceof Error ? err.message : String(err ?? "");
	return msg || fallback;
}

/** The HTTP status carried by an ApiError-shaped rejection, if it has one. */
export function errorStatus(err: unknown): number | undefined {
	if (typeof err !== "object" || err === null) return undefined;
	const status = (err as { status?: unknown }).status;
	return typeof status === "number" ? status : undefined;
}

/** The value as an Error, wrapping anything that is not already one. */
export function asError(err: unknown): Error {
	return err instanceof Error ? err : new Error(String(err ?? ""));
}
