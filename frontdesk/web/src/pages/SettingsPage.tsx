import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { Settings } from "../api/types";
import {
	AdminTokenResetPanel,
	AdminTokenSyncPanel,
} from "../components/AdminTokenPanels";
import { useToast } from "../context/ToastContext";
import { useMembers } from "../hooks/useMembers";

// NumberField is a labeled integer input bound to a Settings numeric key. It
// holds a local string draft so the field can be cleared and retyped without
// the value snapping to a fallback mid-edit; it only commits valid integers to
// the parent, and coerces an empty/invalid field to the minimum on blur so a
// NaN can never reach the settings PUT.
function NumberField({
	id,
	label,
	hint,
	value,
	min,
	onChange,
}: {
	id: string;
	label: string;
	hint?: string;
	value: number;
	min: number;
	onChange: (n: number) => void;
}) {
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
				value={draft}
				onChange={(e) => {
					setDraft(e.target.value);
					const n = Number.parseInt(e.target.value, 10);
					if (!Number.isNaN(n)) onChange(n);
				}}
				onBlur={() => {
					const n = Number.parseInt(draft, 10);
					const safe = Number.isNaN(n) ? min : n;
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

	const save = async (e: FormEvent) => {
		e.preventDefault();
		if (!settings) return;
		setSaveError("");
		setSaving(true);
		try {
			await api.putSettings(settings);
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
				</div>

				<label
					className="fd-row"
					style={{ marginTop: "0.5rem", cursor: "pointer" }}
				>
					<input
						type="checkbox"
						checked={settings.sticky_enabled}
						onChange={(e) => patch({ sticky_enabled: e.target.checked })}
					/>
					<span>
						<span style={{ fontWeight: 500 }}>{t("settings.sticky")}</span>
						<span
							className="fd-faint"
							style={{ display: "block", fontSize: "0.78rem" }}
						>
							{t("settings.stickyHint")}
						</span>
					</span>
				</label>

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

			<div className="ui-card ui-card-pad">
				<h2 style={{ fontSize: "1rem" }}>{t("settings.tokenSection")}</h2>
				<p
					className="fd-muted"
					style={{ fontSize: "0.85rem", marginTop: "0.4rem" }}
				>
					{t("settings.tokenSectionHint")}
				</p>
			</div>

			<AdminTokenSyncPanel members={members} onChanged={refetchMembers} />
			<AdminTokenResetPanel members={members} onChanged={refetchMembers} />
		</div>
	);
}
