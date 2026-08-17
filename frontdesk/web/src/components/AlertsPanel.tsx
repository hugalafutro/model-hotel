import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { AlertEventDef, AlertStatus, Settings } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ntfyAppriseURL } from "../utils/ntfy";
import { AlertsWizard } from "./alerts/AlertsWizard";
import { APPRISE_SERVICES_URL, ntfyServerOf } from "./alerts/composers";
import { DestinationList } from "./alerts/DestinationList";
import { eventLabel, SEVERITY_COLOR } from "./alerts/events";

// The failure codes /api/alert/test returns with a 502 that map to an
// actionable sentence; anything else falls back to the generic error.
const REASON_CODES = new Set([
	"not_configured",
	"invalid_url",
	"unreachable",
	"unhealthy",
	"apprise_reject",
	"deliver_failed",
	"undecryptable",
]);

// parseCsv turns the stored alert_events CSV into a membership Set.
function parseCsv(csv: string): Set<string> {
	return new Set(
		csv
			.split(",")
			.map((s) => s.trim())
			.filter(Boolean),
	);
}

interface LoadedTargets {
	targets: string[];
	/** Translation key of the read failure, or "" when the read succeeded. */
	error: string;
}

// fetchTargets reads the plaintext destination list. It resolves either way so
// it can sit in the same Promise.all as the other reads: an unreadable stored
// value (master key rotated) is a message beside an empty list, not a card that
// refuses to render.
async function fetchTargets(): Promise<LoadedTargets> {
	try {
		const { targets } = await api.getAlertTargets();
		return { targets, error: "" };
	} catch (err) {
		return {
			targets: [],
			error:
				err instanceof ApiError && err.code === "undecryptable"
					? "settings.alerts.destinationsError"
					: "errors.generic",
		};
	}
}

