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
}

export const CopyablePill = memo(function CopyablePill({
	text,
	displayText,
	tooltip = "Click to copy",
	className = "",
	textClassName = "",
	iconClassName = "",
	suffix,
}: CopyablePillProps) {
	const { toast } = useToast();

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
		<div className={`flex items-center gap-2 min-w-0 ${className}`}>
			<button
				type="button"
				onClick={handleCopy}
				className="flex items-center gap-1.5 min-w-0 overflow-hidden select-none px-1 py-0.5 rounded hover:bg-gray-700 transition-colors cursor-pointer"
				title={tooltip}
				aria-label={tooltip}
			>
				<span className={`truncate ${textClassName}`}>
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
