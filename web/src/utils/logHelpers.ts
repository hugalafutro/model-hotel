/**
 * Checks whether a request log error message indicates the request was
 * cancelled or interrupted (client disconnect, timeout, etc.) rather than
 * failing due to an upstream provider error.
 */
export const isCancelled = (errorMessage?: string): boolean => {
	if (!errorMessage) return false;
	const msg = errorMessage.toLowerCase();
	return (
		msg.includes("cancel") ||
		msg.includes("disconnect") ||
		msg.includes("upstream request timed out") ||
		msg.includes("param-strip retry timed out")
	);
};
