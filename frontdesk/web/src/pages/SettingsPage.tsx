import { type SyntheticEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { Settings } from "../api/types";
import { AlertsPanel } from "../components/AlertsPanel";
import { FleetSyncWizard } from "../components/FleetSyncWizard";
import { ObservabilityPanel } from "../components/ObservabilityPanel";
import { OidcPanel } from "../components/OidcPanel";
import { SecurityPanels } from "../components/SecurityPanels";
import { useToast } from "../context/ToastContext";
import { useMembers } from "../hooks/useMembers";

// NumberField is a labeled integer input bound to a Settings numeric key. It
// holds a local string draft so the field can be cleared and retyped without
// the value snapping to a fallback mid-edit; it only commits valid integers to
// the parent, and coerces an empty, invalid, or below-minimum field to the
// minimum on blur so an out-of-range value can never reach the settings PUT.
function NumberField({
	id,
	label,
	hint,
	value,
	min,
	max,
	onChange,
}: {
	id: string;
	label: string;
	hint?: string;
	value: number;
	min: number;
	max?: number;
	onChange: (n: number) => void;
}) {
	const clamp = (n: number) => {
		const lo = n < min ? min : n;
		return max !== undefined && lo > max ? max : lo;
	};
	const [draft, setDraft] = useState(String(value));
	// Re-sync the draft when the committed value changes from outside this field
	// (e.g. a reset), using the render-time adjustment pattern rather than an
	// effect so there's no extra render pass.
	const [lastValue, setLastValue] = useState(value);
	if (value !== lastValue) {
		setLastValue(value);
		setDraft(String(value));
	}

	return (
		<div className="ui-field">
			<label className="ui-label" htmlFor={id}>
				{label}
			</label>
			<input
				id={id}
				className="ui-input"
				type="number"
				min={min}
				max={max}
				value={draft}
				onChange={(e) => {
					setDraft(e.target.value);
					const n = Number.parseInt(e.target.value, 10);
					if (!Number.isNaN(n)) onChange(clamp(n));
				}}
				onBlur={() => {
					const n = Number.parseInt(draft, 10);
					const safe = Number.isNaN(n) ? min : clamp(n);
					setDraft(String(safe));
					onChange(safe);
				}}
			/>
			{hint && (
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					{hint}
				</div>
			)}
		</div>
	);
}

export function SettingsPage() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { members, refetch: refetchMembers } = useMembers();
	const [settings, setSettings] = useState<Settings | null>(null);
	const [error, setError] = useState(false);
	const [saving, setSaving] = useState(false);
	const [saveError, setSaveError] = useState("");

	useEffect(() => {
		api
			.getSettings()
			.then(setSettings)
			.catch(() => setError(true));
	}, []);

	const patch = (p: Partial<Settings>) =>
		setSettings((s) => (s ? { ...s, ...p } : s));

	const save = async (e: SyntheticEvent) => {
		e.preventDefault();
		if (!settings) return;
		setSaveError("");
		setSaving(true);
		try {
			// PUT only the polling fields this form owns; the server merges them onto
			// the stored row, so alert settings edited in the Alerts panel are never
			// reverted by saving here (and vice versa).
			await api.putSettings({
				health_poll_secs: settings.health_poll_secs,
				traefik_poll_secs: settings.traefik_poll_secs,
				traefik_stale_secs: settings.traefik_stale_secs,
				event_retention_days: settings.event_retention_days,
				retry_attempts: settings.retry_attempts,
				health_fail_threshold: settings.health_fail_threshold,
				session_idle_timeout_minutes: settings.session_idle_timeout_minutes,
			});
			toast(t("settings.saved"), "success");
		} catch (err) {
			setSaveError(
				err instanceof ApiError && err.status === 400
					? err.message
					: t("errors.generic"),
			);
		} finally {
			setSaving(false);
		}
	};

	if (error)
		return <div className="fd-empty fd-error-text">{t("errors.network")}</div>;
	if (!settings) return <div className="fd-empty">{t("common.loading")}</div>;

	return (
		<div className="fd-stack">
			<h1 className="fd-page-title">{t("settings.title")}</h1>

			<form className="ui-card ui-card-pad" onSubmit={save}>
				<h2 style={{ fontSize: "1rem" }}>{t("settings.pollSection")}</h2>
				<p
					className="fd-faint"
					style={{ fontSize: "0.82rem", margin: "0.3rem 0 1rem" }}
				>
					{t("settings.pollSectionHint")}
				</p>

				<div
					className="fd-row"
					style={{ alignItems: "flex-start", flexWrap: "wrap", gap: "0.8rem" }}
				>
					<div style={{ flex: "1 1 200px" }}>
						<NumberField
							id="health-poll"
							label={t("settings.healthPoll")}
							value={settings.health_poll_secs}
							min={1}
							onChange={(n) => patch({ health_poll_secs: n })}
						/>
					</div>
					<div style={{ flex: "1 1 200px" }}>
						<NumberField
							id="traefik-poll"
							label={t("settings.traefikPoll")}
							value={settings.traefik_poll_secs}
							min={1}
							onChange={(n) => patch({ traefik_poll_secs: n })}
						/>
					</div>
				</div>

				<NumberField
					id="traefik-stale"
					label={t("settings.traefikStale")}
					hint={t("settings.traefikStaleHint")}
					value={settings.traefik_stale_secs}
					min={1}
					onChange={(n) => patch({ traefik_stale_secs: n })}
				/>

				<div
					className="fd-row"
					style={{ alignItems: "flex-start", flexWrap: "wrap", gap: "0.8rem" }}
				>
					<div style={{ flex: "1 1 200px" }}>
						<NumberField
							id="retention"
							label={t("settings.retention")}
							value={settings.event_retention_days}
							min={1}
							onChange={(n) => patch({ event_retention_days: n })}
						/>
					</div>
					<div style={{ flex: "1 1 200px" }}>
						<NumberField
							id="retry"
							label={t("settings.retryAttempts")}
							hint={t("settings.retryHint")}
							value={settings.retry_attempts}
							min={0}
							onChange={(n) => patch({ retry_attempts: n })}
						/>
					</div>
					<div style={{ flex: "1 1 200px" }}>
						<NumberField
							id="health-fail-threshold"
							label={t("settings.healthFailThreshold")}
							hint={t("settings.healthFailThresholdHint")}
							value={settings.health_fail_threshold}
							min={1}
							onChange={(n) => patch({ health_fail_threshold: n })}
						/>
					</div>
				</div>

				<NumberField
					id="session-idle-timeout"
					label={t("settings.sessionTimeout")}
					hint={t("settings.sessionTimeoutHint")}
					value={settings.session_idle_timeout_minutes}
					min={0}
					max={240}
					onChange={(n) => patch({ session_idle_timeout_minutes: n })}
				/>

				{saveError && (
					<div
						className="fd-error-text"
						role="alert"
						style={{ marginTop: "0.8rem" }}
					>
						{saveError}
					</div>
				)}
				<div style={{ marginTop: "1rem" }}>
					<button
						type="submit"
						className="ui-btn ui-btn-primary"
						disabled={saving}
					>
						{saving ? t("common.saving") : t("common.save")}
					</button>
				</div>
			</form>

			<FleetSyncWizard members={members} onChanged={refetchMembers} />

			<AlertsPanel />

			<SecurityPanels />

			<OidcPanel />

			<ObservabilityPanel />
		</div>
	);
}
