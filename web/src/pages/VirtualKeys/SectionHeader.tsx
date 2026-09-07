/** An accent-coloured section heading inside the virtual-key modals. */
export function SectionHeader({
	icon: Icon,
	label,
	spacing = "mt-4 first:mt-0",
}: {
	icon: React.ComponentType<{ size?: number; className?: string }>;
	label: string;
	/** Margin utilities; the default stacks sections inside a modal body. */
	spacing?: string;
}) {
	return (
		<div className={`flex items-center gap-2 text-(--accent) ${spacing}`}>
			<Icon size={12} className="shrink-0" />
			<span className="text-xs font-semibold uppercase tracking-wider">
				{label}
			</span>
		</div>
	);
}
