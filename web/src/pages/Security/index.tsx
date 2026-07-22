import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, Download, ShieldCheck, X } from "@/lib/icons";
import { ApiError, api, clearAuth } from "../../api/client";
import { CopyButton } from "../../components/CopyButton";
import { PageHeader } from "../../components/PageHeader";
import { useToast } from "../../context/ToastContext";
import { formatDate } from "../../utils/format";
import { isBreachedPasswordError } from "../../utils/passwordPolicy";

/**
 * Self-service security page for users-row identities: enroll, inspect, and
 * disable the caller's own TOTP second factor. Mirrors the admin TotpPanel
 * (Settings) against /api/auth/totp/* — no session re-mint is needed here
 * because enabling a user's TOTP changes login requirements only, not the
 * session the caller already holds. Reuses the settings.totp.* strings so the
 * two panels stay word-for-word consistent across locales.
 */
export function Security() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const [enrollUri, setEnrollUri] = useState("");
	const [enrollSecret, setEnrollSecret] = useState("");
	const [verifyCode, setVerifyCode] = useState("");
	const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
	const [showRecovery, setShowRecovery] = useState(false);
	const [disabling, setDisabling] = useState(false);
	const [disableCode, setDisableCode] = useState("");
	const [qrDataUrl, setQrDataUrl] = useState("");
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");

	const { data: status } = useQuery({
		queryKey: ["user-totp", "status"],
		queryFn: () => api.userTotp.status(),
	});
	const enabled = status?.enabled ?? false;

	useEffect(() => {
		if (typeof window === "undefined") return;
		if (!enrollUri) return;
		let cancelled = false;
		QRCode.toDataURL(enrollUri, {
			width: 200,
			margin: 2,
			errorCorrectionLevel: "M",
		})
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

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["user-totp", "status"] });

	const enrollStartMutation = useMutation({
		mutationFn: () => api.userTotp.enrollStart(),
		onSuccess: (data) => {
			setEnrollUri(data.uri);
			setEnrollSecret(data.secret);
			setVerifyCode("");
		},
		onError: (err: Error) => {
			toast(
				t("settings.totp.failedToStart", { message: err.message }),
				"error",
			);
		},
	});

	const enrollVerifyMutation = useMutation({
		mutationFn: (code: string) => api.userTotp.enrollVerify(code),
		onSuccess: (data) => {
			setRecoveryCodes(data.recovery_codes);
			setShowRecovery(true);
			setEnrollUri("");
			setEnrollSecret("");
			setVerifyCode("");
			setQrDataUrl("");
			invalidate();
			toast(t("settings.totp.verifiedSuccess"), "success");
		},
		onError: () => {
			toast(t("settings.totp.failedToVerify"), "error");
		},
	});

	const disableMutation = useMutation({
		mutationFn: (code: string) => api.userTotp.disable(code),
		onSuccess: () => {
			setDisabling(false);
			setDisableCode("");
			invalidate();
			toast(t("settings.totp.disabled"), "success");
		},
		onError: () => {
			toast(t("settings.totp.failedToDisable"), "error");
		},
	});

	const handleVerify = () => {
		const code = verifyCode.trim();
		if (!code) return;
		enrollVerifyMutation.mutate(code);
	};

	const handleDisable = () => {
		const code = disableCode.trim();
		if (!code) return;
		disableMutation.mutate(code);
	};

	const handleCancelEnroll = () => {
		setEnrollUri("");
		setEnrollSecret("");
		setVerifyCode("");
		setQrDataUrl("");
	};

	const handleCopySecret = async () => {
		try {
			await navigator.clipboard.writeText(enrollSecret);
			toast(t("settings.totp.secretCopied"), "success");
		} catch {
			toast(t("common.failedToCopy"), "error");
		}
	};

	const handleDownloadCodes = () => {
		const blob = new Blob([`${recoveryCodes.join("\n")}\n`], {
			type: "text/plain",
		});
		const url = URL.createObjectURL(blob);
		const a = document.createElement("a");
		a.href = url;
		a.download = "model-hotel-totp-recovery-codes.txt";
		document.body.appendChild(a);
		a.click();
		a.remove();
		URL.revokeObjectURL(url);
	};

	const passwordMutation = useMutation({
		mutationFn: () => api.userTotp.changePassword(currentPassword, newPassword),
		onSuccess: () => {
			// The server revoked every session of the account, this one included.
			// Give the toast a moment, then tear down auth state the same way the
			// logout button does and land on the login screen.
			toast(t("security.password.success"), "success");
			setTimeout(() => {
				clearAuth();
				queryClient.cancelQueries();
				window.location.reload();
			}, 1500);
		},
		onError: (err: Error) => {
			if (err instanceof ApiError && err.status === 401) {
				toast(t("security.password.wrongCurrent"), "error");
				return;
			}
			if (isBreachedPasswordError(err)) {
				toast(t("users.validation.passwordBreached"), "error");
				return;
			}
			toast(t("security.password.failed"), "error");
		},
	});

	const passwordFormValid =
		currentPassword.length > 0 &&
		newPassword.length >= 8 &&
		newPassword === confirmPassword;

	const handleChangePassword = () => {
		if (newPassword !== confirmPassword) {
			toast(t("security.password.mismatch"), "error");
			return;
		}
		if (newPassword.length < 8) {
			toast(t("users.validation.passwordShort"), "error");
			return;
		}
		passwordMutation.mutate();
	};

	const handleSavedRecoveryCodes = () => {
		setRecoveryCodes([]);
		setShowRecovery(false);
		invalidate();
	};

	let body: React.ReactNode;
	if (showRecovery && recoveryCodes.length > 0) {
		body = (
			<div className="space-y-4">
				<div className="p-3 bg-amber-900/30 border border-amber-600 rounded-(--radius-box)">
					<p className="text-sm text-amber-300 font-medium">
						{t("settings.totp.recoveryCodesWarning")}
					</p>
				</div>
				<div>
					<div className="flex items-center justify-between mb-2">
						<h3 className="text-sm font-medium text-(--text-primary)">
							{t("settings.totp.recoveryCodes")}
						</h3>
						<CopyButton
							text={recoveryCodes.join("\n")}
							size={16}
							title={t("settings.totp.copyAll")}
							toastType="success"
						/>
					</div>
					<div className="p-3 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default)">
						<div className="font-mono text-sm space-y-1">
							{recoveryCodes.map((code) => (
								<div key={code} className="text-(--text-primary) break-all">
									{code}
								</div>
							))}
						</div>
					</div>
				</div>
				<div className="flex flex-wrap gap-2">
					<button
						type="button"
						onClick={handleDownloadCodes}
						className="ui-btn ui-btn-secondary"
						aria-label={t("settings.totp.downloadCodesAriaLabel")}
						data-testid="security-download-codes"
					>
						<Download size={16} />
						{t("settings.totp.downloadCodes")}
					</button>
					<button
						type="button"
						onClick={handleSavedRecoveryCodes}
						className="ui-btn ui-btn-primary"
						aria-label={t("settings.totp.savedAriaLabel")}
						data-testid="security-saved-codes"
					>
						<Check size={16} />
						{t("settings.totp.saved")}
					</button>
				</div>
			</div>
		);
	} else if (enrollUri) {
		body = (
			<div className="space-y-4">
				<p className="text-(--text-secondary) text-sm">
					{t("settings.totp.enableDescription")}
				</p>
				{qrDataUrl && (
					<div className="flex justify-center">
						<img
							src={qrDataUrl}
							alt={t("settings.totp.qrAlt")}
							className="rounded-lg"
						/>
					</div>
				)}
				<div>
					<label
						htmlFor="user-totp-secret"
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.secret")}
					</label>
					<div className="flex items-center gap-2">
						<code
							id="user-totp-secret"
							className="flex-1 p-2 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default) font-mono text-sm text-(--text-primary) break-all"
						>
							{enrollSecret}
						</code>
						<button
							type="button"
							onClick={handleCopySecret}
							className="ui-icon-btn shrink-0"
							aria-label={t("settings.totp.copySecretAriaLabel")}
							data-testid="security-copy-secret"
						>
							<Copy size={16} />
						</button>
					</div>
				</div>
				<div>
					<label
						htmlFor="user-totp-verify-code"
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.enterCode")}
					</label>
					<input
						id="user-totp-verify-code"
						type="text"
						value={verifyCode}
						onChange={(e) => setVerifyCode(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Enter") handleVerify();
						}}
						inputMode="numeric"
						maxLength={6}
						autoComplete="one-time-code"
						pattern="[0-9]*"
						placeholder={t("settings.totp.codePlaceholder")}
						className="ui-input"
						aria-label={t("settings.totp.codeAriaLabel")}
						data-testid="security-verify-code"
					/>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						onClick={handleVerify}
						disabled={enrollVerifyMutation.isPending || !verifyCode.trim()}
						className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
						aria-label={t("settings.totp.verifyAriaLabel")}
						data-testid="security-verify-button"
					>
						{enrollVerifyMutation.isPending
							? t("settings.totp.verifying")
							: t("settings.totp.verify")}
					</button>
					<button
						type="button"
						onClick={handleCancelEnroll}
						className="ui-btn ui-btn-secondary"
						aria-label={t("settings.totp.cancelEnrollAriaLabel")}
						data-testid="security-cancel-enroll"
					>
						<X size={16} />
						{t("common.cancel")}
					</button>
				</div>
			</div>
		);
	} else if (enabled) {
		body = (
			<div className="space-y-4">
				<div className="flex items-center justify-between">
					<span className="ui-badge ui-badge-success">
						{t("settings.totp.enabled")}
					</span>
					<button
						type="button"
						onClick={() => setDisabling(!disabling)}
						className="ui-btn ui-btn-danger"
						aria-label={t("settings.totp.disableAriaLabel")}
						data-testid="security-disable-toggle"
					>
						{t("settings.totp.disable")}
					</button>
				</div>
				{status?.enabled_at && (
					<p className="text-(--text-tertiary) text-sm">
						{t("settings.totp.enabledOn", {
							date: formatDate(status.enabled_at),
						})}
					</p>
				)}
				{status?.recovery_total != null && (
					<dl className="text-(--text-tertiary) text-sm space-y-1">
						<div className="flex items-center justify-between gap-2">
							<dt>{t("settings.totp.recoveryRemaining")}</dt>
							<dd className="text-(--text-secondary) tabular-nums">
								{status.recovery_remaining} / {status.recovery_total}
							</dd>
						</div>
					</dl>
				)}
				{disabling && (
					<div className="space-y-3">
						<p className="text-(--text-secondary) text-sm">
							{t("settings.totp.disableDescription")}
						</p>
						<input
							type="text"
							value={disableCode}
							onChange={(e) => setDisableCode(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") handleDisable();
							}}
							inputMode="text"
							autoComplete="one-time-code"
							maxLength={19}
							placeholder={t("settings.totp.codePlaceholder")}
							className="ui-input"
							aria-label={t("settings.totp.disableCodeAriaLabel")}
							data-testid="security-disable-code"
						/>
						<button
							type="button"
							onClick={handleDisable}
							disabled={disableMutation.isPending || !disableCode.trim()}
							className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
							aria-label={t("settings.totp.confirmDisableAriaLabel")}
							data-testid="security-disable-confirm"
						>
							{disableMutation.isPending
								? t("settings.totp.disabling")
								: t("settings.totp.disable")}
						</button>
					</div>
				)}
			</div>
		);
	} else {
		body = (
			<div className="space-y-4">
				<p className="text-(--text-secondary) text-sm">
					{t("settings.totp.description")}
				</p>
				<button
					type="button"
					onClick={() => enrollStartMutation.mutate()}
					disabled={enrollStartMutation.isPending}
					className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
					aria-label={t("settings.totp.enableAriaLabel")}
					data-testid="security-enable-button"
				>
					{t("settings.totp.enable")}
				</button>
			</div>
		);
	}

	return (
		<div className="space-y-6 pb-8">
			<PageHeader
				icon={ShieldCheck}
				title={t("security.title")}
				description={t("security.description")}
			/>
			<div className="ui-card p-6 max-w-2xl">
				<h2 className="text-base font-semibold text-(--text-primary) mb-4">
					{t("settings.totp.title")}
				</h2>
				{body}
			</div>
			<div className="ui-card p-6 max-w-2xl">
				<h2 className="text-base font-semibold text-(--text-primary) mb-1">
					{t("security.password.title")}
				</h2>
				<p className="text-(--text-secondary) text-sm mb-4">
					{t("security.password.description")}
				</p>
				<form
					className="space-y-3"
					onSubmit={(e) => {
						e.preventDefault();
						handleChangePassword();
					}}
				>
					{/* Hidden username field helps password managers bind the entry. */}
					<input
						type="text"
						autoComplete="username"
						className="hidden"
						tabIndex={-1}
						aria-hidden="true"
						readOnly
					/>
					<div>
						<label
							htmlFor="security-current-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.current")}
						</label>
						<input
							id="security-current-password"
							type="password"
							value={currentPassword}
							onChange={(e) => setCurrentPassword(e.target.value)}
							autoComplete="current-password"
							className="ui-input"
							data-testid="security-current-password"
						/>
					</div>
					<div>
						<label
							htmlFor="security-new-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.new")}
						</label>
						<input
							id="security-new-password"
							type="password"
							value={newPassword}
							onChange={(e) => setNewPassword(e.target.value)}
							autoComplete="new-password"
							placeholder={t("users.modal.passwordPlaceholder")}
							className="ui-input"
							data-testid="security-new-password"
						/>
					</div>
					<div>
						<label
							htmlFor="security-confirm-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.confirm")}
						</label>
						<input
							id="security-confirm-password"
							type="password"
							value={confirmPassword}
							onChange={(e) => setConfirmPassword(e.target.value)}
							autoComplete="new-password"
							className="ui-input"
							data-testid="security-confirm-password"
						/>
						{confirmPassword.length > 0 && newPassword !== confirmPassword && (
							<p className="text-sm text-red-400 mt-1">
								{t("security.password.mismatch")}
							</p>
						)}
					</div>
					<button
						type="submit"
						disabled={passwordMutation.isPending || !passwordFormValid}
						className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
						data-testid="security-password-submit"
					>
						{t("security.password.submit")}
					</button>
				</form>
			</div>
		</div>
	);
}
