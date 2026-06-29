import { FingerprintIcon } from "@phosphor-icons/react";
import { startAuthentication } from "@simplewebauthn/browser";
import { type SyntheticEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api, clearAuthToken, setAuthToken } from "../api/client";

interface LoginProps {
	onAuthenticated: () => void;
}

// Login covers all three Front Desk auth paths (HTTPS-only ingress, so every
// method is available): raw FRONTDESK_TOKEN when TOTP is off, token + TOTP code
// when 2FA is on, and passkey. Each path ends by storing a bearer token the rest
// of the app sends (raw token or a minted session token).
export function Login({ onAuthenticated }: LoginProps) {
	const { t } = useTranslation();
	const [token, setToken] = useState("");
	const [code, setCode] = useState("");
	const [totpEnabled, setTotpEnabled] = useState(false);
	const [passkeyEnabled, setPasskeyEnabled] = useState(false);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		api
			.totpStatus()
			.then((s) => setTotpEnabled(s.enabled))
			.catch(() => {});
		api
			.webauthnAvailable()
			.then((a) => setPasskeyEnabled(a.enabled && a.has_credentials))
			.catch(() => {});
	}, []);

	const fail = (e: unknown, fallback: string) => {
		if (e instanceof ApiError && e.status === 429) {
			setError(t("login.tooManyAttempts"));
		} else {
			setError(fallback);
		}
	};

	const submitToken = async (e: SyntheticEvent) => {
		e.preventDefault();
		setError("");
		setBusy(true);
		try {
			if (totpEnabled) {
				const res = await api.totpLogin(token, code);
				setAuthToken(res.token);
			} else {
				// No 2FA: the raw token is the bearer, so it must be installed before
				// the validating call can authenticate. If validation fails for any
				// reason (a wrong token OR a plain network error), clear it again so a
				// bad token can't linger in storage and brief-render the authenticated
				// view on the next page load.
				setAuthToken(token);
				try {
					await api.listMembers();
				} catch (err) {
					clearAuthToken();
					throw err;
				}
			}
			onAuthenticated();
		} catch (err) {
			fail(err, totpEnabled ? t("login.invalidCode") : t("login.invalidToken"));
		} finally {
			setBusy(false);
		}
	};

	const loginPasskey = async () => {
		setError("");
		setBusy(true);
		try {
			const start = await api.webauthnLoginStart();
			const credential = await startAuthentication({
				optionsJSON: start.options,
			});
			const res = await api.webauthnLoginFinish(start.session_id, credential);
			setAuthToken(res.token);
			onAuthenticated();
		} catch (err) {
			fail(err, t("login.failed"));
		} finally {
			setBusy(false);
		}
	};

	return (
		<div className="fd-login">
			<div className="ui-card ui-card-pad">
				<div className="fd-brand" style={{ marginBottom: "1rem" }}>
					<span className="fd-dot" />
					{t("login.title")}
				</div>
				<form onSubmit={submitToken}>
					<div className="ui-field">
						<label className="ui-label" htmlFor="fd-token">
							{t("login.tokenLabel")}
						</label>
						<input
							id="fd-token"
							className="ui-input"
							type="password"
							autoComplete="current-password"
							value={token}
							onChange={(e) => setToken(e.target.value)}
							placeholder={t("login.tokenPlaceholder")}
						/>
					</div>
					{totpEnabled && (
						<div className="ui-field">
							<label className="ui-label" htmlFor="fd-code">
								{t("login.totpLabel")}
							</label>
							<input
								id="fd-code"
								className="ui-input"
								inputMode="numeric"
								autoComplete="one-time-code"
								value={code}
								onChange={(e) => setCode(e.target.value)}
								placeholder={t("login.totpPlaceholder")}
							/>
							<div
								className="fd-faint"
								style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
							>
								{t("login.totpHint")}
							</div>
						</div>
					)}
					{error && (
						<div
							className="fd-error-text"
							role="alert"
							style={{ marginBottom: "0.6rem" }}
						>
							{error}
						</div>
					)}
					<button
						className="ui-btn ui-btn-primary"
						type="submit"
						disabled={busy || !token}
						style={{ width: "100%" }}
					>
						{busy
							? t("login.signingIn")
							: totpEnabled
								? t("login.verify")
								: t("login.signIn")}
					</button>
				</form>
				{passkeyEnabled && (
					<button
						className="ui-btn ui-btn-ghost"
						type="button"
						onClick={loginPasskey}
						disabled={busy}
						style={{ width: "100%", marginTop: "0.6rem" }}
					>
						<FingerprintIcon size={18} weight="bold" />
						{t("login.passkey")}
					</button>
				)}
			</div>
		</div>
	);
}
