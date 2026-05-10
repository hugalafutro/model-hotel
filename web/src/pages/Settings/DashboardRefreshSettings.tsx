import { LayoutDashboard } from "lucide-react";
import { SettingsSection } from "../../components/SettingsSection";
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
				<div>
					<label
						htmlFor="dashboard-refresh-interval"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Refresh Interval
					</label>
					<select
						id="dashboard-refresh-interval"
						value={(() => {
							try {
								return localStorage.getItem("dashboardRefreshSec") || "30";
							} catch {
								return "30";
							}
						})()}
						onChange={(e) => {
							const val = e.target.value;
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
						}}
						className="ui-input"
					>
						<option value="10">10 seconds (manual refresh hidden)</option>
						<option value="30">30 seconds (default)</option>
						<option value="60">1 minute</option>
						<option value="120">2 minutes</option>
						<option value="300">5 minutes</option>
						<option value="600">10 minutes</option>
						<option value="0">Disabled (manual only)</option>
					</select>
					<p className="text-gray-500 text-xs mt-1">
						At 10 seconds the manual refresh button is hidden. Changes take
						effect on next navigation to the dashboard.
					</p>
				</div>
			</div>
		</SettingsSection>
	);
}
