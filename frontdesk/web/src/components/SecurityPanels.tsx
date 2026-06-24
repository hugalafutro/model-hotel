import {
	Check,
	Copy,
	DownloadSimple,
	Fingerprint,
	PencilSimple,
	Plus,
	Trash,
	X,
} from "@phosphor-icons/react";
import QRCode from "qrcode";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, setAuthToken } from "../api/client";
import type { TotpInfo, WebAuthnCredential } from "../api/types";
import { useToast } from "../context/ToastContext";
import { formatAbsolute } from "../utils/time";
import { registerPasskey } from "../utils/webauthn";

// SecurityPanels holds Front Desk's own admin-auth management: passkeys and
// authenticator-app TOTP. Both are stored on this Front Desk instance only and
// are never synced to members (a passkey is bound to this origin; TOTP is a
// local secret). The Login screen consumes the results: the passkey button
// appears once a credential exists, and the TOTP field appears once enabled.

export function SecurityPanels() {
	return (
		<>
			<PasskeyPanel />
			<TotpPanel />
		</>
	);
}

function copyText(
	value: string,
	ok: () => void,
	fail: () => void,
): Promise<void> {
	return navigator.clipboard.writeText(value).then(ok, fail);
}

function PasskeyPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [configured, setConfigured] = useState(false);
	const [creds, setCreds] = useState<WebAuthnCredential[]>([]);
	const [registering, setRegistering] = useState(false);

	const loadCreds = useCallback(() => {
		api
			.webauthnListCredentials()
			.then(setCreds)
			.catch(() => {});
	}, []);

	useEffect(() => {
		let cancelled = false;
		api
			.webauthnAvailable()
			.then((a) => {
				if (cancelled) return;
				setConfigured(a.enabled);
				if (a.enabled) loadCreds();
			})
			.catch(() => {});
		return () => {
			cancelled = true;
		};
	}, [loadCreds]);

	const handleRegister = async () => {
		setRegistering(true);
		try {
			const ok = await registerPasskey();
			if (ok) {
				toast(t("settings.passkeys.registered"), "success");
				loadCreds();
			}
		} catch (err) {
			const msg =
				err instanceof Error && err.name === "InvalidStateError"
					? t("settings.passkeys.alreadyRegistered")
					: t("settings.passkeys.registerFailed");
			toast(msg, "error");
		} finally {
			setRegistering(false);
		}
	};

	const handleDelete = (id: string) => {
		api
			.webauthnDeleteCredential(id)
			.then(() => {
				toast(t("settings.passkeys.deleted"), "success");
				loadCreds();
			})
			.catch(() => toast(t("settings.passkeys.deleteFailed"), "error"));
	};

	const handleRename = (id: string, name: string) => {
		api
			.webauthnRenameCredential(id, name)
			.then(() => {
				toast(t("settings.passkeys.renamed"), "success");
				loadCreds();
			})
			.catch(() => toast(t("settings.passkeys.renameFailed"), "error"));
	};

	return (
		<div className="ui-card ui-card-pad">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.passkeys.section")}</h2>
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.passkeys.intro")}
			</p>

			{!configured ? (
				<div
					className="ui-badge ui-badge-warn"
					style={{
						display: "block",
						padding: "0.5rem 0.7rem",
						whiteSpace: "normal",
						lineHeight: 1.4,
					}}
				>
					{t("settings.passkeys.notConfigured")}
				</div>
			) : (
				<>
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						onClick={handleRegister}
						disabled={registering}
					>
						<Plus size={16} weight="bold" />
						{registering
							? t("settings.passkeys.registering")
							: t("settings.passkeys.register")}
					</button>

					{creds.length === 0 ? (
						<p
							className="fd-faint"
							style={{ fontSize: "0.82rem", marginTop: "0.8rem" }}
						>
							{t("settings.passkeys.none")}
						</p>
					) : (
						<div className="fd-stack" style={{ marginTop: "0.8rem" }}>
							{creds.map((cred) => (
								<CredentialRow
									key={cred.id}
									cred={cred}
									onRename={handleRename}
									onDelete={handleDelete}
								/>
							))}
						</div>
					)}
				</>
			)}
		</div>
	);
}

