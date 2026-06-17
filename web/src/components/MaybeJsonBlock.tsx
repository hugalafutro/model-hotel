import { ShikiCode } from "./ShikiCode";

interface MaybeJsonBlockProps {
	/** The raw text to display (provider error body, app-log message, ...). */
	text: string;
	/** Classes for the wrapping <pre>. Callers own the color/background. */
	className?: string;
}

/**
 * Renders `text` inside a <pre>. When the text contains a JSON object or array
 * (optionally behind a leading prefix like "HTTP 401: " that the proxy prepends
 * to upstream error bodies), the JSON is pretty-printed and syntax-highlighted
 * via shiki (json grammar) with the prefix kept on its own plain line above it;
 * otherwise the raw text renders as plain monospace, unchanged. Used for
 * provider error bodies and structured app-log messages — JSON some of the
 * time, plain prose the rest — so the parse is best-effort and silent.
 */
export function MaybeJsonBlock({ text, className }: MaybeJsonBlockProps) {
	const parsed = extractJson(text);
	return (
		<pre className={className}>
			{parsed === null ? (
				text
			) : (
				<>
					{parsed.prefix && `${parsed.prefix}\n`}
					{/* Key by content: ShikiCode caches highlighted tokens in local
					    state and does not clear them when `code` changes, so a new
					    JSON body must remount it rather than briefly show the
					    previous log's stale (or failed-to-update) highlighting. */}
					<ShikiCode key={parsed.json} code={parsed.json} lang="json" />
				</>
			)}
		</pre>
	);
}

interface ExtractedJson {
	/** Leading text before the JSON (e.g. "HTTP 401:"), trimmed; "" if none. */
	prefix: string;
	/** The indented JSON body. */
	json: string;
}

/**
 * The leading prefix before a JSON body is always short — an HTTP status
 * ("HTTP 401: ") or a slog label ("[ERROR] ", "WARN [proxy] "). Only brackets
 * within this many leading chars are considered body openers, which bounds the
 * parse attempts: without it, a bracket-heavy non-JSON log would re-parse a
 * suffix at every bracket and could jank the modal on open.
 */
const MAX_PREFIX_SCAN = 256;

/**
 * Pulls a JSON object/array out of `text`, tolerating a leading prefix such as
 * "HTTP 401: " that the proxy prepends to upstream error bodies, or a bracketed
 * log label like "[ERROR] " / "WARN [proxy] ". Each `{`/`[` within the leading
 * {@link MAX_PREFIX_SCAN} chars is tried in order as a body opener; the first
 * whose substring-to-end parses as a whole object or array wins, and the text
 * before it becomes the prefix. Requiring the parse to reach end-of-string means
 * a partial or trailing-junk body falls back to plain text rather than being
 * mangled; bare scalars ("123", "true") have no opener and so stay plain too.
 *
 * The scan returns on the first successful opener, so a well-formed body (the
 * common case) parses once; only pure-prose fallbacks with stray brackets retry,
 * and only across the bounded prefix window.
 */
function extractJson(text: string): ExtractedJson | null {
	const limit = Math.min(text.length, MAX_PREFIX_SCAN);
	for (let start = 0; start < limit; start++) {
		const ch = text[start];
		if (ch !== "{" && ch !== "[") continue;
		try {
			const parsed: unknown = JSON.parse(text.slice(start));
			if (parsed === null || typeof parsed !== "object") continue;
			return {
				prefix: text.slice(0, start).trim(),
				json: JSON.stringify(parsed, null, 2),
			};
		} catch {
			// Not a clean body at this opener; try the next bracket in the window.
		}
	}
	return null;
}
