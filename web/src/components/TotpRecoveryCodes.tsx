import { useTranslation } from "react-i18next";
import { Check, Download } from "@/lib/icons";
import { CopyButton } from "./CopyButton";

/**
 * One-time reveal of freshly minted TOTP recovery codes, shared by the admin
 * TOTP panel (Settings) and the self-service Security page so the two stay
 * word-for-word and pixel-for-pixel identical. `onSaved` is the caller's
 * acknowledgement that the codes have been stored somewhere safe.
 */
export function TotpRecoveryCodes({
	codes,
	onSaved,
	testIdPrefix,
}: {
	codes: string[];
	onSaved: () => void;
	testIdPrefix?: string;
}) {
	const { t } = useTranslation();

	const handleDownload = () => {
		const blob = new Blob([`${codes.join("\n")}\n`], {
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

	const testId = (name: string) =>
		testIdPrefix ? { "data-testid": `${testIdPrefix}-${name}` } : {};

	return (
		<div className="space-y-4">
			<div className="ui-callout ui-callout-warning">
				<p className="font-medium">{t("settings.totp.recoveryCodesWarning")}</p>
			</div>
			<div>
				<div className="flex items-center justify-between mb-2">
					<h3 className="text-sm font-medium text-(--text-primary)">
						{t("settings.totp.recoveryCodes")}
					</h3>
					<CopyButton
						text={codes.join("\n")}
						size={16}
						title={t("settings.totp.copyAll")}
						toastType="success"
					/>
				</div>
				<div className="p-3 bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default)">
					<div className="font-mono text-sm space-y-1">
						{codes.map((code) => (
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
					onClick={handleDownload}
					className="ui-btn ui-btn-secondary"
					aria-label={t("settings.totp.downloadCodesAriaLabel")}
					{...testId("download-codes")}
				>
					<Download size={16} />
					{t("settings.totp.downloadCodes")}
				</button>
				<button
					type="button"
					onClick={onSaved}
					className="ui-btn ui-btn-primary"
					aria-label={t("settings.totp.savedAriaLabel")}
					{...testId("saved-codes")}
				>
					<Check size={16} />
					{t("settings.totp.saved")}
				</button>
			</div>
		</div>
	);
}
