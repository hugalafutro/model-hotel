/** Display-layer decoding for app-log lines.
 *
 * The backend's flattened k=v log form quotes any attribute value that holds
 * a space, quote, backslash or control character (strconv.Quote) and then
 * escapes the spaces inside that quoted value as `\x20` (see
 * internal/api/applogs_slog.go quoteLogValue), so whitespace-splitting readers
 * (CrowdSec grok, fail2ban, awk) can never be fed a forged key=value token by
 * caller-controlled input. That protection is about the STORED text and line
 * parsers; the dashboard renders text nodes, so it can safely show the human
 * form.
 *
 * Only the space escaping is reversed, and only where the encoder can have
 * produced it: inside a double-quoted token. A `\x20` outside quotes is raw
 * text (the pre-quoting encoder wrote bare values) and stays put, and so does
 * a `\x20` whose backslash is itself escaped (`\\x20`): the encoder doubles a
 * literal backslash, so an odd run of backslashes before `x20` is our space
 * escape and an even run belongs to the value. An escaped quote (`\"`) inside
 * a quoted value does not end the token.
 */
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
	const at = Math.min(Math.max(attrsAt ?? 0, 0), message.length);
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