function CredentialRow({
	cred,
	onRename,
	onDelete,
}: {
	cred: WebAuthnCredential;
	onRename: (id: string, name: string) => void;
	onDelete: (id: string) => void;
}) {
	const { t } = useTranslation();
	const [editing, setEditing] = useState(false);
	const [draft, setDraft] = useState(cred.name);
	const displayName = cred.name || t("settings.passkeys.unnamed");

	const save = () => {
		const name = draft.trim();
		setEditing(false);
		if (name && name !== cred.name) onRename(cred.id, name);
	};

	return (
		<div
			className="fd-row fd-spread"
			style={{ alignItems: "center", gap: "0.6rem" }}
		>
			<div style={{ minWidth: 0 }}>
				{editing ? (
					<div className="fd-row" style={{ gap: "0.3rem" }}>
						<input
							className="ui-input"
							value={draft}
							// biome-ignore lint/a11y/noAutofocus: intentional focus when entering inline rename
							autoFocus
							maxLength={128}
							onChange={(e) => setDraft(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") save();
								if (e.key === "Escape") setEditing(false);
							}}
							aria-label={t("settings.passkeys.nameLabel")}
							style={{ width: "12rem" }}
						/>
						<button
							type="button"
							className="ui-btn ui-btn-ghost ui-btn-sm"
							onClick={save}
							aria-label={t("common.save")}
						>
							<Check size={14} weight="bold" />
						</button>
						<button
							type="button"
							className="ui-btn ui-btn-ghost ui-btn-sm"
							onClick={() => {
								setDraft(cred.name);
								setEditing(false);
							}}
							aria-label={t("common.cancel")}
						>
							<X size={14} weight="bold" />
						</button>
					</div>
				) : (
					<button
						type="button"
						className="ui-btn ui-btn-ghost ui-btn-sm"
						onClick={() => {
							setDraft(cred.name);
							setEditing(true);
						}}
						aria-label={t("settings.passkeys.renameLabel")}
						style={{ maxWidth: "100%" }}
					>
						<span
							style={{
								overflow: "hidden",
								textOverflow: "ellipsis",
								whiteSpace: "nowrap",
							}}
						>
							{displayName}
						</span>
						<PencilSimple size={12} />
					</button>
				)}
				<p className="fd-faint" style={{ fontSize: "0.75rem" }}>
					{t("settings.passkeys.registeredOn", {
						date: formatAbsolute(cred.created_at),
					})}
				</p>
			</div>
			<button
				type="button"
				className="ui-btn ui-btn-ghost ui-btn-sm"
				onClick={() => onDelete(cred.id)}
				aria-label={t("settings.passkeys.deleteLabel")}
			>
				<Trash size={14} />
			</button>
		</div>
	);
}

function TotpPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();

	const [enabled, setEnabled] = useState(false);
	const [enabledAt, setEnabledAt] = useState<string | undefined>(undefined);
	const [info, setInfo] = useState<TotpInfo | null>(null);

	const [enrollUri, setEnrollUri] = useState("");
	const [enrollSecret, setEnrollSecret] = useState("");
	const [qrDataUrl, setQrDataUrl] = useState("");
	const [verifyCode, setVerifyCode] = useState("");
	const [verifying, setVerifying] = useState(false);

	const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
	const [disabling, setDisabling] = useState(false);
	const [disableCode, setDisableCode] = useState("");
	const [working, setWorking] = useState(false);

	const loadStatus = useCallback(() => {
		api
			.totpStatus()
			.then((s) => {
				setEnabled(s.enabled);
				setEnabledAt(s.enabled_at);
				if (s.enabled)
					api
						.totpInfo()
						.then(setInfo)
						.catch(() => {});
			})
			.catch(() => {});
	}, []);

	useEffect(() => {
		loadStatus();
	}, [loadStatus]);

	useEffect(() => {
		if (!enrollUri) return;
		let cancelled = false;
		QRCode.toDataURL(enrollUri, { width: 200, margin: 2 })
			.then((url) => {
				if (!cancelled) setQrDataUrl(url);
			})
			.catch(() => {
				if (!cancelled) setQrDataUrl("");
			});
		return () => {
			cancelled = true;
		};
	}, [enrollUri]);

	const resetEnroll = () => {
		setEnrollUri("");
		setEnrollSecret("");
		setQrDataUrl("");
		setVerifyCode("");
	};

	const startEnroll = () => {
		setWorking(true);
		api
			.totpEnrollStart()
			.then((d) => {
				setEnrollUri(d.uri);
				setEnrollSecret(d.secret);
				setVerifyCode("");
			})
			.catch(() => toast(t("settings.totp.failedToStart"), "error"))
			.finally(() => setWorking(false));
	};

	const verify = () => {
		const code = verifyCode.trim();
		if (!code) return;
		setVerifying(true);
		api
			.totpEnrollVerify(code)
			.then((d) => {
				// Enabling TOTP invalidates the raw FRONTDESK_TOKEN bearer; the server
				// mints a session token so we stay logged in. Install it before any
				// follow-up request.
				if (d.token) setAuthToken(d.token);
				setRecoveryCodes(d.recovery_codes);
				resetEnroll();
				loadStatus();
				toast(t("settings.totp.verifiedSuccess"), "success");
			})
			.catch(() => toast(t("settings.totp.failedToVerify"), "error"))
			.finally(() => setVerifying(false));
	};

	const disable = () => {
		const code = disableCode.trim();
		if (!code) return;
		setWorking(true);
		api
			.totpDisable(code)
			.then(() => {
				setDisabling(false);
				setDisableCode("");
				setInfo(null);
				loadStatus();
				toast(t("settings.totp.disabled"), "success");
			})
			.catch(() => toast(t("settings.totp.failedToDisable"), "error"))
			.finally(() => setWorking(false));
	};

	const downloadCodes = () => {
		const blob = new Blob([`${recoveryCodes.join("\n")}\n`], {
			type: "text/plain",
		});
		const url = URL.createObjectURL(blob);
		const a = document.createElement("a");
		a.href = url;
		a.download = "front-desk-totp-recovery-codes.txt";
		document.body.appendChild(a);
		a.click();
		a.remove();
		URL.revokeObjectURL(url);
	};

	const heading = (
		<h2 style={{ fontSize: "1rem" }}>{t("settings.totp.section")}</h2>
	);

	// Recovery-codes reveal: shown once right after a successful verify.
	if (recoveryCodes.length > 0) {
		return (
			<div className="ui-card ui-card-pad">
				{heading}
				<div
					className="ui-badge ui-badge-warn"
					style={{
						display: "block",
						margin: "0.6rem 0",
						padding: "0.5rem 0.7rem",
						whiteSpace: "normal",
						lineHeight: 1.4,
					}}
				>
					{t("settings.totp.recoveryWarning")}
				</div>
				<div className="fd-mono" style={{ fontSize: "0.85rem" }}>
					{recoveryCodes.map((c) => (
						<div key={c}>{c}</div>
					))}
				</div>
				<div className="fd-row" style={{ gap: "0.5rem", marginTop: "0.8rem" }}>
					<button
						type="button"
						className="ui-btn ui-btn-ghost"
						onClick={() =>
							copyText(
								recoveryCodes.join("\n"),
								() => toast(t("settings.totp.codesCopied"), "success"),
								() => toast(t("common.copy"), "error"),
							)
						}
					>
						<Copy size={16} />
						{t("settings.totp.copyAll")}
					</button>
					<button
						type="button"
						className="ui-btn ui-btn-ghost"
						onClick={downloadCodes}
					>
						<DownloadSimple size={16} />
						{t("settings.totp.downloadCodes")}
					</button>
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						onClick={() => setRecoveryCodes([])}
					>
						<Check size={16} weight="bold" />
						{t("settings.totp.saved")}
					</button>
				</div>
			</div>
		);
	}

	// Enrolling: QR + secret + confirm code.
	if (enrollUri) {
		return (
			<div className="ui-card ui-card-pad">
				{heading}
				<p
					className="fd-muted"
					style={{ fontSize: "0.85rem", margin: "0.4rem 0 0.8rem" }}
				>
					{t("settings.totp.enableDescription")}
				</p>
				{qrDataUrl && (
					<div style={{ display: "flex", justifyContent: "center" }}>
						{/* QR is a data: URL generated from the otpauth URI on this page. */}
						<img
							src={qrDataUrl}
							alt={t("settings.totp.qrAlt")}
							style={{ borderRadius: "0.4rem" }}
						/>
					</div>
				)}
				<div className="ui-field" style={{ marginTop: "0.8rem" }}>
					<span className="ui-label">{t("settings.totp.secret")}</span>
					<div className="fd-row" style={{ gap: "0.4rem" }}>
						<code
							className="fd-mono"
							style={{
								flex: 1,
								wordBreak: "break-all",
								fontSize: "0.85rem",
							}}
						>
							{enrollSecret}
						</code>
						<button
							type="button"
							className="ui-btn ui-btn-ghost ui-btn-sm"
							onClick={() =>
								copyText(
									enrollSecret,
									() => toast(t("settings.totp.secretCopied"), "success"),
									() => toast(t("common.copy"), "error"),
								)
							}
							aria-label={t("settings.totp.secret")}
						>
							<Copy size={16} />
						</button>
					</div>
				</div>
				<div className="ui-field">
					<label className="ui-label" htmlFor="fd-totp-verify">
						{t("settings.totp.enterCode")}
					</label>
					<input
						id="fd-totp-verify"
						className="ui-input"
						inputMode="numeric"
						autoComplete="one-time-code"
						maxLength={6}
						value={verifyCode}
						onChange={(e) => setVerifyCode(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Enter") verify();
						}}
						placeholder={t("settings.totp.codePlaceholder")}
					/>
				</div>
				<div className="fd-row" style={{ gap: "0.5rem" }}>
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						onClick={verify}
						disabled={verifying || !verifyCode.trim()}
					>
						{verifying
							? t("settings.totp.verifying")
							: t("settings.totp.verify")}
					</button>
					<button
						type="button"
						className="ui-btn ui-btn-ghost"
						onClick={resetEnroll}
					>
						<X size={16} />
						{t("common.cancel")}
					</button>
				</div>
			</div>
		);
	}

	// Enabled: status + recovery counts + disable flow.
	if (enabled) {
		return (
			<div className="ui-card ui-card-pad">
				{heading}
				<div
					className="fd-row fd-spread"
					style={{ alignItems: "center", margin: "0.6rem 0" }}
				>
					<span className="ui-badge ui-badge-ok">
						{t("settings.totp.enabled")}
					</span>
					<button
						type="button"
						className="ui-btn ui-btn-danger"
						onClick={() => setDisabling((v) => !v)}
					>
						{t("settings.totp.disable")}
					</button>
				</div>
				{enabledAt && (
					<p className="fd-faint" style={{ fontSize: "0.8rem" }}>
						{t("settings.totp.enabledOn", { date: formatAbsolute(enabledAt) })}
					</p>
				)}
				{info && (
					<p className="fd-faint" style={{ fontSize: "0.8rem" }}>
						{t("settings.totp.recoveryRemaining")}: {info.recovery_remaining} /{" "}
						{info.recovery_total}
						{info.last_used_at
							? ` · ${t("settings.totp.lastUsed")} ${formatAbsolute(info.last_used_at)}`
							: ""}
					</p>
				)}
				{disabling && (
					<div className="fd-stack" style={{ marginTop: "0.8rem" }}>
						<p className="fd-muted" style={{ fontSize: "0.85rem" }}>
							{t("settings.totp.disableDescription")}
						</p>
						<input
							className="ui-input"
							autoComplete="one-time-code"
							maxLength={19}
							value={disableCode}
							onChange={(e) => setDisableCode(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") disable();
							}}
							placeholder={t("settings.totp.disablePlaceholder")}
							aria-label={t("settings.totp.disablePlaceholder")}
						/>
						<button
							type="button"
							className="ui-btn ui-btn-danger"
							onClick={disable}
							disabled={working || !disableCode.trim()}
						>
							{working
								? t("settings.totp.disabling")
								: t("settings.totp.disable")}
						</button>
					</div>
				)}
			</div>
		);
	}

	// Disabled: description + enable.
	return (
		<div className="ui-card ui-card-pad">
			{heading}
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.totp.intro")}
			</p>
			<button
				type="button"
				className="ui-btn ui-btn-primary"
				onClick={startEnroll}
				disabled={working}
			>
				<Fingerprint size={16} weight="bold" />
				{t("settings.totp.enable")}
			</button>
		</div>
	);
}
