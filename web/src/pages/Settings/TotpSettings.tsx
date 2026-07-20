import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, Download, X } from "@/lib/icons";
import { api } from "../../api/client";
import { CopyButton } from "../../components/CopyButton";
import { useToast } from "../../context/ToastContext";
import { formatDate } from "../../utils/format";

export function TotpPanel() {
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

	const { data: status } = useQuery({
		queryKey: ["totp", "status"],
		queryFn: () => api.totp.status(),
	});

	const enabled = status?.enabled ?? false;

	// Recovery-code usage + last-used, for the enabled view. Lives on the
	// admin-gated /totp/info (not the polled public /totp/status).
	const { data: info } = useQuery({
		queryKey: ["totp", "info"],
		queryFn: () => api.totp.info(),
		enabled,
	});

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

	const enrollStartMutation = useMutation({
		mutationFn: () => api.totp.enrollStart(),
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
		mutationFn: (code: string) => api.totp.enrollVerify(code),
		onSuccess: (data) => {
			// Enabling 2FA invalidates the raw admin token the browser was using;
			// the server rotates the session cookie pair in the response so we stay
			// logged in instead of the dashboard suddenly going "API offline". No
			// client-side token juggling is needed.
			setRecoveryCodes(data.recovery_codes);
			setShowRecovery(true);
			setEnrollUri("");
			setEnrollSecret("");
			setVerifyCode("");
			setQrDataUrl("");
			queryClient.invalidateQueries({ queryKey: ["totp", "status"] });
			toast(t("settings.totp.verifiedSuccess"), "success");
		},
		onError: () => {
			toast(t("settings.totp.failedToVerify"), "error");
		},
	});

	const disableMutation = useMutation({
		mutationFn: (code: string) => api.totp.disable(code),
		onSuccess: () => {
			setDisabling(false);
			setDisableCode("");
			queryClient.invalidateQueries({ queryKey: ["totp", "status"] });
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

	const handleSavedRecoveryCodes = () => {
		setRecoveryCodes([]);
		setShowRecovery(false);
		queryClient.invalidateQueries({ queryKey: ["totp", "status"] });
	};

	// Recovery codes reveal view: shown once after a successful verify.
	if (showRecovery && recoveryCodes.length > 0) {
		return (
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
					>
						<Download size={16} />
						{t("settings.totp.downloadCodes")}
					</button>
					<button
						type="button"
						onClick={handleSavedRecoveryCodes}
						className="ui-btn ui-btn-primary"
						aria-label={t("settings.totp.savedAriaLabel")}
					>
						<Check size={16} />
						{t("settings.totp.saved")}
					</button>
				</div>
			</div>
		);
	}

	// Enrolling view: QR + secret + code input.
	if (enrollUri) {
		return (
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
						htmlFor="totp-secret"
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.secret")}
					</label>
					<div className="flex items-center gap-2">
						<code className="flex-1 p-2 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default) font-mono text-sm text-(--text-primary) break-all">
							{enrollSecret}
						</code>
						<button
							type="button"
							onClick={handleCopySecret}
							className="ui-icon-btn shrink-0"
							aria-label={t("settings.totp.copySecretAriaLabel")}
						>
							<Copy size={16} />
						</button>
					</div>
				</div>
				<div>
					<label
						htmlFor="totp-verify-code"
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.enterCode")}
					</label>
					<input
						id="totp-verify-code"
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
					/>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						onClick={handleVerify}
						disabled={enrollVerifyMutation.isPending || !verifyCode.trim()}
						className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
						aria-label={t("settings.totp.verifyAriaLabel")}
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
					>
						<X size={16} />
						{t("common.cancel")}
					</button>
				</div>
			</div>
		);
	}

	// Enabled view: badge + disable flow.
	if (enabled) {
		return (
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
				{info && (
					<dl className="text-(--text-tertiary) text-sm space-y-1">
						<div className="flex items-center justify-between gap-2">
							<dt>{t("settings.totp.recoveryRemaining")}</dt>
							<dd className="text-(--text-secondary) tabular-nums">
								{info.recovery_remaining} / {info.recovery_total}
							</dd>
						</div>
						{info.last_used_at && (
							<div className="flex items-center justify-between gap-2">
								<dt>{t("settings.totp.lastUsed")}</dt>
								<dd className="text-(--text-secondary)">
									{formatDate(info.last_used_at)}
								</dd>
							</div>
						)}
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
						/>
						<button
							type="button"
							onClick={handleDisable}
							disabled={disableMutation.isPending || !disableCode.trim()}
							className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
							aria-label={t("settings.totp.confirmDisableAriaLabel")}
						>
							{disableMutation.isPending
								? t("settings.totp.disabling")
								: t("settings.totp.disable")}
						</button>
					</div>
				)}
			</div>
		);
	}

	// Disabled view: description + enable button.
	return (
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
			>
				{t("settings.totp.enable")}
			</button>
		</div>
	);
}
