import { memo, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Brain, ChevronDown, ChevronRight } from "@/lib/icons";

export const ThinkingBlock = memo(function ThinkingBlock({
	thinking,
	isStreaming,
}: {
	thinking: string;
	isStreaming: boolean;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const contentRef = useRef<HTMLDivElement>(null);

	// Auto-scroll thinking content during streaming
	const thinkingLen = thinking.length;
	// biome-ignore lint/correctness/useExhaustiveDependencies: thinkingLen triggers re-scroll on streaming updates
	useEffect(() => {
		if (!isStreaming || !open) return;
		const el = contentRef.current;
		if (!el) return;
		const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;
		if (nearBottom) {
			el.scrollTop = el.scrollHeight;
		}
	}, [thinkingLen, isStreaming, open]);

	// On unroll during streaming, scroll to the bottom immediately
	// so the user sees the latest content instead of staring at the top.
	const wasOpenRef = useRef(false);
	useEffect(() => {
		if (!open || !isStreaming) {
			wasOpenRef.current = !!open;
			return;
		}
		if (!wasOpenRef.current) {
			// Just opened while streaming — scroll to bottom
			requestAnimationFrame(() => {
				const el = contentRef.current;
				if (el) el.scrollTop = el.scrollHeight;
			});
		}
		wasOpenRef.current = true;
	}, [open, isStreaming]);

	return (
		<>
			<button
				type="button"
				onClick={() => setOpen(!open)}
				className={`inline-flex items-center gap-1.5 text-xs transition-colors mb-2 ${
					isStreaming
						? "text-(--accent) animate-pulse"
						: "ui-link-accent text-(--accent)/70"
				}`}
			>
				<Brain size={12} />
				<span>{t("components.thinkingBlock.thinking")}</span>
				{open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
			</button>
			{open && (
				<div
					ref={contentRef}
					className="mb-3 px-3 py-2 rounded-lg bg-(--accent)/5 border border-(--accent)/10 text-xs text-(--text-secondary) whitespace-pre-wrap max-h-60 overflow-y-auto"
				>
					{thinking.replace(/^\n+/, "")}
				</div>
			)}
		</>
	);
});
