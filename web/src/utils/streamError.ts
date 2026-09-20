import type { TFunction } from "i18next";

/**
 * The error a failed streaming request reads as: the message from the JSON
 * error envelope the server writes ({"error":{"message":...}}), or the raw
 * body when there is none, behind a translated prefix. The page shows this
 * text in the toast and the reply card, so it is never the raw envelope.
 */
export async function streamRequestError(
	resp: Response,
	t: TFunction,
): Promise<Error> {
	const text = await resp.text();
	let detail = text;
	try {
		const envelope = (
			JSON.parse(text) as { error?: string | { message?: unknown } }
		).error;
		if (typeof envelope === "string") {
			detail = envelope;
		} else if (typeof envelope?.message === "string") {
			detail = envelope.message;
		}
	} catch {
		// Not JSON: the body is the detail.
	}
	return new Error(
		t("chat.stream.requestFailed", { status: resp.status, detail }),
	);
}
