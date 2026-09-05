import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "@/lib/icons";
import { api } from "../../api/client";
import { CopyablePill } from "../../components/CopyablePill";
import { SecretField } from "../../components/SecretField";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { isForcedBlur } from "../../utils/forcedBlur";
import { useSettingsMutations } from "./useSettingsMutations";

/**
 * OidcPanel configures OpenID Connect single sign-on: a third admin-login path
 * alongside passkeys and TOTP. The client secret is an encrypted-at-rest secret
 * (masked on read, echo-preserved on save), mirroring the Apprise target field.
 *
 * SSO is additive and never replaces local login: the admin token / passkey /
 * TOTP paths always remain, so a misconfigured or unreachable IdP cannot lock
 * the operator out. The redirect URI shown here is what the operator registers
 * with their IdP; it is derived from the configured public base URL.
 */
export function OidcPanel({ managed }: { managed?: boolean }) {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const enabled = settings?.oidc_enabled === "true";
	const issuer = settings?.oidc_issuer_url ?? "";
	const clientId = settings?.oidc_client_id ?? "";
	const baseUrl = settings?.oidc_public_base_url ?? "";
	const allowedEmails = settings?.oidc_allowed_emails ?? "";
	const secretConfigured = Boolean(settings?.oidc_client_secret);

	// Blur-committed drafts so typing doesn't fire a save per keystroke.
	const [issuerDraft, setIssuerDraft] = useState<string | null>(null);
	const [clientIdDraft, setClientIdDraft] = useState<string | null>(null);
	const [baseUrlDraft, setBaseUrlDraft] = useState<string | null>(null);
	const [emailsDraft, setEmailsDraft] = useState<string | null>(null);
	const [secretDraft, setSecretDraft] = useState("");

	const commit = (key: string, draft: string | null, current: string) => {
		if (draft !== null && draft !== current) {
			updateMutation.mutate({ [key]: draft });
		}
	};

	const commitSecret = () => {
		const v = secretDraft.trim();
		if (v !== "") {
			updateMutation.mutate({ oidc_client_secret: v });
			setSecretDraft("");
		}
	};

	const clearSecret = () => {
		updateMutation.mutate({ oidc_client_secret: "" });
		setSecretDraft("");
	};

	const redirectUri = baseUrl
		? `${baseUrl.replace(/\/+$/, "")}/api/auth/oidc/callback`
		: "";

	// Configured-state pill, keyed on the saved config so it re-runs on edits.
	const statusQuery = useQuery({
		queryKey: ["oidc-status", issuer, clientId, baseUrl],
		queryFn: () => api.oidc.status(),
		enabled,
		refetchOnWindowFocus: false,
	});
	const configured = statusQuery.data?.enabled ?? false;

	return (
		<div className="space-y-5" data-testid="oidc-panel">
			<p className="text-gray-400 text-sm">{t("settings.oidc.description")}</p>

			{/* Enable toggle */}
			<SettingToggleRow
				className="ui-settings-group"
				label={t("settings.oidc.enable")}
				description={t("settings.oidc.enableDescription")}
				checked={enabled}
				onChange={(v) =>
					updateMutation.mutate({ oidc_enabled: v ? "true" : "false" })
				}
				onReset={() => resetSettingMutation.mutate(["oidc_enabled"])}
				resetDisabled={isResetting}
			/>

			{enabled && (
				<>
					{/* Issuer URL */}
					<div className="space-y-1.5">
						<label
							htmlFor="oidc-issuer"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.oidc.issuer")}
						</label>
						<input
							id="oidc-issuer"
							type="text"
							value={issuerDraft ?? issuer}
							placeholder="https://auth.example.com"
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setIssuerDraft(e.target.value)}
							onBlur={(e) => {
								if (!isForcedBlur(e))
									commit("oidc_issuer_url", issuerDraft, issuer);
								setIssuerDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") e.currentTarget.blur();
							}}
							className="ui-input text-sm w-full"
							data-testid="oidc-issuer-input"
						/>
						<p className="text-gray-500 text-xs">
							{t("settings.oidc.issuerDescription")}
						</p>
						<div
							className="flex items-center gap-2 text-xs"
							data-testid="oidc-status"
						>
							{statusQuery.isFetching ? (
								<span className="inline-flex items-center gap-1.5 text-gray-400">
									<RefreshCw size={12} className="animate-spin" />
									{t("settings.oidc.status.checking")}
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5 text-gray-300">
									<span
										className={`inline-block w-2 h-2 rounded-full ${configured ? "bg-green-500" : "bg-amber-500"}`}
										aria-hidden="true"
									/>
									{configured
										? t("settings.oidc.status.configured")
										: t("settings.oidc.status.incomplete")}
								</span>
							)}
						</div>
					</div>

					{/* Client ID */}
					<div className="space-y-1.5">
						<label
							htmlFor="oidc-client-id"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.oidc.clientId")}
						</label>
						<input
							id="oidc-client-id"
							type="text"
							value={clientIdDraft ?? clientId}
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setClientIdDraft(e.target.value)}
							onBlur={(e) => {
								if (!isForcedBlur(e))
									commit("oidc_client_id", clientIdDraft, clientId);
								setClientIdDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") e.currentTarget.blur();
							}}
							className="ui-input text-sm w-full font-mono"
							data-testid="oidc-client-id-input"
						/>
					</div>

					{/* Client secret (encrypted at rest) */}
					<div className="space-y-1.5">
						<label
							htmlFor="oidc-client-secret"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.oidc.clientSecret")}
						</label>
						<SecretField
							id="oidc-client-secret"
							testId="oidc-client-secret"
							value={secretDraft}
							configured={secretConfigured}
							placeholder={
								secretConfigured
									? t("settings.oidc.secretConfigured")
									: t("settings.oidc.secretPlaceholder")
							}
							onChange={setSecretDraft}
							onCommit={commitSecret}
							onClear={clearSecret}
							toggleLabel={t("settings.oidc.toggleSecret")}
							clearLabel={t("settings.oidc.clear")}
							clearConfirmTitle={t("settings.common.clearSecretConfirmTitle")}
							clearConfirmMessage={t(
								"settings.common.clearSecretConfirmMessage",
							)}
						/>
					</div>

					{/* Public base URL */}
					<div className="space-y-1.5">
						<label
							htmlFor="oidc-base-url"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.oidc.publicBaseUrl")}
						</label>
						<input
							id="oidc-base-url"
							type="text"
							value={baseUrlDraft ?? baseUrl}
							placeholder="https://hotel.example.com"
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setBaseUrlDraft(e.target.value)}
							onBlur={(e) => {
								if (!isForcedBlur(e))
									commit("oidc_public_base_url", baseUrlDraft, baseUrl);
								setBaseUrlDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") e.currentTarget.blur();
							}}
							className="ui-input text-sm w-full"
							data-testid="oidc-base-url-input"
						/>
						<p className="text-gray-500 text-xs">
							{t("settings.oidc.publicBaseUrlDescription")}
						</p>
					</div>

					{/* Redirect URI to register with the IdP */}
					{redirectUri && (
						<div className="space-y-1.5">
							<p className="text-sm font-medium text-gray-300">
								{t("settings.oidc.redirectUri")}
							</p>
							<CopyablePill
								text={redirectUri}
								tooltip={t("settings.oidc.copyRedirectUri")}
								textClassName="font-mono text-xs break-all text-gray-200 select-all"
							/>
							<p className="text-gray-500 text-xs">
								{t("settings.oidc.redirectUriDescription")}
							</p>
						</div>
					)}

					<p className="text-gray-500 text-xs">
						{t("settings.oidc.fallbackNote")}
					</p>
				</>
			)}

			{/* Allowed emails: rendered even while the provider is disabled. The
			    allowlist is fleet-synced (the ACL), so on a fleet primary that
			    does not itself offer this IdP it is still THE place to set who
			    may log in on the members that do. */}
			<div className="space-y-1.5">
				<label
					htmlFor="oidc-allowed-emails"
					className="text-sm font-medium text-gray-300"
				>
					{t("settings.oidc.allowedEmails")}
				</label>
				<textarea
					id="oidc-allowed-emails"
					rows={3}
					value={emailsDraft ?? allowedEmails}
					placeholder="admin@example.com, ops@example.com"
					spellCheck={false}
					autoComplete="off"
					onChange={(e) => setEmailsDraft(e.target.value)}
					onBlur={(e) => {
						if (!isForcedBlur(e))
							commit("oidc_allowed_emails", emailsDraft, allowedEmails);
						setEmailsDraft(null);
					}}
					className="ui-input text-sm w-full font-mono"
					data-testid="oidc-allowed-emails-input"
					// The allowlist is the fleet-wide ACL: unlike the rest of
					// this panel it stays synced, so a managed member cannot
					// edit it.
					disabled={managed}
				/>
				<p className="text-gray-500 text-xs">
					{t("settings.oidc.allowedEmailsDescription")}
				</p>
			</div>
		</div>
	);
}
