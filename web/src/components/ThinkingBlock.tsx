import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import { memo, useState } from "react";

export const ThinkingBlock = memo(function ThinkingBlock({
	thinking,
	isStreaming,
}: {
	thinking: string;
	isStreaming: boolean;
}) {
	const [open, setOpen] = useState(false);

	return (
		<>
			<button
				type="button"
				onClick={() => setOpen(!open)}
				className={`flex items-center gap-1.5 text-xs transition-colors mb-2 w-full text-left ${
					isStreaming
						? "text-(--accent) animate-pulse cursor-pointer"
						: "text-(--accent)/70 hover:text-(--accent) cursor-pointer"
				}`}
			>
				<Brain size={12} />
				<span>Thinking</span>
				{open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
			</button>
			{open && (
				<div className="mb-3 px-3 py-2 rounded-lg bg-(--accent)/5 border border-(--accent)/10 text-xs text-(--text-secondary) whitespace-pre-wrap max-h-60 overflow-y-auto">
					{thinking.replace(/^\n+/, "")}
				</div>
			)}
		</>
	);
});
