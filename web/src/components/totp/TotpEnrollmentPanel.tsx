import { useTranslation } from "react-i18next";
import { Copy, X } from "@/lib/icons";
import { formatDate } from "../../utils/format";
import { TotpRecoveryCodes } from "../TotpRecoveryCodes";
import type { TotpEnrollment } from "./useTotpEnrollment";

/**
 * The four views of a TOTP panel: the one-off recovery-code reveal, the
 * enrolling QR/secret/code form, the enabled badge with its disable flow, and
 * the not-yet-enabled description.
 *
 * `recoveryRemaining`/`recoveryTotal` come from the caller because the admin
 * panel reads them off a separate admin-gated endpoint while the self-service
 * page gets them with its status.
 */
export function TotpEnrollmentPanel({
	totp,
	idPrefix,
	testIdPrefix,
	recoveryRemaining,
	recoveryTotal,
	lastUsedAt,
}: {
	totp: TotpEnrollment;
	/** Prefix for the element ids the labels point at. */
	idPrefix: string;
	/** When set, each control carries `<prefix>-<name>` as its data-testid. */
	testIdPrefix?: string;
	recoveryRemaining?: number | null;
	recoveryTotal?: number | null;
	lastUsedAt?: string | null;
}) {
	const { t } = useTranslation();
	const testId = (name: string) =>
		testIdPrefix ? `${testIdPrefix}-${name}` : undefined;

	if (totp.showRecovery && totp.recoveryCodes.length > 0) {
		return (
			<TotpRecoveryCodes
				codes={totp.recoveryCodes}
				onSaved={totp.savedRecoveryCodes}
				testIdPrefix={testIdPrefix}
			/>
		);
	}

	if (totp.enrol.uri) {
		return (
			<div className="space-y-4">
				<p className="text-(--text-secondary) text-sm">
					{t("settings.totp.enableDescription")}
				</p>
				{totp.enrol.qrDataUrl && (
					<div className="flex justify-center">
						<img
							src={totp.enrol.qrDataUrl}
							alt={t("settings.totp.qrAlt")}
							className="rounded-lg"
						/>
					</div>
				)}
				<div>
					<label
						htmlFor={`${idPrefix}-secret`}
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.secret")}
					</label>
					<div className="flex items-center gap-2">
						<code
							id={`${idPrefix}-secret`}
							className="flex-1 p-2 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default) font-mono text-sm text-(--text-primary) break-all"
						>
							{totp.enrol.secret}
						</code>
						<button
							type="button"
							onClick={totp.copySecret}
							className="ui-icon-btn shrink-0"
							aria-label={t("settings.totp.copySecretAriaLabel")}
							data-testid={testId("copy-secret")}
						>
							<Copy size={16} />
						</button>
					</div>
				</div>
				<div>
					<label
						htmlFor={`${idPrefix}-verify-code`}
						className="block text-sm font-medium text-(--text-primary) mb-2"
					>
						{t("settings.totp.enterCode")}
					</label>
					<input
						id={`${idPrefix}-verify-code`}
						type="text"
						value={totp.enrol.verifyCode}
						onChange={(e) => totp.setVerifyCode(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Enter") totp.verify();
						}}
						inputMode="numeric"
						maxLength={6}
						autoComplete="one-time-code"
						pattern="[0-9]*"
						placeholder={t("settings.totp.codePlaceholder")}
						className="ui-input"
						aria-label={t("settings.totp.codeAriaLabel")}
						data-testid={testId("verify-code")}
					/>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						onClick={totp.verify}
						disabled={totp.verifyPending || !totp.enrol.verifyCode.trim()}
						className="ui-btn ui-btn-primary"
						aria-label={t("settings.totp.verifyAriaLabel")}
						data-testid={testId("verify-button")}
					>
						{totp.verifyPending
							? t("settings.totp.verifying")
							: t("settings.totp.verify")}
					</button>
					<button
						type="button"
						onClick={totp.cancelEnroll}
						className="ui-btn ui-btn-secondary"
						aria-label={t("settings.totp.cancelEnrollAriaLabel")}
						data-testid={testId("cancel-enroll")}
					>
						<X size={16} />
						{t("common.cancel")}
					</button>
				</div>
			</div>
		);
	}

	if (totp.enabled) {
		return (
			<div className="space-y-4">
				<div className="flex items-center justify-between">
					<span className="ui-badge ui-badge-success">
						{t("settings.totp.enabled")}
					</span>
					<button
						type="button"
						onClick={totp.toggleDisabling}
						className="ui-btn ui-btn-danger"
						aria-label={t("settings.totp.disableAriaLabel")}
						data-testid={testId("disable-toggle")}
					>
						{t("settings.totp.disable")}
					</button>
				</div>
				{totp.status?.enabled_at && (
					<p className="text-(--text-tertiary) text-sm">
						{t("settings.totp.enabledOn", {
							date: formatDate(totp.status.enabled_at),
						})}
					</p>
				)}
				{recoveryTotal != null && (
					<dl className="text-(--text-tertiary) text-sm space-y-1">
						<div className="flex items-center justify-between gap-2">
							<dt>{t("settings.totp.recoveryRemaining")}</dt>
							<dd className="text-(--text-secondary) tabular-nums">
								{recoveryRemaining} / {recoveryTotal}
							</dd>
						</div>
						{lastUsedAt && (
							<div className="flex items-center justify-between gap-2">
								<dt>{t("settings.totp.lastUsed")}</dt>
								<dd className="text-(--text-secondary)">
									{formatDate(lastUsedAt)}
								</dd>
							</div>
						)}
					</dl>
				)}
				{totp.disabling && (
					<div className="space-y-3">
						<p className="text-(--text-secondary) text-sm">
							{t("settings.totp.disableDescription")}
						</p>
						<input
							type="text"
							value={totp.disableCode}
							onChange={(e) => totp.setDisableCode(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter") totp.disable();
							}}
							inputMode="text"
							autoComplete="one-time-code"
							maxLength={19}
							placeholder={t("settings.totp.codePlaceholder")}
							className="ui-input"
							aria-label={t("settings.totp.disableCodeAriaLabel")}
							data-testid={testId("disable-code")}
						/>
						<button
							type="button"
							onClick={totp.disable}
							disabled={totp.disablePending || !totp.disableCode.trim()}
							className="ui-btn ui-btn-danger"
							aria-label={t("settings.totp.confirmDisableAriaLabel")}
							data-testid={testId("disable-confirm")}
						>
							{totp.disablePending
								? t("settings.totp.disabling")
								: t("settings.totp.disable")}
						</button>
					</div>
				)}
			</div>
		);
	}

	return (
		<div className="space-y-4">
			<p className="text-(--text-secondary) text-sm">
				{t("settings.totp.description")}
			</p>
			<button
				type="button"
				onClick={totp.startEnroll}
				disabled={totp.startPending}
				className="ui-btn ui-btn-primary"
				aria-label={t("settings.totp.enableAriaLabel")}
				data-testid={testId("enable-button")}
			>
				{t("settings.totp.enable")}
			</button>
		</div>
	);
}
