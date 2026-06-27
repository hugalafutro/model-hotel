import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { AlertEventDef, AlertStatus, Settings } from "../api/types";
import { useToast } from "../context/ToastContext";

// The mask the API returns in place of a stored Apprise target. Echoing it back
// unchanged preserves the stored secret; any other value replaces it. Must match
// the server's alertMaskValue.
const MASK = "********";

// parseCsv / joinCsv convert between the stored alert_events CSV and a Set.
function parseCsv(csv: string): Set<string> {
	return new Set(
		csv
			.split(",")
			.map((s) => s.trim())
			.filter(Boolean),
	);
}

// AlertsPanel is the Settings -> Alerts control: point Front Desk at an apprise-api
// container, choose which HA events to be notified about, and send a test. It is
// self-contained (loads and saves its own copy of Settings) like AutoSyncPanel;
// on a fresh save it re-reads the freshest Settings so it never clobbers numeric
// edits made in the polling form above it. It stays quiet (renders nothing) if it
// cannot load, so the rest of the page still works.
export function AlertsPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();

	const [catalog, setCatalog] = useState<AlertEventDef[] | null>(null);
	const [loadError, setLoadError] = useState(false);
	const [enabled, setEnabled] = useState(false);
	const [url, setUrl] = useState("");
	const [target, setTarget] = useState("");
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [status, setStatus] = useState<AlertStatus | null>(null);
	const [saving, setSaving] = useState(false);
	const [testing, setTesting] = useState(false);
	const [saveError, setSaveError] = useState("");

	const applySettings = (s: Settings) => {
		setEnabled(s.alert_enabled);
		setUrl(s.alert_apprise_api_url);
		setTarget(s.alert_apprise_targets); // mask when a secret is stored
		setSelected(parseCsv(s.alert_events));
	};

	const refreshStatus = () =>
		api
			.getAlertStatus()
			.then(setStatus)
			.catch(() => {});

	// Load once on mount. Inlined (not via applySettings/refreshStatus) so the
	// effect's only dependencies are stable setters and the empty array is honest.
	useEffect(() => {
		Promise.all([api.getSettings(), api.getAlertEvents()])
			.then(([s, cat]) => {
				setEnabled(s.alert_enabled);
				setUrl(s.alert_apprise_api_url);
				setTarget(s.alert_apprise_targets);
				setSelected(parseCsv(s.alert_events));
				setCatalog(cat);
			})
			.catch(() => setLoadError(true));
		api
			.getAlertStatus()
			.then(setStatus)
			.catch(() => {});
	}, []);

	// Group the catalog by its (English) category for the picker.
	const grouped = useMemo(() => {
		const m = new Map<string, AlertEventDef[]>();
		for (const e of catalog ?? []) {
			const g = m.get(e.category) ?? [];
			g.push(e);
			m.set(e.category, g);
		}
		return [...m.entries()];
	}, [catalog]);

	if (loadError || !catalog) return null; // stay quiet; the rest of Settings works

	// persist writes the current alert fields onto the freshest Settings (so a
	// concurrent edit to the polling form is not lost) and PUTs them.
	const persist = async () => {
		const fresh = await api.getSettings();
		const next: Settings = {
			...fresh,
			alert_enabled: enabled,
			alert_apprise_api_url: url.trim(),
			alert_apprise_targets: target, // MASK preserves, new replaces, "" clears
			alert_events: [...selected].join(","),
		};
		await api.putSettings(next);
		applySettings(await api.getSettings());
	};

	const save = async () => {
		setSaveError("");
		setSaving(true);
		try {
			await persist();
			await refreshStatus();
			toast(t("settings.alerts.saved"), "success");
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

	// sendTest persists first so the test reflects the on-screen config, then asks
	// the server to deliver a test notification to the configured target(s).
	const sendTest = async () => {
		setSaveError("");
		setTesting(true);
		try {
			await persist();
			await api.testAlert();
			await refreshStatus();
			toast(t("settings.alerts.testSent"), "success");
		} catch (err) {
			toast(t("settings.alerts.testFailed"), "error");
			if (err instanceof ApiError && err.message) setSaveError(err.message);
		} finally {
			setTesting(false);
		}
	};

	const toggleEvent = (type: string, on: boolean) =>
		setSelected((prev) => {
			const next = new Set(prev);
			if (on) next.add(type);
			else next.delete(type);
			return next;
		});

	const busy = saving || testing;

	return (
		<div className="ui-card ui-card-pad fd-stack">
			<div className="fd-row" style={{ justifyContent: "space-between" }}>
				<h2 style={{ fontSize: "1rem" }}>{t("settings.alerts.title")}</h2>
				<StatusPill status={status} t={t} />
			</div>
			<p
				className="fd-faint"
				style={{ fontSize: "0.82rem", margin: "0.3rem 0 0.6rem" }}
			>
				{t("settings.alerts.hint")}
			</p>

			<label className="fd-row" style={{ cursor: "pointer" }}>
				<input
					type="checkbox"
					data-testid="alert-enabled"
					checked={enabled}
					disabled={busy}
					onChange={(e) => setEnabled(e.target.checked)}
				/>
				<span style={{ fontWeight: 500 }}>
					{t("settings.alerts.enableLabel")}
				</span>
			</label>

			<div className="ui-field">
				<label className="ui-label" htmlFor="alert-url">
					{t("settings.alerts.apiUrlLabel")}
				</label>
				<input
					id="alert-url"
					className="ui-input"
					type="url"
					placeholder="http://apprise:8000"
					value={url}
					disabled={busy}
					onChange={(e) => setUrl(e.target.value)}
				/>
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					{t("settings.alerts.apiUrlHint")}
				</div>
			</div>

			<div className="ui-field">
				<label className="ui-label" htmlFor="alert-target">
					{t("settings.alerts.targetLabel")}
				</label>
				<input
					id="alert-target"
					className="ui-input"
					type="password"
					autoComplete="off"
					placeholder="tgram://token/chat_id"
					value={target}
					disabled={busy}
					onChange={(e) => setTarget(e.target.value)}
				/>
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					{target === MASK
						? t("settings.alerts.targetStoredNote")
						: t("settings.alerts.targetHint")}
				</div>
			</div>

			<fieldset
				style={{ border: "none", padding: 0, margin: 0 }}
				disabled={busy}
			>
				<legend className="ui-label">{t("settings.alerts.eventsLabel")}</legend>
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", margin: "0 0 0.5rem" }}
				>
					{t("settings.alerts.eventsHint")}
				</div>
				{grouped.map(([category, defs]) => (
					<div key={category} style={{ marginBottom: "0.6rem" }}>
						<div style={{ fontWeight: 500, fontSize: "0.85rem" }}>
							{category}
						</div>
						{defs.map((d) => (
							<label
								key={d.type}
								className="fd-row"
								style={{ cursor: "pointer", marginTop: "0.2rem" }}
							>
								<input
									type="checkbox"
									data-testid={`alert-event-${d.type}`}
									checked={selected.has(d.type)}
									onChange={(e) => toggleEvent(d.type, e.target.checked)}
								/>
								<span style={{ fontSize: "0.85rem" }}>{d.type}</span>
							</label>
						))}
					</div>
				))}
			</fieldset>

			{saveError && (
				<div className="fd-error-text" role="alert">
					{saveError}
				</div>
			)}

			<div className="fd-row" style={{ gap: "0.6rem" }}>
				<button
					type="button"
					className="ui-btn ui-btn-primary"
					disabled={busy}
					onClick={save}
				>
					{saving ? t("common.saving") : t("settings.alerts.saveBtn")}
				</button>
				<button
					type="button"
					className="ui-btn"
					data-testid="alert-test"
					disabled={busy}
					onClick={sendTest}
				>
					{testing
						? t("settings.alerts.testing")
						: t("settings.alerts.testBtn")}
				</button>
			</div>
		</div>
	);
}

// StatusPill renders the apprise-api reachability as a coloured badge.
function StatusPill({
	status,
	t,
}: {
	status: AlertStatus | null;
	t: (k: string) => string;
}) {
	if (!status?.configured) {
		return (
			<span className="ui-badge ui-badge-info" data-testid="alert-status">
				{t("settings.alerts.statusNotConfigured")}
			</span>
		);
	}
	if (!status.reachable) {
		return (
			<span className="ui-badge ui-badge-danger" data-testid="alert-status">
				{t("settings.alerts.statusUnreachable")}
			</span>
		);
	}
	if (!status.healthy) {
		return (
			<span className="ui-badge ui-badge-warn" data-testid="alert-status">
				{t("settings.alerts.statusUnhealthy")}
			</span>
		);
	}
	return (
		<span className="ui-badge ui-badge-ok" data-testid="alert-status">
			{t("settings.alerts.statusOk")}
		</span>
	);
}
