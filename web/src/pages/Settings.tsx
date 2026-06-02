import { useQuery } from "@tanstack/react-query";
import { Settings as SettingsIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { useCollapsible } from "../components/CollapsibleToggle";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { PageHeader } from "../components/PageHeader";
import { AppearanceSettings } from "./Settings/AppearanceSettings";
import { CircuitBreakerSettings } from "./Settings/CircuitBreakerSettings";
import { DatabaseBackupSettings } from "./Settings/DatabaseBackupSettings";
import { DataStorageSettings } from "./Settings/DataStorageSettings";
import { DiscoverySettings } from "./Settings/DiscoverySettings";
import { PasskeySettings } from "./Settings/PasskeySettings";
import { ProxySettings } from "./Settings/ProxySettings";
import { RateLimitSettings } from "./Settings/RateLimitSettings";

export function Settings() {
	const { t } = useTranslation();
	const { collapsed: modelDiscoveryCollapsed, toggle: toggleModelDiscovery } =
		useCollapsible("settings_modelDiscoveryCollapsed");
	const { collapsed: appearanceCollapsed, toggle: toggleAppearance } =
		useCollapsible("settings_appearanceCollapsed");
	const { collapsed: dataStorageCollapsed, toggle: toggleDataStorage } =
		useCollapsible("settings_dataStorageCollapsed");
	const { collapsed: backupCollapsed, toggle: toggleBackup } = useCollapsible(
		"settings_backupCollapsed",
	);
	const { collapsed: rateLimitCollapsed, toggle: toggleRateLimit } =
		useCollapsible("settings_rateLimitCollapsed");
	const { collapsed: circuitBreakerCollapsed, toggle: toggleCircuitBreaker } =
		useCollapsible("settings_circuitBreakerCollapsed");
	const { collapsed: proxyCollapsed, toggle: toggleProxy } = useCollapsible(
		"settings_proxyCollapsed",
	);
	const { collapsed: passkeyCollapsed, toggle: togglePasskey } = useCollapsible(
		"settings_passkeyCollapsed",
	);

	const { isLoading } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-8 max-w-5xl pb-8">
			<PageHeader
				icon={SettingsIcon}
				title={t("settings.title")}
				description={t("settings.description")}
			/>

			<div className="space-y-6">
				<DiscoverySettings
					collapsed={modelDiscoveryCollapsed}
					onToggle={toggleModelDiscovery}
				/>

				<PasskeySettings
					collapsed={passkeyCollapsed}
					onToggle={togglePasskey}
				/>

				<AppearanceSettings
					collapsed={appearanceCollapsed}
					onToggle={toggleAppearance}
				/>

				<DataStorageSettings
					collapsed={dataStorageCollapsed}
					onToggle={toggleDataStorage}
				/>

				<DatabaseBackupSettings
					collapsed={backupCollapsed}
					onToggle={toggleBackup}
				/>

				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
				/>

				<CircuitBreakerSettings
					collapsed={circuitBreakerCollapsed}
					onToggle={toggleCircuitBreaker}
				/>

				<ProxySettings collapsed={proxyCollapsed} onToggle={toggleProxy} />
			</div>
		</div>
	);
}
