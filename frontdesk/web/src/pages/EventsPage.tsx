import { useTranslation } from "react-i18next";

// Placeholder — implemented in the Events-tab commit.
export function EventsPage() {
	const { t } = useTranslation();
	return <h1 className="fd-page-title">{t("events.title")}</h1>;
}
