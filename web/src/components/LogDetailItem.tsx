import type { LucideIcon } from "lucide-react";

interface DetailItemProps {
	icon: LucideIcon;
	label: string;
	value?: string | number | null;
	mono?: boolean;
	labelExtra?: React.ReactNode;
	children?: React.ReactNode;
	/** Extra classes on the tile wrapper (e.g. col-span-2) */
	className?: string;
}

export function DetailItem({
	icon: Icon,
	label,
	value,
	mono = false,
	labelExtra,
	children,
	className = "",
}: DetailItemProps) {
	const displayValue =
		value === null || value === undefined || value === "" ? "-" : value;

	return (
		<div
			className={`flex items-start gap-3 p-3 rounded-(--radius-box) bg-(--surface-bg) border border-(--border-subtle) ${className}`}
		>
			<div className="shrink-0 mt-0.5">
				<Icon size={16} className="text-(--accent)" />
			</div>
			<div className="flex-1 min-w-0">
				<div className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-(--text-tertiary) font-medium mb-1">
					{label}
					{labelExtra}
				</div>
				{children ? (
					children
				) : (
					<div
						className={`text-sm text-(--text-primary) ${mono ? "font-mono truncate" : "break-words"}`}
					>
						{displayValue}
					</div>
				)}
			</div>
		</div>
	);
}
