import { Copy } from "lucide-react";
import { useToast } from "../context/ToastContext";

interface CopyButtonProps {
	text: string;
	size?: number;
	className?: string;
	title?: string;
}

export function CopyButton({
	text,
	size = 10,
	className = "inline-flex items-center cursor-pointer transition-all text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_4px_var(--accent)]",
	title = "Copy",
}: CopyButtonProps) {
	const { toast } = useToast();
	return (
		<button
			type="button"
			className={className}
			onClick={() => {
				navigator.clipboard
					.writeText(text)
					.then(() => toast("Copied to clipboard", "info"))
					.catch(() => toast("Failed to copy", "error"));
			}}
			title={title}
		>
			<Copy size={size} />
		</button>
	);
}
