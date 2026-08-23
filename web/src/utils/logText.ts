/** Display-layer decoding for app-log lines.
 *
 * The backend's flattened k=v log form escapes spaces inside quoted attribute
 * values as `\x20` (see internal/api/applogs_slog.go quoteLogValue) so that
 * whitespace-splitting readers (CrowdSec grok, fail2ban, awk) can never be
 * fed a forged key=value token by caller-controlled input. That protection is
 * about the STORED text and line parsers; the dashboard renders text nodes,
 * so it can safely show the human form.
 *
 * Only the space escaping is reversed. A `\x20` whose backslash is itself
 * escaped (`\\x20`) was a literal `\x20` in the original value and stays put:
 * an even run of preceding backslashes means the escape is ours, an odd run
 * means the backslash belongs to the value.
 */
export function decodeLogEscapes(message: string): string {
	return message.replace(/(\\*)\\x20/g, (match, slashes: string) =>
		slashes.length % 2 === 0 ? `${slashes} ` : match,
	);
}