// AlertsPanel is the Settings -> Alerts control: point Front Desk at an apprise-api
// container, choose which HA events to be notified about, and send a test. It is
// self-contained (loads and saves its own copy of Settings); on every save it
// re-reads the freshest Settings before writing so it never
// clobbers edits made in the polling form above it (and that form does the same).
// It stays quiet (renders nothing) if it cannot load, so the rest of the page
// still works.
//
// Destinations are shown in clear, both as the readable list and in the manual
// field: the page is admin-only and anyone who can read it can already rewrite
// the targets, while a masked list makes two phones indistinguishable.
export function AlertsPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();

	const [catalog, setCatalog] = useState<AlertEventDef[] | null>(null);
	const [loadError, setLoadError] = useState(false);
	const [enabled, setEnabled] = useState(false);
	const [url, setUrl] = useState("");
	const [targets, setTargets] = useState<string[]>([]);
	const [targetsError, setTargetsError] = useState("");
	const [target, setTarget] = useState("");
	const [appended, setAppended] = useState(false);
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [status, setStatus] = useState<AlertStatus | null>(null);
	const [saving, setSaving] = useState(false);
	const [testing, setTesting] = useState(false);
	const [saveError, setSaveError] = useState("");
	// Which entry point opened the wizard, or null while it is closed. Step 1
	// walks the whole setup; step 2 goes straight to adding a destination to an
	// apprise address that is already stored.
	const [wizardAt, setWizardAt] = useState<1 | 2 | null>(null);

	// applyLoaded pushes a freshly read settings row and destination list into the
	// form. The manual field is derived from the plaintext list, so it shows
	// exactly what is stored and a save round-trips it unchanged.
	const applyLoaded = (s: Settings, loaded: LoadedTargets) => {
		setEnabled(s.alert_enabled);
		setUrl(s.alert_apprise_api_url);
		setSelected(parseCsv(s.alert_events));
		setTargets(loaded.targets);
		setTargetsError(loaded.error);
		setTarget(loaded.targets.join("; "));
		setAppended(false);
	};

	const refreshStatus = () =>
		api
			.getAlertStatus()
			.then(setStatus)
			.catch(() => {});

	// Load once on mount. Inlined (not via applyLoaded/refreshStatus) so the
	// effect's only dependencies are stable setters and the empty array is honest.
	useEffect(() => {
		Promise.all([api.getSettings(), api.getAlertEvents(), fetchTargets()])
			.then(([s, cat, loaded]) => {
				setEnabled(s.alert_enabled);
				setUrl(s.alert_apprise_api_url);
				setSelected(parseCsv(s.alert_events));
				setCatalog(cat);
				setTargets(loaded.targets);
				setTargetsError(loaded.error);
				setTarget(loaded.targets.join("; "));
			})
			.catch(() => setLoadError(true));
		api
			.getAlertStatus()
			.then(setStatus)
			.catch(() => {});
	}, []);

	// A validation error (400) carries a safe, user-facing message and a 502 from
	// the test endpoint carries a machine-readable reason code; anything else
	// (network, other 5xx, auth) is shown as a generic string so internals do not
	// leak.
	const describeError = (err: unknown) => {
		if (!(err instanceof ApiError)) return t("errors.generic");
		if (err.status === 400) return err.message;
		if (err.status === 502 && err.code && REASON_CODES.has(err.code)) {
			return t(`settings.alerts.reason.${err.code}`);
		}
		return t("errors.generic");
	};

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

	// persist PUTs only the alert fields; the server merges them onto the stored
	// row, so this never disturbs the polling form's settings (and vice versa).
	// `overrides` lets a row action (e.g. removing one destination) write a
	// different target list than the manual field currently holds.
	const persist = async (overrides?: Partial<Settings>) => {
		const body: Partial<Settings> = {
			alert_enabled: enabled,
			alert_apprise_api_url: url.trim(),
			alert_apprise_targets: target.trim(),
			alert_events: [...selected].join(","),
			...overrides,
		};
		// The manual field is derived from the destination read, so when that read
		// failed and nothing was typed into it, it holds nothing and writing it
		// would clear the stored destinations. The key is left out of the PUT
		// instead, and the server's partial merge keeps the stored ciphertext.
		// Anything actually typed is the operator rewriting an unreadable list
		// (the master-key-rotated recovery path) and is written, as is a list a
		// caller passes itself (a row removal).
		if (
			targetsError !== "" &&
			target.trim() === "" &&
			overrides?.alert_apprise_targets === undefined
		) {
			delete body.alert_apprise_targets;
		}
		await api.putSettings(body);
		const [s, loaded] = await Promise.all([api.getSettings(), fetchTargets()]);
		applyLoaded(s, loaded);
	};

	// reload re-reads what the wizard wrote. The card holds its own copy of the
	// settings row, so after a write it did not make itself it has to fetch what
	// actually landed rather than mirror what the wizard said it sent.
	const reload = async () => {
		setSaveError("");
		try {
			const [s, loaded] = await Promise.all([
				api.getSettings(),
				fetchTargets(),
			]);
			applyLoaded(s, loaded);
			await refreshStatus();
			toast(t("settings.alerts.saved"), "success");
		} catch {
			setSaveError(t("errors.generic"));
		}
	};

	const save = async () => {
		setSaveError("");
		setSaving(true);
		try {
			await persist();
			await refreshStatus();
			toast(t("settings.alerts.saved"), "success");
		} catch (err) {
			setSaveError(describeError(err));
		} finally {
			setSaving(false);
		}
	};

	// sendTest persists first so the test reflects the on-screen config, then asks
	// the server to deliver a test notification to the configured target(s). A
	// failure shows a generic toast (the reachability pill carries the reason); the
	// raw transport/5xx error is never surfaced.
	const sendTest = async () => {
		setSaveError("");
		setTesting(true);
		try {
			await persist();
			await api.testAlert();
			toast(t("settings.alerts.testSent"), "success");
		} catch (err) {
			setSaveError(describeError(err));
			toast(t("settings.alerts.testFailed"), "error");
		} finally {
			await refreshStatus();
			setTesting(false);
		}
	};

	// testDestination delivers to one saved destination only, so a fleet with
	// several phones can tell which one is broken. It tests what is stored, so
	// nothing on screen is persisted first; the probe result is refreshed after,
	// exactly as a full Send test does.
	const testDestination = async (dest: string) => {
		setSaveError("");
		setTesting(true);
		try {
			await api.testAlert({ targets: [dest] });
			toast(t("settings.alerts.testSent"), "success");
		} catch (err) {
			setSaveError(describeError(err));
			toast(t("settings.alerts.testFailed"), "error");
		} finally {
			await refreshStatus();
			setTesting(false);
		}
	};

	// removeDestination persists the list without that one URL. The remaining
	// on-screen edits ride along, exactly as they do for Save.
	const removeDestination = async (dest: string) => {
		setSaveError("");
		setSaving(true);
		try {
			await persist({
				alert_apprise_targets: targets.filter((x) => x !== dest).join("; "),
			});
			await refreshStatus();
			toast(t("settings.alerts.saved"), "success");
		} catch (err) {
			setSaveError(describeError(err));
		} finally {
			setSaving(false);
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

	// The manual field holds an unsaved edit, so the rows below no longer describe
	// what is stored: testing or removing one would act on the stored list while
	// the operator is looking at a different one. Both row actions wait for a Save.
	const targetsDirty = target.trim() !== targets.join("; ");

	// Why the wizard cannot be opened right now. The write itself is safe either
	// way (the wizard re-reads the stored destinations immediately before it, so
	// it cannot drop one), but the run leading up to it would be misleading: an
	// unreadable list means step 5 cannot show what is already configured, and a
	// pending manual edit means it would show the list from before that edit. In
	// both cases the destinations have to be sorted out on the card first, which
	// is what the message points at.
	const wizardBlocked = targetsError
		? t(targetsError)
		: targetsDirty
			? t("settings.alerts.destinationsDirty")
			: undefined;

	return (
		<div className="ui-card ui-card-pad fd-stack">
			<div className="fd-row" style={{ justifyContent: "space-between" }}>
				<h2 className="fd-card-title">{t("settings.alerts.title")}</h2>
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
					checked={enabled}
					disabled={busy}
					onChange={(e) => setEnabled(e.target.checked)}
				/>
				<span style={{ fontWeight: 500 }}>
					{t("settings.alerts.enableLabel")}
				</span>
			</label>

			{/* A stored list that cannot be read (a rotated master key) is worth
			    saying whether or not alerts are switched on: it is the one
			    condition on this card the toggle does nothing about, and it also
			    greys out the guided button below. */}
			{targetsError && (
				<div
					className="fd-error-text"
					data-testid="alert-destinations-error"
					style={{ marginBottom: "0.4rem" }}
				>
					{t(targetsError)}
				</div>
			)}

			{/* Like the SSO card, the configuration only unrolls once alerts are on;
			    switched off, the card is just its toggle plus Save. */}
			{enabled && (
				<>
					<div className="ui-field">
						<span className="ui-label">
							{t("settings.alerts.destinationsTitle")}
						</span>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", margin: "0.1rem 0 0.4rem" }}
						>
							{t("settings.alerts.destinationsNote")}
						</div>
						<DestinationList
							targets={targets}
							onRemove={removeDestination}
							onTest={testDestination}
							busy={busy}
							disabledReason={
								targetsDirty
									? t("settings.alerts.destinationsDirty")
									: undefined
							}
						/>
					</div>

					{/* Everything the wizard writes for you, kept reachable for the
					    operator who would rather type the Apprise URL themselves. */}
					<details data-testid="alert-manual">
						<summary style={{ cursor: "pointer", fontWeight: 500 }}>
							{t("settings.alerts.manualTitle")}
						</summary>
						<div className="fd-stack" style={{ marginTop: "0.6rem" }}>
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
									className="ui-input fd-mono"
									type="text"
									autoComplete="off"
									spellCheck={false}
									placeholder="tgram://token/chat_id"
									value={target}
									disabled={busy}
									onChange={(e) => {
										setTarget(e.target.value);
										setAppended(false);
									}}
								/>
								<div
									className="fd-faint"
									style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
								>
									{t("settings.alerts.targetHint")}{" "}
									<a
										className="fd-link"
										href={APPRISE_SERVICES_URL}
										target="_blank"
										rel="noreferrer"
									>
										{t("settings.alerts.browseServices")}
										<ArrowSquareOutIcon
											size={12}
											style={{ marginLeft: 3, verticalAlign: "-1px" }}
										/>
									</a>
								</div>
							</div>

							<NtfyHelper
								disabled={busy}
								initialServer={ntfyServerOf(targets)}
								onUse={(apprise) => {
									setTarget((prev) =>
										prev.trim() ? `${prev.trim()}; ${apprise}` : apprise,
									);
									setAppended(true);
								}}
							/>
							{appended && (
								<div
									className="fd-faint"
									data-testid="alert-ntfy-appended"
									style={{ fontSize: "0.78rem" }}
								>
									{t("settings.alerts.ntfyAppended")}
								</div>
							)}

							<fieldset
								style={{ border: "none", padding: 0, margin: 0 }}
								disabled={busy}
							>
								<legend className="ui-label">
									{t("settings.alerts.eventsLabel")}
								</legend>
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
										{defs.map((d) => {
											const label = eventLabel(t, d.type);
											return (
												<label
													key={d.type}
													className="fd-row"
													style={{ cursor: "pointer", marginTop: "0.2rem" }}
												>
													<input
														type="checkbox"
														aria-label={label}
														checked={selected.has(d.type)}
														onChange={(e) =>
															toggleEvent(d.type, e.target.checked)
														}
													/>
													<span
														aria-hidden="true"
														style={{
															display: "inline-block",
															width: "0.5rem",
															height: "0.5rem",
															borderRadius: "50%",
															background:
																SEVERITY_COLOR[d.severity] ??
																"var(--text-faint)",
														}}
													/>
													<span style={{ fontSize: "0.85rem" }}>{label}</span>
												</label>
											);
										})}
									</div>
								))}
							</fieldset>
						</div>
					</details>
				</>
			)}

			{saveError && (
				<div className="fd-error-text" role="alert">
					{saveError}
				</div>
			)}

			<div className="fd-row" style={{ gap: "0.6rem", flexWrap: "wrap" }}>
				{/* Exactly one guided entry point, chosen by whether anything is
				    stored, and offered once the toggle is on like the rest of the
				    configuration: a switched-off card is its toggle plus Save, so
				    the way in is the same on every visit (switch on, then set up).
				    With no destination the card offers the full run from step 1;
				    once one exists the only thing left to do is append another,
				    which starts at step 2 and skips the Apprise step. */}
				{enabled &&
					(targets.length === 0 ? (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="alert-wizard-open"
							title={wizardBlocked}
							disabled={busy || wizardBlocked !== undefined}
							onClick={() => setWizardAt(1)}
						>
							{t("settings.alerts.wizard.open")}
						</button>
					) : (
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							data-testid="alert-wizard-add"
							title={wizardBlocked}
							disabled={busy || wizardBlocked !== undefined}
							onClick={() => setWizardAt(2)}
						>
							{t("settings.alerts.wizard.addDestination")}
						</button>
					))}
				<button type="button" className="ui-btn" disabled={busy} onClick={save}>
					{saving ? t("common.saving") : t("settings.alerts.saveBtn")}
				</button>
				{enabled && (
					<button
						type="button"
						className="ui-btn"
						disabled={busy}
						onClick={sendTest}
					>
						{testing
							? t("settings.alerts.testing")
							: t("settings.alerts.testBtn")}
					</button>
				)}
			</div>

			{wizardAt !== null && (
				<AlertsWizard
					initialApiUrl={url}
					savedTargets={targets}
					savedEvents={[...selected].join(",")}
					catalog={catalog}
					startAt={wizardAt}
					// Cancel: the wizard wrote nothing, so there is nothing to reload.
					onClose={() => setWizardAt(null)}
					onFinished={() => {
						setWizardAt(null);
						reload();
					}}
				/>
			)}
		</div>
	);
}

