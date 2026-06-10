import { Fragment, useEffect, useState } from "react";
import type { ThemedToken } from "shiki/core";
import {
	getSnippetHighlighter,
	resolveShikiLang,
} from "../utils/shikiHighlighter";
import { splitLineByHighlights } from "../utils/snippetHighlights";

interface ShikiCodeProps {
	/** The plain-text snippet (same string the copy button uses). */
	code: string;
	/** Grammar to tokenize with — a canonical shiki id or a common alias
	 *  (e.g. a markdown fence string like "py"). Unsupported languages render
	 *  as plain text. */
	lang: string;
	/** Substrings to emphasize with the white terminal-highlight style
	 *  (instance origin, YOUR_API_KEY, model id). */
	highlights?: string[];
}

/**
 * Renders a code snippet syntax-highlighted by shiki (dark-plus theme) with
 * the user-replaceable parts emphasized in white. Renders the plain text
 * until the lazily-loaded highlighter resolves, so content is available
 * immediately; if highlighting fails the plain text simply remains.
 */
export function ShikiCode({ code, lang, highlights = [] }: ShikiCodeProps) {
	const [tokens, setTokens] = useState<ThemedToken[][] | null>(null);

	useEffect(() => {
		let cancelled = false;
		getSnippetHighlighter(lang)
			.then((highlighter) => {
				if (cancelled || !highlighter) return;
				const canonical = resolveShikiLang(lang);
				if (!canonical) return;
				setTokens(highlighter.codeToTokensBase(code, { lang: canonical }));
			})
			.catch(() => {
				// Keep the plain-text fallback.
			});
		return () => {
			cancelled = true;
		};
	}, [code, lang]);

	if (!tokens) return code;

	return tokens.map((line, lineIdx) => (
		// biome-ignore lint/suspicious/noArrayIndexKey: lines are static per `code`
		<Fragment key={lineIdx}>
			{lineIdx > 0 && "\n"}
			{splitLineByHighlights(line, highlights).map((seg, segIdx) =>
				seg.highlighted ? (
					<span
						// biome-ignore lint/suspicious/noArrayIndexKey: segments are static per `code`
						key={segIdx}
						className="text-white font-semibold terminal-highlight"
					>
						{seg.content}
					</span>
				) : (
					<span
						// biome-ignore lint/suspicious/noArrayIndexKey: segments are static per `code`
						key={segIdx}
						style={seg.color ? { color: seg.color } : undefined}
					>
						{seg.content}
					</span>
				),
			)}
		</Fragment>
	));
}
