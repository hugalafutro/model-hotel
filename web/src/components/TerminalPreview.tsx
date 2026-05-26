import type { ReactNode } from "react";
import { CopyButton } from "./CopyButton";
import { LangIcon, type LangIconKey } from "./langIcons";

export type TerminalVariant = "bash" | "powershell" | "code";

interface TerminalPreviewProps {
	/** Terminal chrome style: macOS dots, Windows 11 titlebar, or plain code block */
	variant: TerminalVariant;
	/** Title shown in the titlebar/header. Defaults to variant name */
	title?: string;
	/** Icon shown in the header for "code" variant. Ignored for bash/powershell */
	icon?: LangIconKey;
	/** Plain text copied to clipboard when the CopyButton is clicked */
	copyText: string;
	/** Syntax-highlighted content rendered inside the terminal */
	children: ReactNode;
}

export function TerminalPreview({
	variant,
	title,
	icon,
	copyText,
	children,
}: TerminalPreviewProps) {
	const defaultTitle =
		variant === "bash" ? "bash" : variant === "powershell" ? "PowerShell" : "";
	const displayTitle = title ?? defaultTitle;

	if (variant === "code") {
		return (
			<div className="relative rounded-lg bg-gray-950 border border-gray-800 overflow-hidden">
				<div className="absolute top-2 right-2 z-10">
					<CopyButton
						text={copyText}
						size={14}
						title={`Copy ${displayTitle} snippet`}
					/>
				</div>
				<div className="flex items-center gap-2 px-3 py-2 border-b border-gray-800 bg-gray-900/50">
					{icon && <LangIcon name={icon} size={14} />}
					<span className="text-xs text-gray-400 font-mono">
						{displayTitle}
					</span>
				</div>
				<pre className="p-4 text-xs text-gray-400 font-mono overflow-x-auto terminal-body">
					<code className="terminal-code">{children}</code>
				</pre>
			</div>
		);
	}

	if (variant === "bash") {
		return (
			<div className="relative rounded-b-lg rounded-tr-lg bg-gray-950 border border-gray-800 overflow-hidden min-h-70">
				<div className="absolute top-2 right-2 z-10">
					<CopyButton
						text={copyText}
						size={14}
						title={`Copy ${displayTitle} snippet`}
					/>
				</div>
				<div className="flex items-center gap-1.5 px-3 py-2 border-b border-gray-800 terminal-titlebar">
					<div className="w-2.5 h-2.5 rounded-full bg-red-500" />
					<div className="w-2.5 h-2.5 rounded-full bg-yellow-500" />
					<div className="w-2.5 h-2.5 rounded-full bg-green-500" />
					<span className="text-xs text-gray-600 ml-2 font-mono terminal-titlebar-label">
						{displayTitle}
					</span>
				</div>
				<pre className="p-4 text-xs text-gray-400 font-mono overflow-x-auto terminal-body">
					<code className="terminal-code">{children}</code>
				</pre>
			</div>
		);
	}

	return (
		<div className="terminal-win11 relative rounded-b-lg rounded-tr-lg overflow-hidden border border-[#333] min-h-70">
			<div className="absolute top-2 right-2 z-10">
				<CopyButton
					text={copyText}
					size={14}
					title={`Copy ${displayTitle} snippet`}
				/>
			</div>
			<div className="terminal-win11-titlebar flex items-center gap-2 px-3 py-1.5 border-b border-[#333]">
				<svg
					className="win11-icon"
					viewBox="0 0 24 24"
					width="14"
					height="14"
					fill="currentColor"
				>
					<title>Windows</title>
					<path d="M0 3.449L9.75 2.1v9.45H0m10.95 0H24v9.35L10.95 21.9M0 12.6h9.75v9.15L0 20.1m10.95-9.5H24V2.1L10.95 3.65" />
				</svg>
				<span className="terminal-win11-titlebar-label text-xs font-mono text-[#ccc]">
					{displayTitle}
				</span>
			</div>
			<pre className="terminal-win11-body p-4 text-xs font-mono overflow-x-auto text-[#ccc] bg-[#0c0c0c]">
				<code className="terminal-win11-code">{children}</code>
			</pre>
		</div>
	);
}
