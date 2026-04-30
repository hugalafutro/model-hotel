import { memo } from "react";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

export const MARKDOWN_PROSE_CLASSES =
	"prose prose-invert prose-xs max-w-none text-(--text-primary) text-xs " +
	"[&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0.5 " +
	"[&_h1]:text-sm [&_h2]:text-xs [&_h3]:text-xs " +
	"[&_code]:text-(--accent) [&_code]:bg-(--surface-hover) [&_code]:px-1 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-[11px] " +
	"[&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:my-2 [&_pre]:text-[11px] " +
	"[&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-3 [&_blockquote]:text-(--text-secondary) " +
	"[&_strong]:text-white [&_em]:text-(--text-secondary) " +
	"[&_a]:text-(--accent) [&_a]:underline " +
	"[&_hr]:border-(--border-subtle) " +
	"[&_table]:text-[10px] [&_th]:px-1.5 [&_th]:py-0.5 [&_td]:px-1.5 [&_td]:py-0.5 " +
	"[&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle)";

interface MarkdownContentProps {
	children: string;
	className?: string;
}

const markdownComponents: Components = {
	a: ({ children, ...props }) => (
		<a {...props} target="_blank" rel="noopener noreferrer">
			{children}
		</a>
	),
};

export const MarkdownContent = memo(function MarkdownContent({ children, className }: MarkdownContentProps) {
	return (
		<div className={`${MARKDOWN_PROSE_CLASSES} ${className || ""}`}>
			<ReactMarkdown
				remarkPlugins={[remarkGfm]}
				components={markdownComponents}
			>
				{children}
			</ReactMarkdown>
		</div>
	);
});