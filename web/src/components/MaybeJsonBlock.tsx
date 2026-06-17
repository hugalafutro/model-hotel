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
					<ShikiCode code={parsed.json} lang="json" />
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
 * Pulls a JSON object/array out of `text`, tolerating a leading prefix such as
 * "HTTP 401: " that the proxy prepends to upstream error bodies. The substring
 * from the first `{`/`[` to end-of-string must parse as a whole, so a partial
 * or trailing-junk body falls back to plain text rather than being mangled.
 * Bare scalars ("123", "true") have no brace and so stay plain too.
 */
function extractJson(text: string): ExtractedJson | null {
	const brace = text.indexOf("{");
	const bracket = text.indexOf("[");
	const offsets = [brace, bracket].filter((i) => i >= 0);
	if (offsets.length === 0) return null;
	const start = Math.min(...offsets);
	try {
		const parsed: unknown = JSON.parse(text.slice(start));
		if (parsed === null || typeof parsed !== "object") return null;
		return {
			prefix: text.slice(0, start).trim(),
			json: JSON.stringify(parsed, null, 2),
		};
	} catch {
		return null;
	}
}
