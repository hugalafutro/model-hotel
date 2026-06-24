import { useTranslation } from "react-i18next";

// Placeholder — implemented in the Members-tab commit.
export function MembersPage() {
	const { t } = useTranslation();
	return <h1 className="fd-page-title">{t("members.title")}</h1>;
}
