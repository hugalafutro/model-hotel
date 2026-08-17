import { useTranslation } from "react-i18next";
import { KeyRound } from "@/lib/icons";
import { ResetButton } from "../../components/ResetButton";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { ActiveSessionsPanel } from "./ActiveSessionsSettings";
import { SETTING_DEFAULTS } from "./defaults";
import { GithubPanel } from "./GithubSettings";
import { OidcPanel } from "./OidcSettings";
import { PasskeyPanel } from "./PasskeySettings";
import { TotpPanel } from "./TotpSettings";
import { useSettingsMutations } from "./useSettingsMutations";

interface AuthenticationSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	managed?: boolean;
}

/**
 * Authentication groups the admin sign-in hardening methods side by side:
 * passkeys with the active-sessions list beneath them on the left, TOTP
 * two-factor, the tab timeout and the password policy stacked on the right.
 * Each method keeps its own panel/logic (PasskeyPanel, TotpPanel,
 * ActiveSessionsPanel); the session timeout is a stored setting
 * (session_idle_timeout_minutes) consumed by useIdleLogout to sign the admin
 * out after inactivity (0 = never). The SSO panels follow below at full width.
 *
 * Like Alerts, this is a mixed section: passkeys, TOTP, sessions, the tab
 * timeout, and the SSO provider config (which IdPs this member offers) are
 * instance-local, while the password policy and the SSO email allowlists are
 * fleet-synced (a managed member 403s those writes). While managed, the
 * password policy sits in a disabled fieldset with the managed note right
 * under it, and each SSO panel disables just its allowlist input, instead of
 * forwarding `managed` to SettingsSection, which would disable the local
 * parts too.
 */
export function AuthenticationSettings({
	collapsed,
	onToggle,
	managed,
}: AuthenticationSettingsProps) {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const idleMinutes = Number(
		settings?.session_idle_timeout_minutes ??
			SETTING_DEFAULTS.session_idle_timeout_minutes,
	);

	const breachCheckEnabled =
		(settings?.pwned_password_check_enabled ??
			SETTING_DEFAULTS.pwned_password_check_enabled) === "true";

	return (
		<SettingsSection
			icon={KeyRound}
			title={t("settings.authentication.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
				<div className="space-y-5">
					<SettingsGroup title={t("settings.passkeys.title")}>
						<PasskeyPanel />
					</SettingsGroup>
					<SettingsGroup title={t("settings.activeSessions.title")}>
						<ActiveSessionsPanel />
					</SettingsGroup>
				</div>
				<div className="space-y-5">
					<SettingsGroup title={t("settings.totp.title")}>
						<TotpPanel />
					</SettingsGroup>
					<SettingsGroup title={t("settings.sessionTimeout.title")}>
						<SettingsSlider
							id="session-idle-timeout"
							label={t("settings.sessionTimeout.label")}
							value={Number.isFinite(idleMinutes) ? idleMinutes : 60}
							min={0}
							max={240}
							step={5}
							clampStep={5}
							infinityValue={0}
							unit="m"
							onChange={(v) =>
								updateMutation.mutate({
									session_idle_timeout_minutes: String(v),
								})
							}
							description={t("settings.sessionTimeout.hint")}
							onReset={() =>
								resetSettingMutation.mutate(["session_idle_timeout_minutes"])
							}
							resetTooltip={t("settings.common.resetSetting")}
						/>
					</SettingsGroup>
					{/* Only the password policy is fully fleet-synced; the SSO panels
					    are per-member apart from their email allowlists, which each
					    panel disables itself. A disabled fieldset natively disables
					    every control it wraps, the same idiom SettingsSection uses. */}
					<fieldset disabled={managed} className="m-0 min-w-0 border-0 p-0">
						<SettingsGroup title={t("settings.passwordPolicy.title")}>
							<div className="flex items-center justify-between gap-3">
								<div className="min-w-0">
									<div className="flex items-center gap-1">
										<p className="text-sm font-medium text-gray-300">
											{t("settings.passwordPolicy.breachCheckLabel")}
										</p>
										<ResetButton
											tooltip={t("settings.common.resetSetting")}
											onClick={() =>
												resetSettingMutation.mutate([
													"pwned_password_check_enabled",
												])
											}
											size={12}
											disabled={isResetting || updateMutation.isPending}
										/>
									</div>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.passwordPolicy.breachCheckDescription")}
									</p>
								</div>
								<Toggle
									checked={breachCheckEnabled}
									size="sm"
									onChange={(v) =>
										updateMutation.mutate({
											pwned_password_check_enabled: v ? "true" : "false",
										})
									}
									disabled={updateMutation.isPending || isResetting}
									ariaLabel={t("settings.passwordPolicy.breachCheckLabel")}
								/>
							</div>
							{/* The one place a trace of a user's password leaves the
							    instance, so the k-anonymity mechanism is summarised
							    next to the toggle. Environment-level configuration
							    (kill-switch, self-hosted mirror) lives in the wiki. */}
							<div
								className="ui-callout ui-callout-info"
								data-testid="breach-check-info"
							>
								<p>
									{t("settings.passwordPolicy.breachCheckInfo")}{" "}
									<a
										href="https://haveibeenpwned.com/API/v3#SearchingPwnedPasswordsByRange"
										target="_blank"
										rel="noopener noreferrer"
									>
										{t("settings.passwordPolicy.breachCheckInfoLink")}
									</a>
								</p>
							</div>
						</SettingsGroup>
					</fieldset>
					{managed && (
						<p
							data-testid="managed-note"
							className="text-xs text-(--text-muted)"
						>
							{t("settings.managed.authNote")}
						</p>
					)}
				</div>
			</div>
			<div className="mt-5">
				<SettingsGroup title={t("settings.oidc.title")}>
					<OidcPanel managed={managed} />
				</SettingsGroup>
			</div>
			<div className="mt-5">
				<SettingsGroup title={t("settings.github.title")}>
					<GithubPanel managed={managed} />
				</SettingsGroup>
			</div>
		</SettingsSection>
	);
}
