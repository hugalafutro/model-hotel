interface DetailItemProps {
	/** Optional leading icon. Accepts lucide icons and custom icon components. */
	icon?: React.ComponentType<{ size?: number; className?: string }>;
	label: React.ReactNode;
	value?: string | number | null;
	mono?: boolean;
	labelExtra?: React.ReactNode;
	/** Id of the control rendered as children; the label then names it. */
	htmlFor?: string;
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
	htmlFor,
	children,
	className = "",
	emphasis = "flat",
}: DetailItemProps) {
	const displayValue =
		value === null || value === undefined || value === "" ? "-" : value;
	const tileClass = emphasis === "stat" ? "ui-stat-tile" : "ui-detail-tile";
	const labelClass =
		"flex items-center gap-2 min-h-4 text-[11px] leading-4 uppercase tracking-wider text-(--text-tertiary) font-medium mb-1";

	return (
		<div className={`flex items-start gap-3 p-3 ${tileClass} ${className}`}>
			{Icon && (
				// The same 16px box as the label's line, so the icon's centre and
				// the label text's centre sit on one axis in every tile.
				<div className="shrink-0 h-4 flex items-center">
					<Icon size={16} className="text-(--accent)" />
				</div>
			)}
			<div className="flex-1 min-w-0">
				<div className={labelClass}>
					{/* Only the label text names the control: an info hint or an
					   action in labelExtra must not join the accessible name, and a
					   click on it must not land in the input. */}
					{htmlFor ? <label htmlFor={htmlFor}>{label}</label> : label}
					{/* Pushed to the row's end edge so actions (e.g. the Message
					   copy pill) don't read as part of the label text. */}
					{labelExtra && <span className="ms-auto">{labelExtra}</span>}
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
