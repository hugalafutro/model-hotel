import type { Components } from "react-markdown";

/** Shared markdown renderer components (external links open in new tab) */
export const markdownComponents: Components = {
	a: ({ children, ...props }) => (
		<a {...props} target="_blank" rel="noopener noreferrer">
			{children}
		</a>
	),
};
