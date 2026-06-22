import { useTranslation } from "react-i18next";
import { KeyRound } from "@/lib/icons";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { PasskeyPanel } from "./PasskeySettings";
import { TotpPanel } from "./TotpSettings";

interface AuthenticationSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

/**
 * Authentication groups the two admin sign-in hardening methods, passkeys and
 * TOTP two-factor, side by side in one section. Each method keeps its own
 * panel/logic (PasskeyPanel, TotpPanel); this just frames them as labelled
 * SettingsGroup columns under a shared header.
 */
export function AuthenticationSettings({
	collapsed,
	onToggle,
}: AuthenticationSettingsProps) {
	const { t } = useTranslation();

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
				<SettingsGroup title={t("settings.totp.title")}>
					<TotpPanel />
				</SettingsGroup>
			</div>
		</SettingsSection>
	);
}
