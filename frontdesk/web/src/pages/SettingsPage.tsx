import { useTranslation } from "react-i18next";

// Placeholder — implemented in the Settings-tab commits.
export function SettingsPage() {
	const { t } = useTranslation();
	return <h1 className="fd-page-title">{t("settings.title")}</h1>;
}
