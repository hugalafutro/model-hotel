import { memo, useMemo } from "react";
import ReactMarkdown from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import "katex/dist/katex.min.css";
import { convertLatexDelimiters } from "../utils/latexDelimiters";
import { markdownComponents } from "./markdownComponents";
import { MarkdownStreamingContext } from "./markdownStreamingContext";

export const MARKDOWN_PROSE_CLASSES =
	"prose prose-invert prose-xs max-w-none text-(--text-primary) text-xs " +
	"[&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0.5 " +
	"[&_h1]:text-sm [&_h2]:text-xs [&_h3]:text-xs " +
	"[&_:not(pre)>code]:text-(--accent) [&_:not(pre)>code]:bg-(--surface-hover) [&_:not(pre)>code]:px-1 [&_:not(pre)>code]:py-0.5 [&_:not(pre)>code]:rounded [&_:not(pre)>code]:text-[11px] " +
	"[&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:my-2 [&_pre]:text-[11px] " +
	"[&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-3 [&_blockquote]:text-(--text-secondary) " +
	"[&_strong]:text-white [&_em]:text-(--text-secondary) " +
	"[&_a]:text-(--accent) [&_a]:underline " +
	"[&_hr]:border-(--border-subtle) " +
	"[&_table]:text-[10px] [&_th]:px-1.5 [&_th]:py-0.5 [&_td]:px-1.5 [&_td]:py-0.5 " +
	"[&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle) " +
	"[&_ .katex-display]:overflow-x-auto [&_ .katex-display]:my-2";

interface MarkdownContentProps {
	children: string;
	className?: string;
	/** When true, fenced code blocks render as plain text (no shiki) until the
	 *  stream finishes — avoids re-tokenizing the whole block on every delta. */
	isStreaming?: boolean;
}

export const MarkdownContent = memo(function MarkdownContent({
	children,
	className,
	isStreaming = false,
}: MarkdownContentProps) {
	const processed = useMemo(() => convertLatexDelimiters(children), [children]);

	return (
		<MarkdownStreamingContext.Provider value={isStreaming}>
			<div className={`${MARKDOWN_PROSE_CLASSES} ${className || ""}`}>
				<ReactMarkdown
					remarkPlugins={[remarkGfm, remarkMath]}
					rehypePlugins={[rehypeKatex]}
					components={markdownComponents}
				>
					{processed}
				</ReactMarkdown>
			</div>
		</MarkdownStreamingContext.Provider>
	);
});