// NtfyHelper is the phone-push convenience block (Bellhop plan section 4.3):
// it pre-formats the Apprise URL for an ntfy topic so pointing fleet alerts at
// a phone is a copy-free two-field job. Self-hosted ntfy and hosted ntfy both
// work; the operator says which server, and the composed URL is appended to the
// target field and goes through the ordinary save flow.
function NtfyHelper({
	disabled,
	initialServer,
	onUse,
}: {
	disabled: boolean;
	// The ntfy server already in use, so a second phone joins it without the
	// operator retyping the URL. Empty when no ntfy target is stored yet.
	initialServer: string;
	onUse: (appriseURL: string) => void;
}) {
	const { t } = useTranslation();
	const [server, setServer] = useState(initialServer);
	const [topic, setTopic] = useState("");
	const composed = ntfyAppriseURL(server, topic);

	return (
		<div className="ui-field">
			<span className="ui-label">{t("settings.alerts.ntfyTitle")}</span>
			<div
				className="fd-faint"
				style={{ fontSize: "0.78rem", margin: "0.1rem 0 0.4rem" }}
			>
				{t("settings.alerts.ntfyHint")}
			</div>
			<div className="fd-row" style={{ flexWrap: "wrap", gap: "0.6rem" }}>
				<input
					className="ui-input"
					type="url"
					aria-label={t("settings.alerts.ntfyServerLabel")}
					placeholder={t("settings.alerts.ntfyServerPlaceholder")}
					value={server}
					disabled={disabled}
					onChange={(e) => setServer(e.target.value)}
					style={{ flex: "1 1 180px" }}
				/>
				<input
					className="ui-input"
					type="text"
					aria-label={t("settings.alerts.ntfyTopicLabel")}
					placeholder={t("settings.alerts.ntfyTopicPlaceholder")}
					value={topic}
					disabled={disabled}
					onChange={(e) => setTopic(e.target.value)}
					style={{ flex: "1 1 180px" }}
				/>
				<button
					type="button"
					className="ui-btn"
					disabled={disabled || !composed}
					onClick={() => onUse(composed)}
				>
					{t("settings.alerts.ntfyUse")}
				</button>
			</div>
			{composed && (
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					<code>{composed}</code>
				</div>
			)}
		</div>
	);
}

