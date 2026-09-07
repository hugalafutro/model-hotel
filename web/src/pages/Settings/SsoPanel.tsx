import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BlurCommitInput } from "../../components/BlurCommitInput";
import { CopyablePill } from "../../components/CopyablePill";
import { SecretField } from "../../components/SecretField";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { StatusDot } from "../../components/StatusDot";
import { isForcedBlur } from "../../utils/forcedBlur";
import { useSettingsMutations } from "./useSettingsMutations";

/** The settings keys one SSO provider owns. `issuer` is OIDC-only. */
export interface SsoKeys {
	enabled: string;
	clientId: string;
	clientSecret: string;
	baseUrl: string;
	allowedEmails: string;
	issuer?: string;
}

/**
 * The shared OAuth/OIDC single-sign-on panel: enable toggle, provider fields
 * committed on blur, the encrypted client secret, the callback URL to register
 * with the provider, and the fleet-synced email allowlist.
 *
 * SSO is additive and never replaces local login: the admin token / passkey /
 * TOTP paths always remain, so a misconfigured or unreachable IdP cannot lock
 * the operator out.
 */
export function SsoPanel({
	prefix,
	keys,
	fetchStatus,
	callbackPath,
	callbackKeys,
	setupHint = false,
	managed,
}: {
	/** Names the i18n namespace (`settings.<prefix>.*`), the element ids and the test ids. */
	prefix: "github" | "oidc";
	keys: SsoKeys;
	fetchStatus: () => Promise<{ enabled: boolean }>;
	/** Path appended to the public base URL to form the callback. */
	callbackPath: string;
	/** The three i18n key names for the callback block, which the two panels word differently. */
	callbackKeys: { label: string; copy: string; description: string };
	/** True to show the provider-registration hint above the fields. */
	setupHint?: boolean;
	managed?: boolean;
}) {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();
	const p = `settings.${prefix}`;

	const enabled = settings?.[keys.enabled] === "true";
	const issuer = keys.issuer ? (settings?.[keys.issuer] ?? "") : "";
	const clientId = settings?.[keys.clientId] ?? "";
	const baseUrl = settings?.[keys.baseUrl] ?? "";
	const allowedEmails = settings?.[keys.allowedEmails] ?? "";
	const secretConfigured = Boolean(settings?.[keys.clientSecret]);

	const [emailsDraft, setEmailsDraft] = useState<string | null>(null);
	const [secretDraft, setSecretDraft] = useState("");

	const commit = (key: string) => (next: string) =>
		updateMutation.mutate({ [key]: next });

	const commitSecret = () => {
		const v = secretDraft.trim();
		if (v !== "") {
			updateMutation.mutate({ [keys.clientSecret]: v });
			setSecretDraft("");
		}
	};

	const clearSecret = () => {
		updateMutation.mutate({ [keys.clientSecret]: "" });
		setSecretDraft("");
	};

	const callbackUri = baseUrl
		? `${baseUrl.replace(/\/+$/, "")}${callbackPath}`
		: "";

	// Configured-state pill, keyed on the saved config so it re-runs on edits.
	const statusQuery = useQuery({
		queryKey: [`${prefix}-status`, issuer, clientId, baseUrl, secretConfigured],
		queryFn: fetchStatus,
		enabled,
		refetchOnWindowFocus: false,
	});
	// Status deliberately does not read the client secret (it's an unauthenticated,
	// login-screen-polled endpoint), so AND in the locally-known secret presence:
	// without this the pill would show a false-positive green when the secret is
	// blank, even though Start would then fail to build a usable runtime.
	const configured = secretConfigured && (statusQuery.data?.enabled ?? false);

	// The pill sits under the first field: the issuer where there is one, the
	// client id otherwise.
	const statusPill = (
		<div
			className="flex items-center gap-2 text-xs"
			data-testid={`${prefix}-status`}
		>
			<StatusDot
				state={statusQuery.isFetching ? "checking" : configured ? "ok" : "warn"}
				label={t(
					statusQuery.isFetching
						? `${p}.status.checking`
						: configured
							? `${p}.status.configured`
							: `${p}.status.incomplete`,
				)}
			/>
		</div>
	);

	return (
		<div className="space-y-5" data-testid={`${prefix}-panel`}>
			<p className="text-gray-400 text-sm">{t(`${p}.description`)}</p>

			{/* Enable toggle */}
			<SettingToggleRow
				className="ui-settings-group"
				label={t(`${p}.enable`)}
				description={t(`${p}.enableDescription`)}
				checked={enabled}
				onChange={(v) =>
					updateMutation.mutate({ [keys.enabled]: v ? "true" : "false" })
				}
				onReset={() => resetSettingMutation.mutate([keys.enabled])}
				resetDisabled={isResetting}
			/>

			{enabled && (
				<>
					{setupHint && (
						<p className="text-gray-500 text-xs">{t(`${p}.setupHint`)}</p>
					)}

					{keys.issuer && (
						<div className="space-y-1.5">
							<label
								htmlFor={`${prefix}-issuer`}
								className="text-sm font-medium text-gray-300"
							>
								{t(`${p}.issuer`)}
							</label>
							<BlurCommitInput
								id={`${prefix}-issuer`}
								value={issuer}
								onCommit={commit(keys.issuer)}
								placeholder="https://auth.example.com"
								testId={`${prefix}-issuer-input`}
							/>
							<p className="text-gray-500 text-xs">
								{t(`${p}.issuerDescription`)}
							</p>
							{statusPill}
						</div>
					)}

					{/* Client ID */}
					<div className="space-y-1.5">
						<label
							htmlFor={`${prefix}-client-id`}
							className="text-sm font-medium text-gray-300"
						>
							{t(`${p}.clientId`)}
						</label>
						<BlurCommitInput
							id={`${prefix}-client-id`}
							value={clientId}
							onCommit={commit(keys.clientId)}
							mono
							testId={`${prefix}-client-id-input`}
						/>
						{!keys.issuer && statusPill}
					</div>

					{/* Client secret (encrypted at rest) */}
					<div className="space-y-1.5">
						<label
							htmlFor={`${prefix}-client-secret`}
							className="text-sm font-medium text-gray-300"
						>
							{t(`${p}.clientSecret`)}
						</label>
						<SecretField
							id={`${prefix}-client-secret`}
							testId={`${prefix}-client-secret`}
							value={secretDraft}
							configured={secretConfigured}
							placeholder={t(
								secretConfigured
									? `${p}.secretConfigured`
									: `${p}.secretPlaceholder`,
							)}
							onChange={setSecretDraft}
							onCommit={commitSecret}
							onClear={clearSecret}
							toggleLabel={t(`${p}.toggleSecret`)}
							clearLabel={t(`${p}.clear`)}
							clearConfirmTitle={t("settings.common.clearSecretConfirmTitle")}
							clearConfirmMessage={t(
								"settings.common.clearSecretConfirmMessage",
							)}
						/>
					</div>

					{/* Public base URL */}
					<div className="space-y-1.5">
						<label
							htmlFor={`${prefix}-base-url`}
							className="text-sm font-medium text-gray-300"
						>
							{t(`${p}.publicBaseUrl`)}
						</label>
						<BlurCommitInput
							id={`${prefix}-base-url`}
							value={baseUrl}
							onCommit={commit(keys.baseUrl)}
							placeholder="https://hotel.example.com"
							testId={`${prefix}-base-url-input`}
						/>
						<p className="text-gray-500 text-xs">
							{t(`${p}.publicBaseUrlDescription`)}
						</p>
					</div>

					{/* Callback URL to register with the provider */}
					{callbackUri && (
						<div className="space-y-1.5">
							<p className="text-sm font-medium text-gray-300">
								{t(`${p}.${callbackKeys.label}`)}
							</p>
							<CopyablePill
								text={callbackUri}
								tooltip={t(`${p}.${callbackKeys.copy}`)}
								textClassName="font-mono text-xs break-all text-gray-200 select-all"
							/>
							<p className="text-gray-500 text-xs">
								{t(`${p}.${callbackKeys.description}`)}
							</p>
						</div>
					)}

					<p className="text-gray-500 text-xs">{t(`${p}.fallbackNote`)}</p>
				</>
			)}

			{/* Allowed emails: rendered even while the provider is disabled. The
			    allowlist is fleet-synced (the ACL), so on a fleet primary that
			    does not itself offer this IdP it is still THE place to set who
			    may log in on the members that do. */}
			<div className="space-y-1.5">
				<label
					htmlFor={`${prefix}-allowed-emails`}
					className="text-sm font-medium text-gray-300"
				>
					{t(`${p}.allowedEmails`)}
				</label>
				<textarea
					id={`${prefix}-allowed-emails`}
					rows={3}
					value={emailsDraft ?? allowedEmails}
					placeholder="admin@example.com, ops@example.com"
					spellCheck={false}
					autoComplete="off"
					onChange={(e) => setEmailsDraft(e.target.value)}
					onBlur={(e) => {
						if (
							!isForcedBlur(e) &&
							emailsDraft !== null &&
							emailsDraft !== allowedEmails
						) {
							updateMutation.mutate({ [keys.allowedEmails]: emailsDraft });
						}
						setEmailsDraft(null);
					}}
					className="ui-input text-sm w-full font-mono"
					data-testid={`${prefix}-allowed-emails-input`}
					// The allowlist is the fleet-wide ACL: unlike the rest of
					// this panel it stays synced, so a managed member cannot
					// edit it.
					disabled={managed}
				/>
				<p className="text-gray-500 text-xs">
					{t(`${p}.allowedEmailsDescription`)}
				</p>
			</div>
		</div>
	);
}
