import { memo } from "react";
import { useToast } from "../context/ToastContext";

interface CopyablePillProps {
	text: string;
	displayText?: string;
	tooltip?: string;
	className?: string;
	textClassName?: string;
	iconClassName?: string;
	suffix?: React.ReactNode;
	/** Max lines before clamping. 1 = truncate (default), 2+ = line-clamp. */
	lines?: number;
}

export const CopyablePill = memo(function CopyablePill({
	text,
	displayText,
	tooltip,
	className = "",
	textClassName = "",
	iconClassName = "",
	suffix,
	lines = 1,
}: CopyablePillProps) {
	const { toast } = useToast();

	// Title shows full text for sighted users (visible on hover when truncated).
	// aria-label provides a short action description for screen readers.
	const ariaLabel = tooltip ?? `Copy ${text}`;
	const effectiveTitle = text;

	const handleCopy = (e: React.MouseEvent) => {
		e.stopPropagation();
		navigator.clipboard
			.writeText(text)
			.then(() => {
				toast("Copied!", "info");
			})
			.catch(() => {
				toast("Failed to copy", "error");
			});
	};

	return (
		<div
			className={`flex ${lines > 1 ? "items-start w-full" : "items-center"} gap-2 min-w-0 ${className}`}
		>
			<button
				type="button"
				onClick={handleCopy}
				className={`flex ${lines > 1 ? "items-start w-full" : "items-center"} gap-1.5 min-w-0 ${lines > 1 ? "" : "overflow-hidden"} select-none text-left pl-0 pr-1 py-0.5 rounded hover:bg-gray-700 transition-colors cursor-pointer`}
				title={effectiveTitle}
				aria-label={ariaLabel}
			>
				<span
					className={`${lines === 1 ? "truncate" : ""} ${textClassName}`}
					{...(lines > 1
						? {
								style: {
									display: "-webkit-box",
									WebkitLineClamp: lines,
									WebkitBoxOrient: "vertical" as const,
									overflow: "hidden",
								},
							}
						: {})}
				>
					{displayText || text}
				</span>
				<svg
					className={`w-3.5 h-3.5 text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all shrink-0 ${iconClassName}`}
					fill="none"
					stroke="currentColor"
					viewBox="0 0 24 24"
				>
					<title>Copy to clipboard</title>
					<path
						strokeLinecap="round"
						strokeLinejoin="round"
						strokeWidth={2}
						d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
					/>
				</svg>
			</button>
			{suffix}
		</div>
	);
});
