import { LayoutDashboard } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { useToast } from "../../context/ToastContext";

interface DashboardRefreshSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DashboardRefreshSettings({
	collapsed,
	onToggle,
}: DashboardRefreshSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [refreshSec, setRefreshSec] = useState(() => {
		try {
			return localStorage.getItem("dashboardRefreshSec") || "30";
		} catch {
			return "30";
		}
	});

	const handleChange = (val: string) => {
		setRefreshSec(val);
		try {
			localStorage.setItem("dashboardRefreshSec", val);
		} catch {
			/* ignore */
		}
		window.dispatchEvent(new CustomEvent("dashboardRefreshChange"));
		toast(
			val === "0"
				? t("settings.dashboard.disabled")
				: t("settings.dashboard.intervalSet", {
						seconds: val,
						count: Number(val),
					}),
			"success",
		);
	};

	return (
		<SettingsSection
			icon={LayoutDashboard}
			title={t("settings.dashboard.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.dashboard.description")}
				</p>
				<SettingsSelect
					id="dashboard-refresh-interval"
					label={t("settings.dashboard.refreshInterval")}
					value={refreshSec}
					options={[
						{
							value: "10",
							label: t("settings.dashboard.intervals.10"),
						},
						{
							value: "30",
							label: t("settings.dashboard.intervals.30"),
						},
						{
							value: "60",
							label: t("settings.dashboard.intervals.60"),
						},
						{
							value: "120",
							label: t("settings.dashboard.intervals.120"),
						},
						{
							value: "300",
							label: t("settings.dashboard.intervals.300"),
						},
						{
							value: "600",
							label: t("settings.dashboard.intervals.600"),
						},
						{
							value: "0",
							label: t("settings.dashboard.intervals.disabled"),
						},
					]}
					onChange={handleChange}
					description={t("settings.dashboard.refreshInterval.description")}
				/>
			</div>
		</SettingsSection>
	);
}
