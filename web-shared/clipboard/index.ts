// The clipboard write both frontends perform, without the React state that
// surrounds it in each app's useCopyToClipboard: put the text on the clipboard,
// and say whether it landed.

/**
 * legacyCopy is the selection-based copy used where `navigator.clipboard` does
 * not exist: a dashboard served over plain HTTP on a LAN (a normal self-hosted
 * setup) is not a secure context, so the Clipboard API is absent there.
 */
function legacyCopy(text: string): boolean {
	const holder = document.createElement("textarea");
	holder.value = text;
	holder.setAttribute("readonly", "");
	holder.style.position = "fixed";
	holder.style.opacity = "0";
	document.body.appendChild(holder);
	holder.select();
	try {
		return document.execCommand("copy");
	} finally {
		document.body.removeChild(holder);
	}
}

/**
 * writeClipboard resolves true once the text has reached the clipboard, and
 * false instead of throwing when it has not, so a caller that reports a failure
 * still can. The write runs inside the async body on purpose: a Clipboard API
 * that rejects or throws synchronously becomes a rejection the catch sees,
 * rather than escaping past it. The legacy selection copy stands in both where
 * the Clipboard API is absent and where its write is refused (an unfocused
 * document, a denied permission), which is where a fallback is worth having.
 */
export async function writeClipboard(text: string): Promise<boolean> {
	try {
		if (navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(text);
			return true;
		}
	} catch {
		// Fall through to the selection copy: a refused write is the case the
		// fallback exists for.
	}
	try {
		return legacyCopy(text);
	} catch {
		return false;
	}
}
