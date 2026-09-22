/** Display-layer decoding for app-log lines stored before v1.0.0.
 *
 * Those releases escaped the spaces inside a quoted attribute value as `\x20`
 * so that readers which split on whitespace without honouring quotes could not
 * be fed a forged key=value token. The gateway no longer does that (the
 * CrowdSec collection reads the line as logfmt instead), but rows written by
 * an older build survive in the DB until retention clears them, and this turns
 * them back into their human form.
 *
 * Only the space escaping is reversed, and only where the encoder can have
 * produced it: inside a double-quoted token. A `\x20` outside quotes is raw
 * text (the pre-quoting encoder wrote bare values) and stays put, and so does
 * a `\x20` whose backslash is itself escaped (`\\x20`): the encoder doubles a
 * literal backslash, so an odd run of backslashes before `x20` is our space
 * escape and an even run belongs to the value. An escaped quote (`\"`) inside
 * a quoted value does not end the token.
 */
import { clamp } from "@web-shared/format";
/** The display form of an app-log message: decoded only when the backend
 * marked the row as using the flattened encoding (AppLogEntry.escaped), and
 * only from the recorded attribute boundary (AppLogEntry.attrs_at) onward.
 * Everything before the boundary is raw developer-written message text and
 * is never altered, whatever it contains; everything after it is pure
 * quoteLogValue output, on which the scan is exact. */
export function displayLogMessage(
	message: string,
	escaped: boolean | undefined,
	attrsAt?: number,
): string {
	if (!escaped) return message;
	const at = clamp(attrsAt ?? 0, 0, message.length);
	return message.slice(0, at) + decodeLogEscapes(message.slice(at));
}

export function decodeLogEscapes(message: string): string {
	let out = "";
	let inQuotes = false;
	let i = 0;
	while (i < message.length) {
		const ch = message[i];
		if (ch === "\\") {
			let j = i;
			while (j < message.length && message[j] === "\\") j++;
			const run = j - i;
			const escapesNext = run % 2 === 1;
			if (inQuotes && escapesNext && message.startsWith("x20", j)) {
				out += `${"\\".repeat(run - 1)} `;
				i = j + 3;
				continue;
			}
			if (escapesNext && message[j] === '"') {
				// \" inside a quoted value: copy it through without toggling.
				out += message.slice(i, j + 1);
				i = j + 1;
				continue;
			}
			out += message.slice(i, j);
			i = j;
			continue;
		}
		if (ch === '"') inQuotes = !inQuotes;
		out += ch;
		i++;
	}
	return out;
}

/** Stable identity for an app-log row. Rows can arrive without an id (raw
 * io.Writer lines), so the fields that together identify one stand in. Two
 * id-less rows logged in the same instant with the same source and opening
 * text share a key, which readers should treat as "one of these" rather than
 * a unique row. */
export function appLogKey(entry: {
	id?: string;
	timestamp: string;
	source: string;
	message: string;
}): string {
	return (
		entry.id ??
		`${entry.timestamp}-${entry.source}-${entry.message.slice(0, 20)}`
	);
}
