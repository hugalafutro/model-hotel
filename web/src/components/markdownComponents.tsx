import type { Components } from "react-markdown";
import { resolveShikiLang } from "../utils/shikiHighlighter";
import { ShikiCode } from "./ShikiCode";

/** Shared markdown renderer components (external links open in new tab,
 *  fenced code blocks are syntax-highlighted by shiki when the fence names
 *  a supported language; inline code and unknown languages stay plain). */
export const markdownComponents: Components = {
	a: ({ children, ...props }) => (
		<a {...props} target="_blank" rel="noopener noreferrer">
			{children}
		</a>
	),
	code: ({ className, children, ...props }) => {
		const lang = /language-([\w#+-]+)/.exec(className ?? "")?.[1];
		if (lang && typeof children === "string" && resolveShikiLang(lang)) {
			return (
				<code className={className} {...props}>
					<ShikiCode code={children} lang={lang} />
				</code>
			);
		}
		return (
			<code className={className} {...props}>
				{children}
			</code>
		);
	},
};
