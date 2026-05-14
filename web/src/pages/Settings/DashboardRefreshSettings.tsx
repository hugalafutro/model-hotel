import { LayoutDashboard } from "lucide-react";
import { useState } from "react";
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
				? "Dashboard auto-refresh disabled - use manual refresh"
				: `Dashboard refresh set to every ${val} second${val === "1" ? "" : "s"}`,
			"success",
		);
	};

	return (
		<SettingsSection
			icon={LayoutDashboard}
			title="Dashboard Refresh"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure how often the dashboard stats and charts are refreshed
					automatically. Manual refresh button is hidden when set to 10 seconds
					or faster.
				</p>
				<SettingsSelect
					id="dashboard-refresh-interval"
					label="Refresh Interval"
					value={refreshSec}
					options={[
						{ value: "10", label: "10 seconds (manual refresh hidden)" },
						{ value: "30", label: "30 seconds (default)" },
						{ value: "60", label: "1 minute" },
						{ value: "120", label: "2 minutes" },
						{ value: "300", label: "5 minutes" },
						{ value: "600", label: "10 minutes" },
						{ value: "0", label: "Disabled (manual only)" },
					]}
					onChange={handleChange}
					description="At 10 seconds the manual refresh button is hidden. Changes take effect on next navigation to the dashboard."
				/>
			</div>
		</SettingsSection>
	);
}
