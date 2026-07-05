import {
	BracketsCurlyIcon,
	BroadcastIcon,
	CheckIcon,
	CopyIcon,
	GaugeIcon,
	type Icon,
} from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { ObservabilityStatus } from "../api/types";

// Exporter is one log-export integration row: its enabled state plus the copy
// for the status badge and the (copyable) environment variable that turns it on.
interface Exporter {
	id: string;
	icon: Icon;
	enabled: boolean;
	name: string;
	description: string;
	envVar: string;
	instructions: string;
}

// CopyEnvVar renders an environment variable as a copyable mono pill, mirroring
// the main dashboard's CopyablePill. Copy feedback swaps the icon briefly; a
// clipboard failure is swallowed (the text is selectable regardless).
function CopyEnvVar({ text }: { text: string }) {
	const { t } = useTranslation();
	const [copied, setCopied] = useState(false);

	const copy = async () => {
		try {
			await navigator.clipboard.writeText(text);
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		} catch {
			// Clipboard unavailable (insecure context / denied): leave the text
			// visible so the operator can select it manually.
		}
	};

	return (
		<button
			type="button"
			className="ui-btn ui-btn-ghost fd-mono"
			onClick={copy}
			title={t("settings.observability.copy")}
			aria-label={t("settings.observability.copy")}
			style={{
				display: "inline-flex",
				alignSelf: "flex-start",
				alignItems: "center",
				justifyContent: "flex-start",
				gap: "0.4rem",
				fontSize: "0.78rem",
				padding: "0.25rem 0.55rem",
			}}
		>
			{copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
			<span>{text}</span>
		</button>
	);
}

// ObservabilityPanel is a read-only status panel for Front Desk's log-export
// integrations (JSON stdout logs, OTLP log export). Each is enabled via its own
// environment variable and resolved server-side; this panel only REFLECTS that
// state, showing an enabled/disabled badge plus copyable enable instructions
// when off. It mirrors the main dashboard's ObservabilitySettings section.
export function ObservabilityPanel() {
	const { t } = useTranslation();
	const [status, setStatus] = useState<ObservabilityStatus | null>(null);
	const [error, setError] = useState(false);

	useEffect(() => {
		api
			.getObservability()
			.then(setStatus)
			.catch(() => setError(true));
	}, []);

	const exporters: Exporter[] = [
		{
			id: "json",
			icon: BracketsCurlyIcon,
			enabled: status?.log_export_json ?? false,
			name: t("settings.observability.json.name"),
			description: t("settings.observability.json.description"),
			envVar: "LOG_FORMAT=json",
			instructions: t("settings.observability.json.instructions"),
		},
		{
			id: "otel",
			icon: BroadcastIcon,
			enabled: status?.log_export_otel ?? false,
			name: t("settings.observability.otel.name"),
			description: t("settings.observability.otel.description"),
			envVar: "OTEL_EXPORTER_OTLP_ENDPOINT=<collector-url>",
			instructions: t("settings.observability.otel.instructions"),
		},
		{
			id: "metrics",
			icon: GaugeIcon,
			enabled: status?.log_export_metrics ?? false,
			name: t("settings.observability.metrics.name"),
			description: t("settings.observability.metrics.description"),
			envVar: "FRONTDESK_METRICS_TOKEN=<token>",
			instructions: t("settings.observability.metrics.instructions"),
		},
	];

	return (
		<div className="ui-card ui-card-pad fd-stack">
			<div>
				<h2 style={{ fontSize: "1rem" }}>
					{t("settings.observability.title")}
				</h2>
				<p
					className="fd-faint"
					style={{ fontSize: "0.82rem", margin: "0.3rem 0 0" }}
				>
					{t("settings.observability.description")}
				</p>
			</div>

			{error ? (
				<div className="fd-faint" style={{ fontSize: "0.82rem" }}>
					{t("settings.observability.loadError")}
				</div>
			) : status === null ? (
				// Until the status resolves, render nothing rather than defaulting to
				// "disabled": an enabled exporter would otherwise flash a wrong badge
				// and misleading enable instructions before the response lands.
				<div className="fd-faint" style={{ fontSize: "0.82rem" }}>
					{t("common.loading")}
				</div>
			) : (
				<div className="fd-stack" style={{ gap: "1.1rem" }}>
					{exporters.map((exp) => {
						const Icon = exp.icon;
						return (
							<div
								key={exp.id}
								data-testid={`observability-card-${exp.id}`}
								className="fd-stack"
								style={{ gap: "0.4rem" }}
							>
								<div
									className="fd-row"
									style={{
										justifyContent: "space-between",
										alignItems: "flex-start",
										gap: "0.75rem",
									}}
								>
									<div
										className="fd-row"
										style={{ gap: "0.6rem", alignItems: "flex-start" }}
									>
										<Icon
											size={18}
											style={{
												marginTop: "0.1rem",
												flexShrink: 0,
												color: "var(--accent)",
											}}
											aria-hidden="true"
										/>
										<div>
											<div style={{ fontSize: "0.9rem", fontWeight: 500 }}>
												{exp.name}
											</div>
											<div className="fd-faint" style={{ fontSize: "0.78rem" }}>
												{exp.description}
											</div>
										</div>
									</div>
									<span
										data-testid={`observability-status-${exp.id}`}
										data-enabled={exp.enabled}
										className={`ui-badge ${exp.enabled ? "ui-badge-ok" : "ui-badge-danger"}`}
										style={{ flexShrink: 0 }}
									>
										{exp.enabled
											? t("settings.observability.enabled")
											: t("settings.observability.disabled")}
									</span>
								</div>

								{!exp.enabled && (
									<div
										data-testid={`observability-instructions-${exp.id}`}
										className="fd-stack"
										style={{ gap: "0.4rem", marginLeft: "1.6rem" }}
									>
										<div className="fd-faint" style={{ fontSize: "0.78rem" }}>
											{exp.instructions}
										</div>
										<CopyEnvVar text={exp.envVar} />
									</div>
								)}
							</div>
						);
					})}
				</div>
			)}
		</div>
	);
}
