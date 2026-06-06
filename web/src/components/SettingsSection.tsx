import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { CollapsibleToggle } from "./CollapsibleToggle";
import { ResetButton } from "./ResetButton";

export interface SettingsSectionProps {
	icon: LucideIcon;
	title: string;
	collapsed: boolean;
	onToggle: () => void;
	children: React.ReactNode;
	/** When present, renders a section reset button left of the collapse toggle */
	onResetSection?: () => void;
	/** Custom tooltip for the section reset button */
	resetTooltip?: string;
}

export function SettingsSection({
	icon: Icon,
	title,
	collapsed,
	onToggle,
	children,
	onResetSection,
	resetTooltip,
}: SettingsSectionProps) {
	const { t } = useTranslation();

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<div className="flex items-center gap-2">
					<Icon size={18} className="text-(--accent)" />
					<h2 className="text-xl font-semibold text-white">{title}</h2>
				</div>
				<div className="flex items-center gap-1.5">
					{onResetSection && (
						<ResetButton
							tooltip={resetTooltip ?? t("settings.common.resetSection")}
							onClick={onResetSection}
							size={14}
						/>
					)}
					<CollapsibleToggle
						collapsed={collapsed}
						onToggle={onToggle}
						size={16}
						variant="muted"
					/>
				</div>
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
