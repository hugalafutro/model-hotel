import { Timer } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

interface SidebarQuotaSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function SidebarQuotaSettings({
	collapsed,
	onToggle,
}: SidebarQuotaSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [quotaDisabled, setQuotaDisabled] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaDisabled") === "true";
		} catch {
			return false;
		}
	});
	const [refreshMin, setRefreshMin] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaRefreshMin") || "5";
		} catch {
			return "5";
		}
	});

	const handleRefreshChange = (val: string) => {
		setRefreshMin(val);
		try {
			localStorage.setItem("sidebarQuotaRefreshMin", val);
		} catch {
			/* ignore */
		}
		window.dispatchEvent(new CustomEvent("sidebarQuotaRefreshChange"));
		toast(
			val === "0"
				? t("settings.sidebarQuota.disabled")
				: t("settings.sidebarQuota.intervalSet", {
						minutes: val,
						count: Number(val),
					}),
			"success",
		);
	};

	return (
		<SettingsSection
			icon={Timer}
			title={t("settings.sidebarQuota.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.sidebarQuota.description")}
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.sidebarQuota.showQuotasPill")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.sidebarQuota.showQuotasPillDescription")}
						</p>
					</div>
					<Toggle
						checked={!quotaDisabled}
						onChange={(v) => {
							const newVal = !v;
							setQuotaDisabled(newVal);
							try {
								localStorage.setItem("sidebarQuotaDisabled", String(newVal));
							} catch {
								/* ignore */
							}
							toast(
								newVal
									? t("settings.sidebarQuota.disabledQuotas")
									: t("settings.sidebarQuota.enabledQuotas"),
								newVal ? "info" : "success",
							);
							window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));
						}}
					/>
				</div>
				<SettingsSelect
					id="quota-refresh-interval"
					label={t("settings.sidebarQuota.refreshInterval")}
					value={refreshMin}
					options={[
						{ value: "1", label: t("settings.sidebarQuota.intervals.1") },
						{ value: "2", label: t("settings.sidebarQuota.intervals.2") },
						{ value: "5", label: t("settings.sidebarQuota.intervals.5") },
						{ value: "10", label: t("settings.sidebarQuota.intervals.10") },
						{ value: "15", label: t("settings.sidebarQuota.intervals.15") },
						{ value: "30", label: t("settings.sidebarQuota.intervals.30") },
						{
							value: "0",
							label: t("settings.sidebarQuota.intervals.disabled"),
						},
					]}
					onChange={handleRefreshChange}
					disabled={quotaDisabled}
					description={t("settings.sidebarQuota.refreshInterval.description")}
				/>
			</div>
		</SettingsSection>
	);
}
