// The clipboard write both frontends perform, without the React state that
// surrounds it in each app's useCopyToClipboard: put the text on the clipboard,
// and say whether it landed.

/**
 * A writer used instead of `navigator.clipboard.writeText`, for the call sites
 * that need their own fallback path (a dashboard served over plain HTTP, where
 * the Clipboard API does not exist).
 */
export type ClipboardWriter = (text: string) => Promise<void> | void;

/**
 * writeClipboard resolves true once the text has reached the clipboard, and
 * false instead of throwing when it has not, so a caller that reports a failure
 * still can. The write runs inside the async body on purpose: both
 * `navigator.clipboard` being undefined in a non-secure (plain HTTP) context
 * and a writer that throws synchronously become rejections the catch sees,
 * rather than escaping past it.
 */
export async function writeClipboard(
	text: string,
	writer?: ClipboardWriter,
): Promise<boolean> {
	try {
		await (writer ? writer(text) : navigator.clipboard.writeText(text));
	} catch {
		return false;
	}
	return true;
}
