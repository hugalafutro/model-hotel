import { CollapsibleIcon } from "./CollapsibleToggle";

/** Group header for the data-dense detail modals: a small accent-iconed label
 * trailed by a hairline rule, so each cluster of tiles/badges reads as its own
 * zone instead of floating in the middle of nothing. Shared by RequestLogDetail,
 * ModelDetailModal, etc. Given onToggle it is also the zone's collapse toggle,
 * with the same chevron as the request log's collapsible sections. */
export function DetailSectionHeader({
	icon: Icon,
	children,
	collapsed,
	onToggle,
	testId,
}: {
	icon: React.ComponentType<{ size?: number; className?: string }>;
	children: React.ReactNode;
	collapsed?: boolean;
	onToggle?: () => void;
	testId?: string;
}) {
	const content = (
		<>
			<Icon size={13} className="text-(--accent)" />
			<span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-(--text-tertiary)">
				{children}
			</span>
			<div className="h-px flex-1 bg-(--border-default)" />
		</>
	);
	if (!onToggle) {
		return <div className="flex items-center gap-2 mb-3">{content}</div>;
	}
	return (
		<button
			type="button"
			onClick={onToggle}
			aria-expanded={!collapsed}
			data-testid={testId}
			className="group flex w-full items-center gap-2 mb-3"
		>
			{content}
			<span className="ui-icon-btn ui-icon-btn-in-group p-1 rounded-md">
				<CollapsibleIcon collapsed={collapsed ?? false} iconStyle="double" />
			</span>
		</button>
	);
}
