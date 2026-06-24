import { useTranslation } from "react-i18next";

// Placeholder — implemented in the Traffic-tab commit.
export function TrafficPage() {
	const { t } = useTranslation();
	return <h1 className="fd-page-title">{t("traffic.title")}</h1>;
}
