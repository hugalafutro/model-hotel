import { useTranslation } from "react-i18next";
import { Activity, Braces, Gauge, type LucideIcon } from "@/lib/icons";
import { SettingsSection } from "../../components/SettingsSection";
import { Toggle } from "../../components/Toggle";
import { useSettingsMutations } from "./useSettingsMutations";

interface ObservabilitySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

interface Exporter {
	id: string;
	icon: LucideIcon;
	enabled: boolean;
	name: string;
	description: string;
	/** Environment variable shown in the enable instructions when disabled. */
	envVar: string;
	instructions: string;
}

/**
 * ObservabilitySettings is a read-only status panel for the three log-export
 * integrations (JSON stdout, Prometheus /metrics, OTLP logs). Each is enabled
 * via its own environment variable and resolved server-side; the toggles here
 * only REFLECT that state (they are disabled), and show enable instructions
 * when an exporter is off. There is nothing to persist, so this section has no
 * reset affordance (mirrors PasskeySettings/AppearanceSettings).
 */
export function ObservabilitySettings({
	collapsed,
	onToggle,
}: ObservabilitySettingsProps) {
	const { t } = useTranslation();
	const { settings } = useSettingsMutations();

	const exporters: Exporter[] = [
		{
			id: "json",
			icon: Braces,
			enabled: settings?.log_export_json === "true",
			name: t("settings.observability.json.name"),
			description: t("settings.observability.json.description"),
			envVar: "LOG_FORMAT=json",
			instructions: t("settings.observability.json.instructions"),
		},
		{
			id: "metrics",
			icon: Gauge,
			enabled: settings?.log_export_metrics === "true",
			name: t("settings.observability.metrics.name"),
			description: t("settings.observability.metrics.description"),
			envVar: "METRICS_TOKEN=<token>",
			instructions: t("settings.observability.metrics.instructions"),
		},
		{
			id: "otel",
			icon: Activity,
			enabled: settings?.log_export_otel === "true",
			name: t("settings.observability.otel.name"),
			description: t("settings.observability.otel.description"),
			envVar: "OTEL_EXPORTER_OTLP_ENDPOINT=<collector-url>",
			instructions: t("settings.observability.otel.instructions"),
		},
	];

	return (
		<SettingsSection
			icon={Activity}
			title={t("settings.observability.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.observability.description")}
				</p>

				<div className="space-y-4">
					{exporters.map((exp) => {
						const Icon = exp.icon;
						return (
							<div
								key={exp.id}
								data-testid={`observability-card-${exp.id}`}
								className="rounded-lg border border-gray-700/60 p-4"
							>
								<div className="flex items-center justify-between gap-3">
									<div className="flex items-start gap-3">
										<Icon
											size={18}
											className="mt-0.5 shrink-0 text-(--accent)"
											aria-hidden="true"
										/>
										<div>
											<p className="text-sm font-medium text-gray-300">
												{exp.name}
											</p>
											<p className="text-gray-500 text-xs mt-0.5">
												{exp.description}
											</p>
										</div>
									</div>
									<Toggle
										checked={exp.enabled}
										disabled
										size="sm"
										onChange={() => {}}
										ariaLabel={exp.name}
									/>
								</div>

								{!exp.enabled && (
									<div
										data-testid={`observability-instructions-${exp.id}`}
										className="mt-3 border-t border-gray-700/60 pt-3"
									>
										<p className="text-gray-400 text-xs">{exp.instructions}</p>
										<code className="mt-2 inline-block rounded bg-gray-800 px-2 py-1 font-mono text-xs text-(--accent)">
											{exp.envVar}
										</code>
									</div>
								)}
							</div>
						);
					})}
				</div>
			</div>
		</SettingsSection>
	);
}
