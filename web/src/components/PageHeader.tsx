import type { LucideIcon } from "@/lib/icons";

export interface PageHeaderProps {
	icon: LucideIcon;
	title: string;
	description?: React.ReactNode;
	badge?: React.ReactNode;
	actions?: React.ReactNode;
}

export function PageHeader({
	icon: Icon,
	title,
	description,
	badge,
	actions,
}: PageHeaderProps) {
	return (
		<div className="flex items-start justify-between">
			<div>
				<div className="flex items-center gap-3 page-header-title-row">
					<Icon size={28} strokeWidth={2} className="text-(--accent)" />
					<h1 className="text-2xl font-bold text-(--text-primary)">{title}</h1>
					{badge}
				</div>
				{description && <div className="text-gray-400 mt-1">{description}</div>}
			</div>
			{actions && <div className="flex items-center gap-2">{actions}</div>}
		</div>
	);
}
