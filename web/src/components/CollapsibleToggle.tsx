import { ChevronsDownUp, ChevronsUpDown } from "lucide-react";

interface CollapsibleToggleProps {
	collapsed: boolean;
	onToggle: () => void;
	expandTitle?: string;
	collapseTitle?: string;
}

export function CollapsibleToggle({
	collapsed,
	onToggle,
	expandTitle = "Expand controls",
	collapseTitle = "Collapse controls",
}: CollapsibleToggleProps) {
	return (
		<button
			type="button"
			onClick={onToggle}
			className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
			title={collapsed ? expandTitle : collapseTitle}
		>
			{collapsed ? <ChevronsUpDown size={14} /> : <ChevronsDownUp size={14} />}
		</button>
	);
}
