import type { SVGProps } from "react";

export type LangIconKey =
	| "javascript"
	| "python"
	| "claude"
	| "openclaw"
	| "hermes"
	| "librechat";

interface LangIconProps extends SVGProps<SVGSVGElement> {
	name: LangIconKey;
	size?: number;
}

/** Small language/tool icons for code example headers. */
export function LangIcon({ name, size = 14, ...rest }: LangIconProps) {
	switch (name) {
		case "javascript":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>JavaScript</title>
					<rect width="24" height="24" rx="4" fill="#F7DF1E" />
					<path
						d="M6 18.5l2.1-1.3c.4.7.8 1.4 1.6 1.4.8 0 1.3-.3 1.3-1.9V10h2.6v6.8c0 2.7-1.6 3.9-3.8 3.9-2.1 0-3.3-1.1-3.8-2.2zm9-0.2l2.1-1.2c.6 1 1.3 1.7 2.6 1.7 1.1 0 1.8-.5 1.8-1.3 0-.9-.7-1.2-1.9-1.7l-.7-.3c-1.9-.8-3.2-1.8-3.2-3.9 0-2 1.5-3.5 3.9-3.5 1.7 0 2.9.6 3.8 2.1l-2.1 1.3c-.5-.8-1-1.1-1.7-1.1-.8 0-1.3.5-1.3 1.1 0 .8.5 1.1 1.6 1.6l.7.3c2.3 1 3.6 2 3.6 4.2 0 2.4-1.9 3.7-4.4 3.7-2.5 0-4.1-1.2-4.8-2.7"
						fill="#000"
					/>
				</svg>
			);

		case "python":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>Python</title>
					<path
						d="M12 2C7.6 2 7 4 7 4v3h5v1H5.5S2 7.5 2 12s3 5.5 3 5.5H7V14c0-2.2 1.8-4 4-4h4c1.7 0 3-1.3 3-3V4s0-2-6-2zm-2 2a1 1 0 110 2 1 1 0 010-2z"
						fill="#3776AB"
					/>
					<path
						d="M12 22c4.4 0 5-2 5-2v-3h-5v-1h6.5s3.5.5 3.5-4-3-5.5-3-5.5H17V10c0 2.2-1.8 4-4 4h-4c-1.7 0-3 1.3-3 3v4s0 2 6 2zm2-2a1 1 0 110-2 1 1 0 010 2z"
						fill="#FFD43B"
					/>
				</svg>
			);

		case "claude":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>Claude Code</title>
					<circle cx="12" cy="12" r="10" fill="#D97706" opacity="0.2" />
					<path
						d="M12 3l2.5 5 5.5.8-4 3.9.9 5.3L12 15l-4.9 2 .9-5.3-4-3.9 5.5-.8z"
						fill="#D97706"
					/>
				</svg>
			);

		case "openclaw":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>OpenClaw</title>
					<circle cx="12" cy="12" r="10" fill="#E05D44" opacity="0.2" />
					<ellipse cx="8" cy="9" rx="2.5" ry="3" fill="#E05D44" />
					<ellipse cx="16" cy="9" rx="2.5" ry="3" fill="#E05D44" />
					<ellipse cx="12" cy="7" rx="2.5" ry="3" fill="#E05D44" />
					<ellipse cx="10" cy="13.5" rx="4" ry="2" fill="#E05D44" />
					<ellipse cx="14" cy="13.5" rx="4" ry="2" fill="#E05D44" />
					<circle cx="10.5" cy="9" r="1" fill="#1a1a2e" />
					<circle cx="13.5" cy="9" r="1" fill="#1a1a2e" />
				</svg>
			);

		case "hermes":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>Hermes</title>
					<circle cx="12" cy="12" r="10" fill="#6366F1" opacity="0.2" />
					<path
						d="M12 4c-2 0-3.5 1-3.5 1l1 2c.8-.5 1.6-.7 2.5-.7s1.7.2 2.5.7l1-2S14 4 12 4zm0 4l-6 6h4v4h4v-4h4l-6-6z"
						fill="#6366F1"
					/>
				</svg>
			);

		case "librechat":
			return (
				<svg
					viewBox="0 0 24 24"
					width={size}
					height={size}
					fill="none"
					{...rest}
				>
					<title>LibreChat</title>
					<circle cx="12" cy="12" r="10" fill="#7C3AED" opacity="0.2" />
					<path d="M7 8h10v2H7zm0 3h7v2H7zm0 3h10v2H7z" fill="#7C3AED" />
					<path
						d="M4 5h16v14H4z"
						stroke="#7C3AED"
						strokeWidth="1.5"
						fill="none"
						strokeLinecap="round"
						strokeLinejoin="round"
					/>
				</svg>
			);
	}
}
