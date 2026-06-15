import { useTranslation } from "react-i18next";
import { Activity, Braces, Gauge, type LucideIcon } from "@/lib/icons";
import { CopyablePill } from "../../components/CopyablePill";
import { SettingsSection } from "../../components/SettingsSection";
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
	/** Environment variable shown (copyable) in the enable instructions when off. */
	envVar: string;
	instructions: string;
}

/**
 * ObservabilitySettings is a read-only status panel for the three log-export
 * integrations (JSON stdout, Prometheus /metrics, OTLP logs). Each is enabled
 * via its own environment variable and resolved server-side; this section only
 * REFLECTS that state — a green/red status badge (not a toggle, since nothing
 * here is changeable at runtime) plus copyable enable instructions when off.
 * No resettable settings, so no reset affordance (mirrors PasskeySettings).
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

				<div className="space-y-6">
					{exporters.map((exp) => {
						const Icon = exp.icon;
						return (
							<div
								key={exp.id}
								data-testid={`observability-card-${exp.id}`}
								className="space-y-2"
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
									<span
										data-testid={`observability-status-${exp.id}`}
										data-enabled={exp.enabled}
										className={`ui-badge ${exp.enabled ? "ui-badge-success" : "ui-badge-error"} shrink-0 inline-flex items-center px-2 py-0.5 text-xs font-medium`}
									>
										{exp.enabled
											? t("settings.common.enabled")
											: t("settings.common.disabled")}
									</span>
								</div>

								{!exp.enabled && (
									<div
										data-testid={`observability-instructions-${exp.id}`}
										className="ml-7 space-y-1.5"
									>
										<p className="text-gray-400 text-xs">{exp.instructions}</p>
										<CopyablePill
											text={exp.envVar}
											textClassName="font-mono text-xs text-(--accent)"
										/>
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
