import { Timer } from "lucide-react";
import { useState } from "react";
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
				? "Sidebar quota auto-refresh disabled - use manual refresh"
				: `Quota refresh set to every ${val} minute${val === "1" ? "" : "s"}`,
			"success",
		);
	};

	return (
		<SettingsSection
			icon={Timer}
			title="Sidebar Quotas"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure how often provider quota and balance data is refreshed in
					the sidebar panel.
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Show Quotas Pill
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Display the quota panel in the sidebar
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
									? "Sidebar quotas disabled - pill hidden and auto-refresh paused"
									: "Sidebar quotas enabled - pill visible and auto-refresh resumed",
								newVal ? "info" : "success",
							);
							window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));
						}}
					/>
				</div>
				<SettingsSelect
					id="quota-refresh-interval"
					label="Refresh Interval"
					value={refreshMin}
					options={[
						{ value: "1", label: "1 minute" },
						{ value: "2", label: "2 minutes" },
						{ value: "5", label: "5 minutes (default)" },
						{ value: "10", label: "10 minutes" },
						{ value: "15", label: "15 minutes" },
						{ value: "30", label: "30 minutes" },
						{ value: "0", label: "Disabled (manual only)" },
					]}
					onChange={handleRefreshChange}
					disabled={quotaDisabled}
					description="Minimum 1 minute. Changes take effect on next scheduled refresh."
				/>
			</div>
		</SettingsSection>
	);
}
