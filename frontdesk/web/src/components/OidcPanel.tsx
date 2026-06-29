import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { OidcStatus, Settings } from "../api/types";
import { useToast } from "../context/ToastContext";

// The mask the API returns in place of a stored OIDC client secret. Echoing it
// back unchanged preserves the stored secret; any other value replaces it; ""
// clears it. Must match the server's alertMaskValue.
const MASK = "********";

// OidcPanel is the Settings -> SSO control: point Front Desk at an external
// OpenID Connect provider so admins get a fourth login path (alongside the
// FRONTDESK_TOKEN, TOTP, and passkeys). It is self-contained (loads and saves
// its own copy of Settings) like AlertsPanel, re-reading the freshest Settings
// before each write so it never clobbers edits made in the panels above it. SSO
// is additive and never replaces local login, so a misconfigured or unreachable
// IdP cannot lock the operator out. GitHub login is main-dashboard-only by
// design and intentionally absent here.
export function OidcPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();

	const [loadError, setLoadError] = useState(false);
	const [loaded, setLoaded] = useState(false);
	const [enabled, setEnabled] = useState(false);
	const [issuer, setIssuer] = useState("");
	const [clientId, setClientId] = useState("");
	const [secret, setSecret] = useState("");
	const [baseUrl, setBaseUrl] = useState("");
	const [emails, setEmails] = useState("");
	const [status, setStatus] = useState<OidcStatus | null>(null);
	const [saving, setSaving] = useState(false);
	const [saveError, setSaveError] = useState("");

	const applySettings = (s: Settings) => {
		setEnabled(s.oidc_enabled);
		setIssuer(s.oidc_issuer_url);
		setClientId(s.oidc_client_id);
		setSecret(s.oidc_client_secret); // mask when a secret is stored
		setBaseUrl(s.oidc_public_base_url);
		setEmails(s.oidc_allowed_emails);
	};

	// Load once on mount. Inlined (not via applySettings) so the effect's only
	// dependencies are the stable setters and the empty array is honest.
	useEffect(() => {
		api
			.getSettings()
			.then((s) => {
				setEnabled(s.oidc_enabled);
				setIssuer(s.oidc_issuer_url);
				setClientId(s.oidc_client_id);
				setSecret(s.oidc_client_secret);
				setBaseUrl(s.oidc_public_base_url);
				setEmails(s.oidc_allowed_emails);
				setLoaded(true);
			})
			.catch(() => setLoadError(true));
		api
			.oidcStatus()
			.then(setStatus)
			.catch(() => {});
	}, []);

	// Only a validation error (400) carries a safe, user-facing message; anything
	// else (network, 5xx, auth) is shown generically so internals do not leak.
	const describeError = (err: unknown) =>
		err instanceof ApiError && err.status === 400
			? err.message
			: t("errors.generic");

	if (loadError || !loaded) return null; // stay quiet; the rest of Settings works

	// The redirect URI the operator registers with their IdP, derived from the
	// public base URL (trailing slashes trimmed).
	const redirectUri = baseUrl.trim()
		? `${baseUrl.trim().replace(/\/+$/, "")}/api/auth/oidc/callback`
		: "";

	const save = async () => {
		setSaveError("");
		setSaving(true);
		try {
			// PUT only the OIDC fields; the server merges them onto the stored row,
			// so the polling/alert panels are never disturbed (and vice versa). MASK
			// preserves the stored secret; any other value replaces it; "" clears it.
			await api.putSettings({
				oidc_enabled: enabled,
				oidc_issuer_url: issuer.trim(),
				oidc_client_id: clientId.trim(),
				oidc_client_secret: secret === MASK ? secret : secret.trim(),
				oidc_public_base_url: baseUrl.trim(),
				oidc_allowed_emails: emails.trim(),
			});
			const fresh = await api.getSettings();
			applySettings(fresh);
			setStatus(await api.oidcStatus().catch(() => status));
			toast(t("settings.oidc.saved"), "success");
		} catch (err) {
			setSaveError(describeError(err));
		} finally {
			setSaving(false);
		}
	};

	const configured = status?.enabled ?? false;

	return (
		<div className="ui-card ui-card-pad fd-stack" data-testid="fd-oidc-panel">
			<div className="fd-row" style={{ justifyContent: "space-between" }}>
				<h2 style={{ fontSize: "1rem" }}>{t("settings.oidc.title")}</h2>
				<span
					className={`ui-badge ${configured ? "ui-badge-ok" : "ui-badge-info"}`}
					data-testid="fd-oidc-status"
				>
					{configured
						? t("settings.oidc.statusConfigured")
						: t("settings.oidc.statusIncomplete")}
				</span>
			</div>
			<p
				className="fd-faint"
				style={{ fontSize: "0.82rem", margin: "0.3rem 0 0.6rem" }}
			>
				{t("settings.oidc.hint")}
			</p>

			<label className="fd-row" style={{ cursor: "pointer" }}>
				<input
					type="checkbox"
					checked={enabled}
					disabled={saving}
					onChange={(e) => setEnabled(e.target.checked)}
					data-testid="fd-oidc-enable"
				/>
				<span style={{ fontWeight: 500 }}>
					{t("settings.oidc.enableLabel")}
				</span>
			</label>

			{enabled && (
				<>
					<div className="ui-field">
						<label className="ui-label" htmlFor="oidc-issuer">
							{t("settings.oidc.issuerLabel")}
						</label>
						<input
							id="oidc-issuer"
							className="ui-input"
							type="url"
							placeholder="https://auth.example.com"
							autoComplete="off"
							spellCheck={false}
							value={issuer}
							disabled={saving}
							onChange={(e) => setIssuer(e.target.value)}
						/>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
						>
							{t("settings.oidc.issuerHint")}
						</div>
					</div>

					<div className="ui-field">
						<label className="ui-label" htmlFor="oidc-client-id">
							{t("settings.oidc.clientIdLabel")}
						</label>
						<input
							id="oidc-client-id"
							className="ui-input"
							type="text"
							autoComplete="off"
							spellCheck={false}
							value={clientId}
							disabled={saving}
							onChange={(e) => setClientId(e.target.value)}
						/>
					</div>

					<div className="ui-field">
						<label className="ui-label" htmlFor="oidc-secret">
							{t("settings.oidc.clientSecretLabel")}
						</label>
						<input
							id="oidc-secret"
							className="ui-input"
							type="password"
							autoComplete="off"
							spellCheck={false}
							value={secret}
							disabled={saving}
							onChange={(e) => setSecret(e.target.value)}
						/>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
						>
							{secret === MASK
								? t("settings.oidc.secretStoredNote")
								: t("settings.oidc.clientSecretHint")}
						</div>
					</div>

					<div className="ui-field">
						<label className="ui-label" htmlFor="oidc-base-url">
							{t("settings.oidc.baseUrlLabel")}
						</label>
						<input
							id="oidc-base-url"
							className="ui-input"
							type="url"
							placeholder="https://frontdesk.example.com"
							autoComplete="off"
							spellCheck={false}
							value={baseUrl}
							disabled={saving}
							onChange={(e) => setBaseUrl(e.target.value)}
						/>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
						>
							{t("settings.oidc.baseUrlHint")}
						</div>
					</div>

					{redirectUri && (
						<div className="ui-field">
							<label className="ui-label" htmlFor="oidc-redirect">
								{t("settings.oidc.redirectUriLabel")}
							</label>
							<input
								id="oidc-redirect"
								className="ui-input"
								type="text"
								readOnly
								value={redirectUri}
								data-testid="fd-oidc-redirect-uri"
								style={{ fontFamily: "var(--font-mono, monospace)" }}
								onFocus={(e) => e.currentTarget.select()}
							/>
							<div
								className="fd-faint"
								style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
							>
								{t("settings.oidc.redirectUriHint")}
							</div>
						</div>
					)}

					<div className="ui-field">
						<label className="ui-label" htmlFor="oidc-emails">
							{t("settings.oidc.allowedEmailsLabel")}
						</label>
						<textarea
							id="oidc-emails"
							className="ui-input"
							rows={3}
							placeholder="admin@example.com, ops@example.com"
							autoComplete="off"
							spellCheck={false}
							value={emails}
							disabled={saving}
							onChange={(e) => setEmails(e.target.value)}
						/>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
						>
							{t("settings.oidc.allowedEmailsHint")}
						</div>
					</div>

					<p className="fd-faint" style={{ fontSize: "0.78rem" }}>
						{t("settings.oidc.fallbackNote")}
					</p>
				</>
			)}

			{saveError && (
				<div className="fd-error-text" role="alert">
					{saveError}
				</div>
			)}

			<div className="fd-row">
				<button
					type="button"
					className="ui-btn ui-btn-primary"
					disabled={saving}
					onClick={save}
					data-testid="fd-oidc-save"
				>
					{saving ? t("common.saving") : t("settings.oidc.saveBtn")}
				</button>
			</div>
		</div>
	);
}
