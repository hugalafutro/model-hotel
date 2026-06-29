import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Eye, EyeOff, RefreshCw } from "@/lib/icons";
import { api } from "../../api/client";
import { CopyablePill } from "../../components/CopyablePill";
import { ResetButton } from "../../components/ResetButton";
import { Toggle } from "../../components/Toggle";
import { useSettingsMutations } from "./useSettingsMutations";

/**
 * GithubPanel configures GitHub OAuth single sign-on: a fourth admin-login path
 * alongside OIDC, passkeys, and TOTP. GitHub is OAuth2 only (no discovery), so
 * there is no issuer URL; the operator registers an OAuth App at GitHub and
 * pastes its client id/secret here. The allowlist matches the GitHub account's
 * *verified* emails (the same verified-email rule OIDC uses). The client secret
 * is an encrypted-at-rest secret (masked on read, echo-preserved on save),
 * mirroring the OIDC/Apprise secret fields.
 *
 * SSO is additive and never replaces local login: the admin token / passkey /
 * TOTP paths always remain, so a misconfigured or unreachable IdP cannot lock
 * the operator out.
 */
export function GithubPanel() {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const enabled = settings?.github_sso_enabled === "true";
	const clientId = settings?.github_client_id ?? "";
	const baseUrl = settings?.github_public_base_url ?? "";
	const allowedEmails = settings?.github_allowed_emails ?? "";
	const secretConfigured = Boolean(settings?.github_client_secret);

	// Blur-committed drafts so typing doesn't fire a save per keystroke.
	const [clientIdDraft, setClientIdDraft] = useState<string | null>(null);
	const [baseUrlDraft, setBaseUrlDraft] = useState<string | null>(null);
	const [emailsDraft, setEmailsDraft] = useState<string | null>(null);
	const [secretDraft, setSecretDraft] = useState("");
	// Reveal toggle for the secret field: the value is entered once and never
	// shown back (it is masked on read), so an operator pasting a secret has no
	// other way to confirm exactly what they pasted - e.g. a stray trailing space.
	const [showSecret, setShowSecret] = useState(false);

	const commit = (key: string, draft: string | null, current: string) => {
		if (draft !== null && draft !== current) {
			updateMutation.mutate({ [key]: draft });
		}
	};

	const commitSecret = () => {
		const v = secretDraft.trim();
		if (v !== "") {
			updateMutation.mutate({ github_client_secret: v });
			setSecretDraft("");
		}
	};

	const clearSecret = () => {
		updateMutation.mutate({ github_client_secret: "" });
		setSecretDraft("");
	};

	const callbackUri = baseUrl
		? `${baseUrl.replace(/\/+$/, "")}/api/auth/github/callback`
		: "";

	// Configured-state pill, keyed on the saved config so it re-runs on edits.
	const statusQuery = useQuery({
		queryKey: ["github-status", clientId, baseUrl, secretConfigured],
		queryFn: () => api.github.status(),
		enabled,
		refetchOnWindowFocus: false,
	});
	const configured = statusQuery.data?.enabled ?? false;

	return (
		<div className="space-y-5" data-testid="github-panel">
			<p className="text-gray-400 text-sm">
				{t("settings.github.description")}
			</p>

			{/* Enable toggle */}
			<div className="flex items-center justify-between gap-3 ui-settings-group">
				<div className="min-w-0">
					<div className="flex items-center gap-1">
						<p className="text-sm font-medium text-gray-300">
							{t("settings.github.enable")}
						</p>
						<ResetButton
							tooltip={t("settings.common.resetSetting")}
							onClick={() =>
								resetSettingMutation.mutate(["github_sso_enabled"])
							}
							size={12}
							disabled={isResetting}
						/>
					</div>
					<p className="text-gray-500 text-xs mt-0.5">
						{t("settings.github.enableDescription")}
					</p>
				</div>
				<Toggle
					checked={enabled}
					size="sm"
					onChange={(v) =>
						updateMutation.mutate({ github_sso_enabled: v ? "true" : "false" })
					}
					ariaLabel={t("settings.github.enable")}
				/>
			</div>

			{enabled && (
				<>
					<p className="text-gray-500 text-xs">
						{t("settings.github.setupHint")}
					</p>

					{/* Client ID */}
					<div className="space-y-1.5">
						<label
							htmlFor="github-client-id"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.github.clientId")}
						</label>
						<input
							id="github-client-id"
							type="text"
							value={clientIdDraft ?? clientId}
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setClientIdDraft(e.target.value)}
							onBlur={() => {
								commit("github_client_id", clientIdDraft, clientId);
								setClientIdDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") e.currentTarget.blur();
							}}
							className="ui-input text-sm w-full font-mono"
							data-testid="github-client-id-input"
						/>
						<div
							className="flex items-center gap-2 text-xs"
							data-testid="github-status"
						>
							{statusQuery.isFetching ? (
								<span className="inline-flex items-center gap-1.5 text-gray-400">
									<RefreshCw size={12} className="animate-spin" />
									{t("settings.github.status.checking")}
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5 text-gray-300">
									<span
										className={`inline-block w-2 h-2 rounded-full ${configured ? "bg-green-500" : "bg-amber-500"}`}
										aria-hidden="true"
									/>
									{configured
										? t("settings.github.status.configured")
										: t("settings.github.status.incomplete")}
								</span>
							)}
						</div>
					</div>

					{/* Client secret (encrypted at rest) */}
					<div className="space-y-1.5">
						<label
							htmlFor="github-client-secret"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.github.clientSecret")}
						</label>
						<div className="flex items-center gap-2">
							<input
								id="github-client-secret"
								type={showSecret ? "text" : "password"}
								value={secretDraft}
								placeholder={
									secretConfigured
										? t("settings.github.secretConfigured")
										: t("settings.github.secretPlaceholder")
								}
								spellCheck={false}
								autoComplete="off"
								onChange={(e) => setSecretDraft(e.target.value)}
								onBlur={commitSecret}
								onKeyDown={(e) => {
									if (e.key === "Enter") e.currentTarget.blur();
								}}
								className="ui-input text-sm w-full font-mono"
								data-testid="github-client-secret-input"
							/>
							<button
								type="button"
								className="ui-icon-btn p-1.5"
								// preventDefault keeps focus in the input: without it the
								// click blurs the field, firing commitSecret (which saves and
								// clears the draft) before the reveal can be seen.
								onMouseDown={(e) => e.preventDefault()}
								onClick={() => setShowSecret((v) => !v)}
								aria-label={t("settings.github.toggleSecret")}
								aria-pressed={showSecret}
								title={t("settings.github.toggleSecret")}
								data-testid="github-client-secret-reveal"
							>
								{showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
							</button>
							{secretConfigured && (
								<button
									type="button"
									className="ui-link-accent text-xs whitespace-nowrap"
									onClick={clearSecret}
									data-testid="github-client-secret-clear"
								>
									{t("settings.github.clear")}
								</button>
							)}
						</div>
					</div>

					{/* Public base URL */}
					<div className="space-y-1.5">
						<label
							htmlFor="github-base-url"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.github.publicBaseUrl")}
						</label>
						<input
							id="github-base-url"
							type="text"
							value={baseUrlDraft ?? baseUrl}
							placeholder="https://hotel.example.com"
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setBaseUrlDraft(e.target.value)}
							onBlur={() => {
								commit("github_public_base_url", baseUrlDraft, baseUrl);
								setBaseUrlDraft(null);
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter") e.currentTarget.blur();
							}}
							className="ui-input text-sm w-full"
							data-testid="github-base-url-input"
						/>
						<p className="text-gray-500 text-xs">
							{t("settings.github.publicBaseUrlDescription")}
						</p>
					</div>

					{/* Callback URL to register with GitHub */}
					{callbackUri && (
						<div className="space-y-1.5">
							<p className="text-sm font-medium text-gray-300">
								{t("settings.github.callbackUri")}
							</p>
							<CopyablePill
								text={callbackUri}
								tooltip={t("settings.github.copyCallbackUri")}
								textClassName="font-mono text-xs break-all text-gray-200 select-all"
							/>
							<p className="text-gray-500 text-xs">
								{t("settings.github.callbackUriDescription")}
							</p>
						</div>
					)}

					{/* Allowed emails */}
					<div className="space-y-1.5">
						<label
							htmlFor="github-allowed-emails"
							className="text-sm font-medium text-gray-300"
						>
							{t("settings.github.allowedEmails")}
						</label>
						<textarea
							id="github-allowed-emails"
							rows={3}
							value={emailsDraft ?? allowedEmails}
							placeholder="admin@example.com, ops@example.com"
							spellCheck={false}
							autoComplete="off"
							onChange={(e) => setEmailsDraft(e.target.value)}
							onBlur={() => {
								commit("github_allowed_emails", emailsDraft, allowedEmails);
								setEmailsDraft(null);
							}}
							className="ui-input text-sm w-full font-mono"
							data-testid="github-allowed-emails-input"
						/>
						<p className="text-gray-500 text-xs">
							{t("settings.github.allowedEmailsDescription")}
						</p>
					</div>

					<p className="text-gray-500 text-xs">
						{t("settings.github.fallbackNote")}
					</p>
				</>
			)}
		</div>
	);
}
