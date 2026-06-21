/** Group header for the data-dense detail modals: a small accent-iconed label
 * trailed by a hairline rule, so each cluster of tiles/badges reads as its own
 * zone instead of floating in the middle of nothing. Shared by RequestLogDetail,
 * ModelDetailModal, etc. */
export function DetailSectionHeader({
	icon: Icon,
	children,
}: {
	icon: React.ComponentType<{ size?: number; className?: string }>;
	children: React.ReactNode;
}) {
	return (
		<div className="flex items-center gap-2 mb-3">
			<Icon size={13} className="text-(--accent)" />
			<span className="text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
				{children}
			</span>
			<div className="h-px flex-1 bg-(--border-default)" />
		</div>
	);
}
