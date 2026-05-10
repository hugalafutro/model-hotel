import { useQuery } from "@tanstack/react-query";
import { Settings as SettingsIcon } from "lucide-react";
import { api } from "../api/client";
import { useCollapsible } from "../components/CollapsibleToggle";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { PageHeader } from "../components/PageHeader";
import { AppearanceSettings } from "./Settings/AppearanceSettings";
import { DashboardRefreshSettings } from "./Settings/DashboardRefreshSettings";
import { DatabaseBackupSettings } from "./Settings/DatabaseBackupSettings";
import { DataStorageSettings } from "./Settings/DataStorageSettings";
import { DiscoverySettings } from "./Settings/DiscoverySettings";
import { LoggingSettings } from "./Settings/LoggingSettings";
import { ProxySettings } from "./Settings/ProxySettings";
import { RateLimitSettings } from "./Settings/RateLimitSettings";
import { SidebarQuotaSettings } from "./Settings/SidebarQuotaSettings";
import { ToastSettings } from "./Settings/ToastSettings";

export function Settings() {
	const { collapsed: modelDiscoveryCollapsed, toggle: toggleModelDiscovery } =
		useCollapsible("settings_modelDiscoveryCollapsed");
	const { collapsed: appearanceCollapsed, toggle: toggleAppearance } =
		useCollapsible("settings_appearanceCollapsed");
	const { collapsed: toastCollapsed, toggle: toggleToast } = useCollapsible(
		"settings_toastCollapsed",
	);
	const { collapsed: sidebarQuotaCollapsed, toggle: toggleSidebarQuota } =
		useCollapsible("settings_sidebarQuotaCollapsed");
	const { collapsed: dashboardCollapsed, toggle: toggleDashboard } =
		useCollapsible("settings_dashboardCollapsed");
	const { collapsed: dataStorageCollapsed, toggle: toggleDataStorage } =
		useCollapsible("settings_dataStorageCollapsed");
	const { collapsed: backupCollapsed, toggle: toggleBackup } = useCollapsible(
		"settings_backupCollapsed",
	);
	const { collapsed: loggingCollapsed, toggle: toggleLogging } = useCollapsible(
		"settings_loggingCollapsed",
	);
	const { collapsed: rateLimitCollapsed, toggle: toggleRateLimit } =
		useCollapsible("settings_rateLimitCollapsed");
	const { collapsed: proxyCollapsed, toggle: toggleProxy } = useCollapsible(
		"settings_proxyCollapsed",
	);

	const { isLoading } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-8 max-w-5xl">
			<PageHeader
				icon={SettingsIcon}
				title="Settings"
				description="Configure your Model Hotel instance"
			/>

			<div className="space-y-6">
				<DiscoverySettings
					collapsed={modelDiscoveryCollapsed}
					onToggle={toggleModelDiscovery}
				/>

				<AppearanceSettings
					collapsed={appearanceCollapsed}
					onToggle={toggleAppearance}
				/>

				<ToastSettings collapsed={toastCollapsed} onToggle={toggleToast} />

				<SidebarQuotaSettings
					collapsed={sidebarQuotaCollapsed}
					onToggle={toggleSidebarQuota}
				/>

				<DashboardRefreshSettings
					collapsed={dashboardCollapsed}
					onToggle={toggleDashboard}
				/>

				<DataStorageSettings
					collapsed={dataStorageCollapsed}
					onToggle={toggleDataStorage}
				/>

				<DatabaseBackupSettings
					collapsed={backupCollapsed}
					onToggle={toggleBackup}
				/>

				<LoggingSettings
					collapsed={loggingCollapsed}
					onToggle={toggleLogging}
				/>

				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
				/>

				<ProxySettings collapsed={proxyCollapsed} onToggle={toggleProxy} />
			</div>
		</div>
	);
}