// StatusPill renders the apprise-api reachability as a coloured badge, with the
// probe detail (e.g. "unreachable", "apprise-api returned status 417") shown as a
// tooltip and inline note so the operator gets a reason, not just a colour. With
// nothing configured yet it says where to start instead of only what is missing.
function StatusPill({
	status,
	t,
}: {
	status: AlertStatus | null;
	t: (k: string) => string;
}) {
	if (!status?.configured) {
		return (
			<span
				className="fd-row"
				style={{ gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}
			>
				<span className="ui-badge ui-badge-info">
					{t("settings.alerts.statusNotConfigured")}
				</span>
				<span
					className="fd-faint"
					data-testid="alert-status-hint"
					style={{ fontSize: "0.72rem" }}
				>
					{t("settings.alerts.statusNotConfiguredHint")}
				</span>
			</span>
		);
	}
	const [variant, label] = !status.reachable
		? (["ui-badge-danger", t("settings.alerts.statusUnreachable")] as const)
		: !status.healthy
			? (["ui-badge-warn", t("settings.alerts.statusUnhealthy")] as const)
			: (["ui-badge-ok", t("settings.alerts.statusOk")] as const);
	// The reason code is the translated, actionable half of the probe result; the
	// detail is raw server text (English, sometimes an HTTP status). The note
	// therefore prefers the reason and keeps the detail as the tooltip, where an
	// operator who wants the literal answer can still find it.
	const note =
		status.reason && REASON_CODES.has(status.reason)
			? t(`settings.alerts.reason.${status.reason}`)
			: status.detail;
	const showNote = note && (!status.reachable || !status.healthy);
	return (
		<span
			className="fd-row"
			style={{ gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}
		>
			<span className={`ui-badge ${variant}`} title={status.detail}>
				{label}
			</span>
			{showNote && (
				<span
					className="fd-faint"
					data-testid="alert-status-note"
					style={{ fontSize: "0.72rem" }}
				>
					{note}
				</span>
			)}
		</span>
	);
}
