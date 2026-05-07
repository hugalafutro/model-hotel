import type { LucideIcon } from "lucide-react";
import { CollapsibleToggle } from "./CollapsibleToggle";

export interface SettingsSectionProps {
	icon: LucideIcon;
	title: string;
	collapsed: boolean;
	onToggle: () => void;
	children: React.ReactNode;
}

export function SettingsSection({
	icon: Icon,
	title,
	collapsed,
	onToggle,
	children,
}: SettingsSectionProps) {
	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div className="flex items-center gap-2">
					<Icon size={18} className="text-(--accent)" />
					<h2 className="text-xl font-semibold text-white">{title}</h2>
				</div>
				<CollapsibleToggle
					collapsed={collapsed}
					onToggle={onToggle}
					size={16}
					variant="muted"
				/>
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
					collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
				}`}
			>
				<div className="overflow-hidden">{children}</div>
			</div>
		</div>
	);
}
