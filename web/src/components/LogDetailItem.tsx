interface DetailItemProps {
	/** Optional leading icon. Accepts lucide icons and custom icon components. */
	icon?: React.ComponentType<{ size?: number; className?: string }>;
	label: string;
	value?: string | number | null;
	mono?: boolean;
	labelExtra?: React.ReactNode;
	children?: React.ReactNode;
	/** Extra classes on the tile wrapper (e.g. col-span-2) */
	className?: string;
	/**
	 * Elevation role. "flat" (default) = recessed metadata tile; "stat" =
	 * raised hero-metric tile. Each maps to a themed depth class so the same
	 * markup gains hierarchy without per-theme branching here.
	 */
	emphasis?: "flat" | "stat";
}

export function DetailItem({
	icon: Icon,
	label,
	value,
	mono = false,
	labelExtra,
	children,
	className = "",
	emphasis = "flat",
}: DetailItemProps) {
	const displayValue =
		value === null || value === undefined || value === "" ? "-" : value;
	const tileClass = emphasis === "stat" ? "ui-stat-tile" : "ui-detail-tile";

	return (
		<div className={`flex items-start gap-3 p-3 ${tileClass} ${className}`}>
			{Icon && (
				<div className="shrink-0 mt-0.5">
					<Icon size={16} className="text-(--accent)" />
				</div>
			)}
			<div className="flex-1 min-w-0">
				<div className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-(--text-tertiary) font-medium mb-1">
					{label}
					{/* Pushed to the row's right edge so actions (e.g. the Message
					   copy pill) don't read as part of the label text. */}
					{labelExtra && <span className="ml-auto">{labelExtra}</span>}
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
