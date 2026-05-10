import { Timer } from "lucide-react";
import { useState } from "react";
import { SettingsSection } from "../../components/SettingsSection";
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
				<div>
					<label
						htmlFor="quota-refresh-interval"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Refresh Interval
					</label>
					<select
						id="quota-refresh-interval"
						disabled={quotaDisabled}
						value={(() => {
							try {
								return localStorage.getItem("sidebarQuotaRefreshMin") || "5";
							} catch {
								return "5";
							}
						})()}
						onChange={(e) => {
							const val = e.target.value;
							try {
								localStorage.setItem("sidebarQuotaRefreshMin", val);
							} catch {
								/* ignore */
							}
							window.dispatchEvent(
								new CustomEvent("sidebarQuotaRefreshChange"),
							);
							toast(
								val === "0"
									? "Sidebar quota auto-refresh disabled - use manual refresh"
									: `Quota refresh set to every ${val} minute${val === "1" ? "" : "s"}`,
								"success",
							);
						}}
						className="ui-input disabled:opacity-50 disabled:cursor-not-allowed"
					>
						<option value="1">1 minute</option>
						<option value="2">2 minutes</option>
						<option value="5">5 minutes (default)</option>
						<option value="10">10 minutes</option>
						<option value="15">15 minutes</option>
						<option value="30">30 minutes</option>
						<option value="0">Disabled (manual only)</option>
					</select>
					<p className="text-gray-500 text-xs mt-1">
						Minimum 1 minute. Changes take effect on next scheduled refresh.
					</p>
				</div>
			</div>
		</SettingsSection>
	);
}
