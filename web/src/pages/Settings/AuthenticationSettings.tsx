import { useTranslation } from "react-i18next";
import { KeyRound } from "@/lib/icons";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SETTING_DEFAULTS } from "./defaults";
import { GithubPanel } from "./GithubSettings";
import { OidcPanel } from "./OidcSettings";
import { PasskeyPanel } from "./PasskeySettings";
import { TotpPanel } from "./TotpSettings";
import { useSettingsMutations } from "./useSettingsMutations";

interface AuthenticationSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

/**
 * Authentication groups the admin sign-in hardening methods, passkeys and TOTP
 * two-factor, side by side. The session auto-logout control sits in the right
 * column beneath TOTP so it doesn't stretch across both columns. Each method
 * keeps its own panel/logic (PasskeyPanel, TotpPanel); the session timeout is a
 * stored setting (session_idle_timeout_minutes) consumed by useIdleLogout to
 * sign the admin out after inactivity (0 = never).
 */
export function AuthenticationSettings({
	collapsed,
	onToggle,
}: AuthenticationSettingsProps) {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation } =
		useSettingsMutations();

	const idleMinutes = Number(
		settings?.session_idle_timeout_minutes ??
			SETTING_DEFAULTS.session_idle_timeout_minutes,
	);

	return (
		<SettingsSection
			icon={KeyRound}
			title={t("settings.authentication.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
				<SettingsGroup title={t("settings.passkeys.title")}>
					<PasskeyPanel />
				</SettingsGroup>
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
				</div>
			</div>
			<div className="mt-5">
				<SettingsGroup title={t("settings.oidc.title")}>
					<OidcPanel />
				</SettingsGroup>
			</div>
			<div className="mt-5">
				<SettingsGroup title={t("settings.github.title")}>
					<GithubPanel />
				</SettingsGroup>
			</div>
		</SettingsSection>
	);
}
