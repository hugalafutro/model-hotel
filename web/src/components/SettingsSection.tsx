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
					/>
				</div>
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
					collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
				}`}
			>
				{/* The clip wrapper the 0fr/1fr trick needs would also clip the
				    Terminal theme's hover glow on buttons flush with the content
				    edge, so when expanded the clip box gets a 1rem bleed (p-4
				    cancelled by -m-4, so layout is unchanged). Collapsed keeps
				    the tight box: padding is unsqueezable, so a bleed there
				    would leave a visible band. */}
				<div className={`overflow-hidden ${collapsed ? "" : "p-4 -m-4"}`}>
					{children}
				</div>
			</div>
		</div>
	);
}
